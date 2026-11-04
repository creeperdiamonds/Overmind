package worldgen

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Golden tests of the Nether against BDS 1.26.60.6's Nether worlds (Task 10, rulings 35 to 37). The
// worlds stay outside the repo: OVERMIND_GOLDEN_BEDROCK/nether-whole holds the study's Nether worlds
// (seed 100 chunks -16..15 and four 9 x 9 windows, seed 1 chunks -8..7), converted to OMBN1 files with
// each chunk's mask of blocks BDS does not reproduce between runs.

type bdsNether struct {
	seed           int64
	x0, x1, z0, z1 int
	names          []string
	chunks         map[[2]int]*bdsNetherChunk
}

type bdsNetherChunk struct {
	ids      []uint16 // Index y<<8 | z<<4 | x.
	unstable []byte
}

func (c *bdsNetherChunk) masked(i int) bool { return c.unstable[i>>3]>>(i&7)&1 != 0 }

func loadBDSNether(t testing.TB, path string) *bdsNether {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	r := bufio.NewReaderSize(zr, 1<<20)
	magic := make([]byte, 5)
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "OMBN1" {
		t.Fatalf("%s: not a Nether world file", path)
	}
	var hdr [6]int32
	if err := binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		t.Fatal(err)
	}
	w := &bdsNether{seed: int64(hdr[0]), x0: int(hdr[1]), x1: int(hdr[2]), z0: int(hdr[3]), z1: int(hdr[4]), chunks: map[[2]int]*bdsNetherChunk{}}
	for range hdr[5] {
		var n uint16
		binary.Read(r, binary.LittleEndian, &n)
		b := make([]byte, n)
		io.ReadFull(r, b)
		w.names = append(w.names, string(b))
	}
	for cz := w.z0; cz < w.z1; cz++ {
		for cx := w.x0; cx < w.x1; cx++ {
			p, err := r.ReadByte()
			if err != nil {
				t.Fatal(err)
			}
			if p == 0 {
				continue
			}
			c := &bdsNetherChunk{ids: make([]uint16, netherHeight*256), unstable: make([]byte, netherHeight*256/8)}
			if err := binary.Read(r, binary.LittleEndian, c.ids); err != nil {
				t.Fatal(err)
			}
			if _, err := io.ReadFull(r, c.unstable); err != nil {
				t.Fatal(err)
			}
			w.chunks[[2]int{cx, cz}] = c
		}
	}
	return w
}

// netherClass groups a block for the terrain check: open air, lava, or something solid.
func netherClass(n string) byte {
	switch {
	case n == "air":
		return 'a'
	case n == "lava" || n == "flowing_lava":
		return 'l'
	}
	return 's'
}

// TestNetherMatchesBDS scores our Nether against BDS's, block for block outside the mask, and the
// terrain's class (air, lava, solid), per world.
func TestNetherMatchesBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	files, _ := filepath.Glob(filepath.Join(dir, "nether-whole", "*.bin.gz"))
	if len(files) == 0 {
		t.Skip("no Nether worlds in nether-whole")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".bin.gz")
		t.Run(name, func(t *testing.T) {
			w := loadBDSNether(t, f)
			ng := NewNether(w.seed)
			var total, same, classSame, floorN, floorOK int
			conf := map[[2]string]int{}
			classConf := map[[2]byte]int{}
			var cells [netherHeight * 256]bs
			for k, c := range w.chunks {
				ng.cells(k[0], k[1], &cells)
				for i := range cells {
					if c.masked(i) || i>>8 == netherHeight-1 { // Y 127 holds the study's loading marker (glass).
						continue
					}
					real, mine := w.names[c.ids[i]], bnameString(cells[i])
					total++
					if real == mine {
						same++
					} else {
						conf[[2]string{real, mine}]++
					}
					if netherClass(real) == netherClass(mine) {
						classSame++
					} else {
						classConf[[2]byte{netherClass(real), netherClass(mine)}]++
					}
					if y := i >> 8; y <= 5 || y >= 122 {
						// The rows are exact when every BDS bedrock is ours and ours stands where BDS has
						// bedrock or a block a feature wrote over it (a spring's lava, a magma blob).
						floorN++
						if (real == "bedrock") == (mine == "bedrock") || mine == "bedrock" && real != "netherrack" && real != "air" {
							floorOK++
						} else if floorN-floorOK <= 3 {
							t.Logf("bedrock row miss at chunk %v cell (%d,%d,%d): BDS %s, ours %s", k, i&15, i>>8, i>>4&15, real, mine)
						}
					}
				}
			}
			type kv struct {
				k [2]string
				n int
			}
			var l []kv
			for k, n := range conf {
				l = append(l, kv{k, n})
			}
			slices.SortFunc(l, func(a, b kv) int { return b.n - a.n })
			if len(l) > 10 {
				l = l[:10]
			}
			var cc []string
			for k, n := range classConf {
				cc = append(cc, string(k[0])+"<-"+string(k[1])+":"+strconv.Itoa(n))
			}
			slices.Sort(cc)
			t.Logf("seed %d %s: blocks same %.4f%% (%d wrong), terrain class same %.4f%% %v, bedrock rows %d/%d; top (bds<-ours) %v",
				w.seed, name, 100*float64(same)/float64(total), total-same, 100*float64(classSame)/float64(total), cc, floorOK, floorN, l)
			if floorOK != floorN {
				t.Errorf("bedrock rows: %d of %d differ", floorN-floorOK, floorN)
			}
		})
	}
}
