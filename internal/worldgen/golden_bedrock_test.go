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
	"sync"
	"testing"
)

// Golden tests of the Bedrock profile compare whole chunks with worlds from Bedrock Dedicated Server
// 1.26.60.6 (Task 4e, rulings 35 to 37). The worlds are not in this repo: set OVERMIND_GOLDEN_BEDROCK
// to the study's golden-bedrock folder, whose whole/seed<N>.bin.gz files hold chunks -10..9 of each
// test seed with the mask of blocks BDS itself does not reproduce between runs. The tests skip if it
// is unset and fail if the folder has no worlds.

// bedrockTestSeeds are the four test seeds of ruling 35.
var bedrockTestSeeds = []int64{100, 1, 12345, 987654321}

func goldenBedrockDir(t testing.TB) string {
	dir := os.Getenv("OVERMIND_GOLDEN_BEDROCK")
	if dir == "" {
		t.Skip("OVERMIND_GOLDEN_BEDROCK is not set: point it at the golden-bedrock folder to run the Bedrock block-exact tests")
	}
	if _, err := os.Stat(filepath.Join(dir, "whole")); err != nil {
		t.Fatalf("OVERMIND_GOLDEN_BEDROCK=%s holds no whole worlds: %v", dir, err)
	}
	return dir
}

// bdsChunk is one chunk of a BDS world: block names by index, with the unstable mask.
type bdsChunk struct {
	ids      []uint16 // cellIndex order: y, z, x.
	unstable []byte   // A bit per cell.
	biomes   []uint16
}

func (c *bdsChunk) masked(cell int) bool { return c.unstable[cell>>3]>>(cell&7)&1 != 0 }

type bdsWorld struct {
	seed           int64
	x0, x1, z0, z1 int // Chunk box, ends exclusive.
	window         string
	names          []string
	chunks map[[2]int]*bdsChunk
}

var bdsWorlds sync.Map

// loadBDSWorld reads a seed's world once per test binary.
func loadBDSWorld(t testing.TB, dir string, seed int64) *bdsWorld {
	return loadBDSFile(t, filepath.Join(dir, "whole", fmt.Sprintf("seed%d.bin.gz", seed)))
}

// loadBDSFile reads a whole-world file: OMBW1 (a square of chunks) or OMBW2 (a box).
func loadBDSFile(t testing.TB, path string) *bdsWorld {
	if w, ok := bdsWorlds.Load(path); ok {
		return w.(*bdsWorld)
	}
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
	if _, err := io.ReadFull(r, magic); err != nil || (string(magic) != "OMBW1" && string(magic) != "OMBW2") {
		t.Fatalf("%s: not a whole-world file", path)
	}
	var w *bdsWorld
	var count int32
	if string(magic) == "OMBW1" {
		var hdr [4]int32
		if err := binary.Read(r, binary.LittleEndian, &hdr); err != nil {
			t.Fatal(err)
		}
		w = &bdsWorld{seed: int64(hdr[0]), x0: int(hdr[1]), x1: int(hdr[2]), z0: int(hdr[1]), z1: int(hdr[2])}
		count = hdr[3]
	} else {
		var hdr [6]int32
		if err := binary.Read(r, binary.LittleEndian, &hdr); err != nil {
			t.Fatal(err)
		}
		w = &bdsWorld{seed: int64(hdr[0]), x0: int(hdr[1]), x1: int(hdr[2]), z0: int(hdr[3]), z1: int(hdr[4])}
		count = hdr[5]
	}
	w.chunks = map[[2]int]*bdsChunk{}
	for range count {
		var n uint16
		binary.Read(r, binary.LittleEndian, &n)
		b := make([]byte, n)
		io.ReadFull(r, b)
		w.names = append(w.names, strings.TrimPrefix(string(b), "minecraft:"))
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
			c := &bdsChunk{ids: make([]uint16, chunkCells), unstable: make([]byte, chunkCells/8), biomes: make([]uint16, chunkCells)}
			if err := binary.Read(r, binary.LittleEndian, c.ids); err != nil {
				t.Fatal(err)
			}
			if _, err := io.ReadFull(r, c.unstable); err != nil {
				t.Fatal(err)
			}
			if err := binary.Read(r, binary.LittleEndian, c.biomes); err != nil {
				t.Fatal(err)
			}
			w.chunks[[2]int{cx, cz}] = c
		}
	}
	bdsWorlds.Store(path, w)
	return w
}

// structureNames mark blocks of structures, which are not generated yet: a chunk holding more than 200
// of them is left out of the score, as in the study's scoreboard (NOTES-bedrock-exact Q5).
var structureNames = []string{"tuff_bricks", "copper", "polished_tuff", "deepslate_bricks", "deepslate_tiles", "planks",
	"cobblestone", "rail", "chest", "cobweb", "stone_bricks", "reinforced", "sculk_shrieker"}

func isStructureName(n string) bool {
	if strings.Contains(n, "mossy_cobblestone") {
		return false
	}
	for _, k := range structureNames {
		if strings.Contains(n, k) {
			return true
		}
	}
	return false
}

// structureBoxes are areas known to hold a structure, left out of the score until structures are
// generated: seed 100's ancient city (coordinator's note, 2026-11-04).
var structureBoxes = map[int64][][4]int{100: {{0, 8, -13, -5}}}

// skipForStructure says whether a chunk is left out of the score.
func (w *bdsWorld) skipForStructure(cx, cz int) bool {
	for _, b := range structureBoxes[w.seed] {
		if cx >= b[0] && cx <= b[1] && cz >= b[2] && cz <= b[3] {
			return true
		}
	}
	c := w.chunks[[2]int{cx, cz}]
	n := 0
	for _, id := range c.ids {
		if isStructureName(w.names[id]) {
			n++
		}
	}
	return n > 200
}

// generatedChunk is a generated chunk as block names in cellIndex order, with its carvers stage's
// host rock class (stoneClass) per cell.
type generatedChunk struct {
	names []string
	base  []byte
}

// generateNames generates chunks.
func generateNames(g *Generator, pos [][2]int32) map[[2]int]generatedChunk {
	out := make(map[[2]int]generatedChunk, len(pos))
	var mu sync.Mutex
	names := blockNames()
	inTiles(pos, func(p [2]int32) {
		s := newScratch()
		var b baseChunk
		g.buildBase(&b, s, int(p[0]), int(p[1]), stageCarvers, 0, 15, 0, 15)
		base := make([]byte, chunkCells)
		for i, st := range b.b {
			base[i] = stoneClass(strings.SplitN(st.name(), "[", 2)[0])
		}
		c := generate(g, p[0], p[1])
		cells := make([]string, chunkCells)
		for y := minY; y <= maxY; y++ {
			for z := range 16 {
				for x := range 16 {
					cells[cellIndex(x, y, z)] = names[c.Block(uint8(x), int16(y), uint8(z), 0)].name
				}
			}
		}
		mu.Lock()
		out[[2]int{int(p[0]), int(p[1])}] = generatedChunk{cells, base}
		mu.Unlock()
	})
	return out
}

// bedrockFamily groups a BDS block name for the per-family table, as the study's scoreboard does.
func bedrockFamily(n string) string {
	if n == "grass_block" {
		return "soil"
	}
	fams := []struct {
		f  string
		ks []string
	}{
		{"ore", []string{"_ore"}}, {"rock", []string{"granite", "diorite", "andesite", "tuff", "calcite"}},
		{"log", []string{"_log", "_wood", "mushroom_stem", "mangrove_roots", "brown_mushroom_block", "red_mushroom_block"}},
		{"leaves", []string{"leaves"}}, {"vine", []string{"vine"}}, {"sculk", []string{"sculk"}}, {"lichen", []string{"glow_lichen"}},
		{"dripstone", []string{"dripstone"}}, {"lush", []string{"moss", "azalea", "cave_vines", "spore_blossom", "dripleaf", "clay"}},
		{"water", []string{"water"}}, {"lava", []string{"lava"}}, {"ice_snow", []string{"ice", "snow"}},
		{"plants", []string{"grass", "fern", "flower", "tulip", "poppy", "dandelion", "daisy", "orchid", "allium", "bluet",
			"cornflower", "lily", "rose", "peony", "lilac", "sunflower", "bush", "kelp", "seagrass", "petals", "litter", "pumpkin",
			"melon", "reeds", "cactus", "berry", "bamboo", "deadbush", "wildflowers", "sapling", "cocoa", "pickle", "coral",
			"eyeblossom", "mushroom", "pale_moss"}},
		{"soil", []string{"dirt", "podzol", "mycelium", "mud", "sand", "gravel", "coarse", "terracotta", "sandstone"}},
		{"stone", []string{"stone", "deepslate", "bedrock", "netherrack"}}, {"air", []string{"air"}},
	}
	for _, f := range fams {
		for _, k := range f.ks {
			if strings.Contains(n, k) {
				return f.f
			}
		}
	}
	return "other"
}

// bedrockScore is a seed's whole-block comparison on the stable mask.
type bedrockScore struct {
	seed            int64
	skipped         int
	total, same     int
	famTotal        map[string]int
	famSame         map[string]int
	confusions      map[string]map[[2]string]int
	floorN, floorOK int // Bedrock floor, Y -64 to -57: bedrock or not.
	// Deepslate gradient, Y -8 to 9: stone or deepslate class of BDS's block against our carvers stage,
	// where both have one, as the study measured it (R2), so features placed differently do not count.
	deepN, deepOK int
	deepMiss        []string
}

// stoneClass gives a host rock class for the deepslate check: 'd' for deepslate and its ores, 's' for
// stone and its ores, 0 for anything else.
func stoneClass(n string) byte {
	switch {
	case n == "deepslate" || strings.HasPrefix(n, "deepslate_") && strings.HasSuffix(n, "_ore"):
		return 'd'
	case n == "stone" || strings.HasSuffix(n, "_ore") && !strings.HasPrefix(n, "deepslate_"):
		return 's'
	}
	return 0
}

// scoreBedrockSeed generates a seed's chunks with the Bedrock profile and compares them with BDS.
func scoreBedrockSeed(t testing.TB, dir string, seed int64) *bedrockScore {
	return scoreBDSWorld(t, loadBDSWorld(t, dir, seed), true)
}

// scoreBDSWorld scores a loaded world, leaving out structure chunks if asked.
func scoreBDSWorld(t testing.TB, w *bdsWorld, skipStructures bool) *bedrockScore {
	seed := w.seed
	var pos [][2]int32
	for cz := w.z0; cz < w.z1; cz++ {
		for cx := w.x0; cx < w.x1; cx++ {
			if _, ok := w.chunks[[2]int{cx, cz}]; ok && (!skipStructures || !w.skipForStructure(cx, cz)) {
				pos = append(pos, [2]int32{int32(cx), int32(cz)})
			}
		}
	}
	got := generateNames(NewWithProfile(seed, ProfileBedrock), pos)
	sc := &bedrockScore{seed: seed, skipped: len(w.chunks) - len(pos), famTotal: map[string]int{}, famSame: map[string]int{},
		confusions: map[string]map[[2]string]int{}}
	for _, p := range pos {
		k := [2]int{int(p[0]), int(p[1])}
		bc, ours := w.chunks[k], got[k]
		for cell := range chunkCells {
			y := cell>>8 + minY
			if y == maxY || bc.masked(cell) { // Y 319 holds the study's loading marker.
				continue
			}
			real, mine := w.names[bc.ids[cell]], ours.names[cell]
			f := bedrockFamily(real)
			sc.total++
			sc.famTotal[f]++
			if real == mine {
				sc.same++
				sc.famSame[f]++
			} else {
				if sc.confusions[f] == nil {
					sc.confusions[f] = map[[2]string]int{}
				}
				sc.confusions[f][[2]string{real, mine}]++
			}
			if y <= minY+7 {
				sc.floorN++
				if (real == "bedrock") == (mine == "bedrock") {
					sc.floorOK++
				}
			}
			if y >= -8 && y <= 9 {
				if a, b := stoneClass(real), ours.base[cell]; a != 0 && b != 0 {
					sc.deepN++
					if a == b {
						sc.deepOK++
					} else if len(sc.deepMiss) < 12 {
						sc.deepMiss = append(sc.deepMiss, fmt.Sprintf("(%d,%d,%d) %s<-%c", int(p[0])*16+cell&15, y, int(p[1])*16+cell>>4&15, real, b))
					}
				}
			}
		}
	}
	return sc
}

func (sc *bedrockScore) report(t testing.TB) {
	t.Logf("seed %d: %d chunks skipped (structures); stable blocks same %d/%d = %.4f%%, wrong %d",
		sc.seed, sc.skipped, sc.same, sc.total, 100*float64(sc.same)/float64(max(sc.total, 1)), sc.total-sc.same)
	fams := make([]string, 0, len(sc.famTotal))
	for f := range sc.famTotal {
		fams = append(fams, f)
	}
	slices.SortFunc(fams, func(a, b string) int { return (sc.famTotal[b] - sc.famSame[b]) - (sc.famTotal[a] - sc.famSame[a]) })
	for _, f := range fams {
		type pair struct {
			k [2]string
			n int
		}
		var ps []pair
		for k, n := range sc.confusions[f] {
			ps = append(ps, pair{k, n})
		}
		slices.SortFunc(ps, func(a, b pair) int { return b.n - a.n })
		var top []string
		for i := 0; i < len(ps) && i < 4; i++ {
			top = append(top, fmt.Sprintf("%s<-%s:%d", ps[i].k[0], ps[i].k[1], ps[i].n))
		}
		t.Logf("  %-9s %9d same %7.3f%% wrong %8d  %s", f, sc.famTotal[f], 100*float64(sc.famSame[f])/float64(sc.famTotal[f]),
			sc.famTotal[f]-sc.famSame[f], strings.Join(top, ", "))
	}
	t.Logf("  bedrock floor %d/%d, deepslate class %d/%d %v", sc.floorOK, sc.floorN, sc.deepOK, sc.deepN, sc.deepMiss)
}

// TestBedrockWorldsMatchBDS needs OVERMIND_BEDROCK_DATA too: without the data the Bedrock profile
// generates its features with the built-in placement. It reports the whole-block match per seed on the stable mask, and holds the
// families the study found exact to every block: the bedrock floor (R1) and the deepslate gradient (R2).
func TestBedrockWorldsMatchBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	defer func() {
		// The placement budget (bedrock_limits.go) must never touch vanilla data.
		m := mostPlacements.Load()
		t.Logf("most scatter placements by one source: %d (budget %d)", m, placementBudget)
		if m > placementBudget/8 {
			t.Errorf("a vanilla source made %d placements, within 8x of the budget %d", m, placementBudget)
		}
	}()
	for _, seed := range bedrockTestSeeds {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			sc := scoreBedrockSeed(t, dir, seed)
			sc.report(t)
			if sc.floorOK != sc.floorN {
				t.Errorf("bedrock floor: %d of %d blocks differ from BDS", sc.floorN-sc.floorOK, sc.floorN)
			}
			if sc.deepOK != sc.deepN {
				t.Errorf("deepslate gradient: %d of %d host blocks differ from BDS", sc.deepN-sc.deepOK, sc.deepN)
			}
		})
	}
}

// TestBedrockWindowsMatchBDS scores the study's biome windows (golden-bedrock/whole/windows, from
// golden-bedrock/biomes: 9 x 9 chunks of one biome each, with their own unstable masks), with no
// structure skipping, as the study scores them. It holds the floor and the deepslate gradient exact
// and reports the rest.
func TestBedrockWindowsMatchBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	files, _ := filepath.Glob(filepath.Join(dir, "whole", "windows", "*.bin.gz"))
	if len(files) == 0 {
		t.Skip("no windows exported in whole/windows")
	}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".bin.gz")
		t.Run(name, func(t *testing.T) {
			sc := scoreBDSWorld(t, loadBDSFile(t, f), false)
			sc.report(t)
			if sc.floorOK != sc.floorN {
				t.Errorf("bedrock floor: %d of %d blocks differ from BDS", sc.floorN-sc.floorOK, sc.floorN)
			}
			if sc.deepOK != sc.deepN {
				t.Errorf("deepslate gradient: %d of %d host blocks differ from BDS", sc.deepN-sc.deepOK, sc.deepN)
			}
		})
	}
}
