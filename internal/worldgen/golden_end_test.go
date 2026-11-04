package worldgen

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Golden tests of the End against BDS 1.26.60.6's End worlds (Task 11). The worlds stay outside the
// repo: OVERMIND_GOLDEN_BEDROCK/end-whole holds the study's End worlds converted to OMBE1 files (seed
// 100 and seed 1, chunks -8..7 around the main island, untouched by players; seed 100's outer islands,
// chunks 56..70 x 0..29, and the 7 x 7 chunks around its one natural gateway), Y 0..127 (nothing in
// them is higher), with each chunk's mask of blocks BDS does not reproduce between runs.

type bdsEnd struct {
	seed           int64
	x0, x1, z0, z1 int
	height         int
	names          []string // Bedrock names with their states, as `name{"key":value,...}`.
	chunks         map[[2]int]*bdsNetherChunk
}

func loadBDSEnd(t testing.TB, path string) *bdsEnd {
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
	if _, err := io.ReadFull(r, magic); err != nil || string(magic) != "OMBE1" {
		t.Fatalf("%s: not an End world file", path)
	}
	var hdr [7]int32
	if err := binary.Read(r, binary.LittleEndian, &hdr); err != nil {
		t.Fatal(err)
	}
	w := &bdsEnd{seed: int64(hdr[0]), x0: int(hdr[1]), x1: int(hdr[2]), z0: int(hdr[3]), z1: int(hdr[4]), height: int(hdr[6]), chunks: map[[2]int]*bdsNetherChunk{}}
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
			c := &bdsNetherChunk{ids: make([]uint16, w.height*256), unstable: make([]byte, w.height*256/8)}
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

// bedrockStateString is a state as the End worlds name it: the Bedrock name, then its states sorted by
// key, booleans as 0 and 1.
func bedrockStateString(s bs) string {
	name, props := dragonflyBlock(s.name()).EncodeBlock()
	name = strings.TrimPrefix(name, "minecraft:")
	if len(props) == 0 {
		return name
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var b strings.Builder
	b.WriteString(name + "{")
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		var v string
		switch x := props[k].(type) {
		case bool:
			v = "0"
			if x {
				v = "1"
			}
		case string:
			v = `"` + x + `"`
		default:
			v = fmt.Sprint(x)
		}
		b.WriteString(`"` + k + `":` + v)
	}
	b.WriteByte('}')
	return b.String()
}

// TestEndMatchesBDS scores our End against BDS's, block for block outside the mask: by block, and by
// block with its states. The main islands carry no exit portal (BDS builds it when a player arrives),
// so they are scored on finalCells, which leaves it out.
func TestEndMatchesBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	files, _ := filepath.Glob(filepath.Join(dir, "end-whole", "*.bin.gz"))
	if len(files) == 0 {
		t.Skip("no End worlds in end-whole")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".bin.gz")
		t.Run(name, func(t *testing.T) {
			w := loadBDSEnd(t, f)
			eg := NewWithProfile(w.seed, ProfileBedrock).End()
			sc := newScratch()
			var total, same, stateSame int
			conf := map[[2]string]int{}
			fams := map[string][2]int{}
			wrongAt := map[[2]int]int{}
			for k, c := range w.chunks {
				final := eg.finalCells(sc, k[0], k[1])
				for i := range w.height * 256 {
					if c.masked(i) {
						continue
					}
					real := w.names[c.ids[i]]
					s := final[cellIndex(i&15, i>>8, i>>4&15)]
					mine := bedrockStateString(s)
					rb, _, _ := strings.Cut(real, "{")
					mb, _, _ := strings.Cut(mine, "{")
					total++
					fam := fams[rb]
					fam[1]++
					if rb == mb {
						same++
						fam[0]++
					} else {
						conf[[2]string{rb, mb}]++
						wrongAt[k]++
					}
					fams[rb] = fam
					if real == mine {
						stateSame++
					} else if rb == mb {
						conf[[2]string{real, mine}]++
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
			if len(l) > 8 {
				l = l[:8]
			}
			var fl []string
			for k, v := range fams {
				if k != "air" {
					fl = append(fl, fmt.Sprintf("%s %d/%d", k, v[0], v[1]))
				}
			}
			slices.Sort(fl)
			t.Logf("seed %d %s: blocks same %.4f%% (%d wrong), with states %.4f%%; recall by BDS block %v; top (bds<-ours) %v; wrong by chunk %v",
				w.seed, name, 100*float64(same)/float64(total), total-same, 100*float64(stateSame)/float64(total), fl, l, wrongAt)
		})
	}
}
