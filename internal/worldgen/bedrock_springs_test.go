package worldgen

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// springWorld finds the study's seed 5 BDS world (biomes/worlds/5, a leveldb folder per run) inside
// or next to a folder of OVERMIND_BDS_WORLDS; the test skips without it.
func springWorld(t *testing.T) string {
	for _, d := range strings.Split(os.Getenv("OVERMIND_BDS_WORLDS"), ";") {
		if d == "" {
			continue
		}
		for _, p := range []string{filepath.Join(d, "5"), filepath.Join(filepath.Dir(d), "5")} {
			if st, err := os.Stat(filepath.Join(p, "big", "db")); err == nil && st.IsDir() {
				return p
			}
		}
	}
	t.Skip("seed 5's BDS world (biomes/worlds/5) not found: set OVERMIND_BDS_WORLDS")
	return ""
}

// levelChunk is a BDS chunk as block name ids by cell, with the cells another run disagrees on.
type levelChunk struct {
	ids      []uint16
	unstable []bool
}

// loadLevelDB reads BDS runs' chunks (x0..x1, z0..z1) into one set: the first run's blocks, a cell
// unstable where a later run differs. Each run is read from a copy (the folder may be locked;
// Dragonfly makes its own level.dat).
func loadLevelDB(t *testing.T, dirs []string, x0, x1, z0, z1 int, ids map[string]uint16, list *[]string) map[[2]int]*levelChunk {
	idOf := func(n string) uint16 {
		if v, ok := ids[n]; ok {
			return v
		}
		ids[n] = uint16(len(*list))
		*list = append(*list, n)
		return ids[n]
	}
	names := blockNames()
	out := map[[2]int]*levelChunk{}
	for ri, dir := range dirs {
		tmp := t.TempDir()
		err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(dir, p)
			dst := filepath.Join(tmp, rel)
			if info.IsDir() {
				return os.MkdirAll(dst, 0o755)
			}
			if info.Name() == "LOCK" || strings.HasPrefix(info.Name(), "level.dat") {
				return nil
			}
			in, err := os.Open(p)
			if err != nil {
				return err
			}
			defer in.Close()
			o, err := os.Create(dst)
			if err != nil {
				return err
			}
			defer o.Close()
			_, err = io.Copy(o, in)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		db, err := mcdb.Open(tmp)
		if err != nil {
			t.Fatal(err)
		}
		for cx := x0; cx <= x1; cx++ {
			for cz := z0; cz <= z1; cz++ {
				col, err := db.LoadColumn(world.ChunkPos{int32(cx), int32(cz)}, world.Overworld)
				if err != nil || col == nil {
					continue
				}
				c := out[[2]int{cx, cz}]
				if ri == 0 {
					c = &levelChunk{ids: make([]uint16, chunkCells), unstable: make([]bool, chunkCells)}
					out[[2]int{cx, cz}] = c
				} else if c == nil {
					continue
				}
				for y := minY; y <= maxY; y++ {
					for z := range 16 {
						for x := range 16 {
							id, cell := idOf(names[col.Chunk.Block(uint8(x), int16(y), uint8(z), 0)].name), cellIndex(x, y, z)
							if ri == 0 {
								c.ids[cell] = id
							} else if c.ids[cell] != id {
								c.unstable[cell] = true
							}
						}
					}
				}
			}
		}
		db.Close()
	}
	return out
}

// TestSulfurSpringsMatchBDS: the sulfur springs of seed 5's BDS world (NOTES-leftovers L11.6: three,
// potent sulfur over magma at (-249,88,-405), (-238,82,-425) and (-120,79,-271), templates small_4,
// small_4 and small_3, which the study's matcher found at 91-92% of their cells). Scored without and
// with the springs: the cells around each BDS spring (x and z within 8, y 10 under to 6 over the potent
// cell) that agree with BDS, every cell of the loaded area (cells the big and strips runs disagree on
// left out), and where ours put potent sulfur over magma.
func TestSulfurSpringsMatchBDS(t *testing.T) {
	bd := testBedrockData(t)
	dir := springWorld(t)
	const x0, x1, z0, z1 = -18, -4, -27, -13
	ids, list := map[string]uint16{}, []string{}
	w := loadLevelDB(t, []string{filepath.Join(dir, "big"), filepath.Join(dir, "strips")}, x0, x1, z0, z1, ids, &list)
	bd0 := dataWithout(t, bd, "minecraft:sulfur_spring_trail_to_surface_feature")
	gens := [2]*Generator{NewWithData(5, ProfileBedrock, bd0), NewWithData(5, ProfileBedrock, bd)}
	var tries []string
	trailTrace = func(p, at pos, ok bool) {
		tries = append(tries, fmt.Sprintf("%v->%v %v", p, at, ok))
	}
	defer func() { trailTrace = nil }()
	springs := [][3]int{{-249, 88, -405}, {-238, 82, -425}, {-120, 79, -271}}
	var boxSame [3][2]int
	var boxN [3]int
	var same [2]int
	var cells int
	var placed [2][][3]int
	hit := [3]bool{}
	names := blockNames()
	for k, c := range w {
		var ours [2][]string
		for i, g := range gens {
			ch := generate(g, int32(k[0]), int32(k[1]))
			ours[i] = make([]string, chunkCells)
			for y := minY; y <= maxY; y++ {
				for z := range 16 {
					for x := range 16 {
						ours[i][cellIndex(x, y, z)] = names[ch.Block(uint8(x), int16(y), uint8(z), 0)].name
					}
				}
			}
			for cell := 256; cell < chunkCells; cell++ {
				if ours[i][cell] == "potent_sulfur" && ours[i][cell-256] == "magma" {
					placed[i] = append(placed[i], [3]int{k[0]*16 + cell&15, cell>>8 + minY, k[1]*16 + cell>>4&15})
				}
			}
		}
		for cell := range chunkCells - 256 { // Y 319 holds the study's loading marker.
			if c.unstable[cell] {
				continue
			}
			cells++
			real := list[c.ids[cell]]
			for i := range ours {
				if ours[i][cell] == real {
					same[i]++
				}
			}
		}
		for si, s := range springs {
			for x := max(s[0]-8, k[0]*16); x <= min(s[0]+8, k[0]*16+15); x++ {
				for z := max(s[2]-8, k[1]*16); z <= min(s[2]+8, k[1]*16+15); z++ {
					for y := s[1] - 10; y <= s[1]+6; y++ {
						cell := cellIndex(x&15, y, z&15)
						if c.unstable[cell] {
							continue
						}
						boxN[si]++
						for i := range ours {
							if ours[i][cell] == list[c.ids[cell]] {
								boxSame[si][i]++
							}
						}
						if x == s[0] && y == s[1] && z == s[2] {
							if list[c.ids[cell]] != "potent_sulfur" {
								t.Errorf("BDS has no potent sulfur at %v", s)
							}
							hit[si] = ours[1][cell] == "potent_sulfur"
						}
					}
				}
			}
		}
	}
	var lines []string
	exact := 0
	for si, s := range springs {
		if hit[si] {
			exact++
		}
		lines = append(lines, fmt.Sprintf("spring %v: %.2f%% -> %.2f%% of %d cells around it; ours has its potent sulfur: %v",
			s, 100*float64(boxSame[si][0])/float64(max(boxN[si], 1)), 100*float64(boxSame[si][1])/float64(max(boxN[si], 1)), boxN[si], hit[si]))
	}
	t.Logf("seed 5 springs: %d of 3 BDS springs have our potent sulfur at their cell; ours place %d springs without the trail, %d with: %v\n%s\n"+
		"whole area (%d chunks): %.4f%% -> %.4f%% of %d stable cells",
		exact, len(placed[0]), len(placed[1]), placed[1], strings.Join(lines, "\n"), len(w),
		100*float64(same[0])/float64(max(cells, 1)), 100*float64(same[1])/float64(max(cells, 1)), cells)
	slices.Sort(tries)
	tries = slices.Compact(tries)
	t.Logf("spring tries (snap -> scan cell, placed): %d\n%s", len(tries), strings.Join(tries, "\n"))
	if len(placed[0]) != 0 {
		t.Errorf("without the trail ours place %d springs", len(placed[0]))
	}
}

// dataWithout reads the install again with some code features left unwritten (testUnwritten).
func dataWithout(t *testing.T, bd *BedrockData, ids ...string) *BedrockData {
	testUnwritten = map[string]bool{}
	for _, id := range ids {
		testUnwritten[id] = true
	}
	defer func() { testUnwritten = nil }()
	bd0, err := LoadBedrockData(bd.Dir)
	if err != nil {
		t.Fatal(err)
	}
	return bd0
}
