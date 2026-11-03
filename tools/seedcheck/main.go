// Command seedcheck compares our generator with reference Bedrock worlds, seed by seed, at biome and
// height level. The reference folder holds seedN-maps.npz files measured from those worlds.
//
// Stage: a reference height is the highest ground block of a finished world, after carvers and
// features, with logs, leaves, plants, water and snow layers not counted as ground. So ours is taken
// from full GenerateChunk output with the same idea of ground. The noise-stage height, which has no
// carvers, lakes or surface work, is printed beside it, since that is what is reproduced exactly.
//
// A reference biome is read from the block at Y = max(height, 63) + 1 at each quart's low corner, so
// ours is the biome of the quart holding that block, at our own height there.
//
// The height gate (owner's ruling 30). Our carvers are Java's and Bedrock's carver positions are hidden
// in its program, so the two worlds' carver holes never sit at the same points. A grid point is a
// carver hole in a world if that world's finished top is more than 2 blocks under our noise height
// there (for Bedrock, read from heights alone, a terrain difference that leaves Bedrock lower counts
// too). At the points that are a hole in neither world, finished height within 2 must reach the
// noise-stage figure on every seed, and seedcheck exits 1 if it does not. The total over all points
// and the hole counts are printed as a measured comparison and do not fail the run.
package main

import (
	"archive/zip"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"sync"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/overmind/overmind/internal/worldgen"
)

// Java 26.1 against the same worlds on the 32-block grid, for the table (BEDROCK-NOTES section 9).
var javaFigures = map[int64][2]float64{100: {99.92, 97.39}, 1: {98.69, 99.27}, 12345: {99.92, 97.80}, 987654321: {99.76, 99.35}}

type refMaps struct {
	height, biome []int16
	mask          []bool
	w, h, bw, bh  int
	x0, z0        int
}

func main() {
	ref := flag.String("ref", "", "folder with seedN-maps.npz reference files")
	seedsFlag := flag.String("seeds", "100,1,12345,987654321", "comma separated seeds")
	flag.Parse()
	if *ref == "" {
		log.Fatal("seedcheck: -ref is required")
	}
	failed := false
	fmt.Printf("%-10s | %-22s | %-22s | %-22s | %s\n", "seed", "biome % grid/all", "height<=2 % grid/all", "noise height<=2 % grid", "Java vs Bedrock biome/height (grid)")
	for _, s := range regexp.MustCompile(`,`).Split(*seedsFlag, -1) {
		seed, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			log.Fatal(err)
		}
		m, err := readMaps(filepath.Join(*ref, fmt.Sprintf("seed%d-maps.npz", seed)))
		if err != nil {
			log.Fatal(err)
		}
		r := check(seed, m)
		j := javaFigures[seed]
		fmt.Printf("%-10d | %9.2f / %9.2f | %9.2f / %9.2f | %22.2f | %6.2f / %6.2f\n", seed, r.gridBiome, r.allBiome, r.gridHeight, r.allHeight, r.gridNoise, j[0], j[1])
		fmt.Printf("    carver holes on the grid: ours %d, Bedrock %d, both %d (%d points left out); height<=2 away from them %.2f%% (gate: noise %.2f%%)\n",
			r.ours, r.bedrock, r.both, r.ours+r.bedrock-r.both, r.clearHeight, r.gridNoise)
		for _, line := range r.feat.report(seed) {
			fmt.Println("    " + line)
		}
		for _, line := range r.notes {
			fmt.Println("    " + line)
		}
		// The grid biome figure must reach what Java itself scores against Bedrock on this seed, within
		// the sample's rounding; a seed with no Java figure is not judged on it. Away from carver holes,
		// finished height must reach the noise-stage figure (ruling 30).
		if ok := javaFigures[seed]; ok != [2]float64{} && r.gridBiome < j[0]-figureSlack {
			fmt.Printf("    FAIL: seed %d biome is under the Java vs Bedrock figure %.2f\n", seed, j[0])
			failed = true
		}
		if r.clearHeight < r.gridNoise {
			fmt.Printf("    FAIL: seed %d height away from carver holes %.2f%% is under the noise-stage %.2f%%\n", seed, r.clearHeight, r.gridNoise)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}

// figureSlack is one grid point of 1225, in percent: the smallest step the figures can move by.
const figureSlack = 100.0 / 1225

type result struct {
	gridBiome, allBiome, gridHeight, allHeight, gridNoise float64
	// Carver holes on the grid: points where a world's finished top is more than 2 under our noise
	// height, which only a carver (or a lake) makes. ours, bedrock and both count them; clearHeight
	// is the height figure over the points with no hole in either world.
	ours, bedrock, both int
	clearHeight         float64
	feat                *tally
	notes               []string
}

// check generates every chunk of the window and compares it.
func check(seed int64, m *refMaps) result {
	g := worldgen.New(seed)
	g.SetCacheBytes(256 << 20) // A tool sweeping a window keeps more; it changes speed and no result.
	ground := worldgen.GroundBlocks()
	jobs := make(chan [2]int)
	var mu sync.Mutex
	var allN, allHit, allB, allBHit, gridH, gridHit, gridB, gridBHit, gridNoiseHit int
	var holesOurs, holesBedrock, holesBoth, clearN, clearHit int
	confused := map[[2]int]int{}
	feat := newTally()
	info := blockInfos()
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				cx, cz := job[0], job[1]
				c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				g.GenerateChunk(world.ChunkPos{int32(cx), int32(cz)}, c)
				var tops [256]int
				for lx := range 16 {
					for lz := range 16 {
						tops[lx*16+lz] = minY - 1
						for y := maxY; y >= minY; y-- {
							if ground[c.Block(uint8(lx), int16(y), uint8(lz), 0)] {
								tops[lx*16+lz] = y
								break
							}
						}
					}
				}
				ct := chunkTally(c, info, &tops, g.BiomeAt(cx*16+8, maxY, cz*16+8).String())
				var a, ah, b, bh, gh, ghit, gb, gbh, gn int
				var ho, hb, hbo, cn, ch int
				local := map[[2]int]int{}
				for lx := range 16 {
					for lz := range 16 {
						x, z := cx*16+lx, cz*16+lz
						i, j := z-m.z0, x-m.x0
						if i < 0 || j < 0 || j >= m.w || i*m.w+j >= len(m.mask) || !m.mask[i*m.w+j] {
							continue
						}
						top := tops[lx*16+lz]
						want := int(m.height[i*m.w+j])
						near := absInt(top-want) <= 2
						a++
						if near {
							ah++
						}
						onGrid := x&31 == 0 && z&31 == 0
						if onGrid {
							gh++
							if near {
								ghit++
							}
							noise := g.NoiseHeight(x, z)
							if absInt(noise-want) <= 2 {
								gn++
							}
							ourHole, bedrockHole := top < noise-2, want < noise-2
							switch {
							case ourHole && bedrockHole:
								hbo++
								ho++
								hb++
							case ourHole:
								ho++
							case bedrockHole:
								hb++
							default:
								cn++
								if near {
									ch++
								}
							}
						}
						if x&3 == 0 && z&3 == 0 {
							wantB := int(m.biome[(i/4)*m.bw+j/4])
							if wantB < 0 {
								continue
							}
							got := g.BiomeAt(x, max(top, 63)+1, z).EncodeBiome()
							b++
							if got == wantB {
								bh++
							} else {
								local[[2]int{got, wantB}]++
							}
							if onGrid {
								gb++
								if got == wantB {
									gbh++
								}
							}
						}
					}
				}
				mu.Lock()
				allN += a
				allHit += ah
				allB += b
				allBHit += bh
				gridH += gh
				gridHit += ghit
				gridB += gb
				gridBHit += gbh
				gridNoiseHit += gn
				holesOurs += ho
				holesBedrock += hb
				holesBoth += hbo
				clearN += cn
				clearHit += ch
				for k, v := range local {
					confused[k] += v
				}
				feat.add(ct)
				mu.Unlock()
			}
		}()
	}
	for cx := m.x0 >> 4; cx <= (m.x0+m.w-1)>>4; cx++ {
		for cz := m.z0 >> 4; cz <= (m.z0+m.h-1)>>4; cz++ {
			jobs <- [2]int{cx, cz}
		}
	}
	close(jobs)
	wg.Wait()
	r := result{
		gridBiome: pct(gridBHit, gridB), allBiome: pct(allBHit, allB),
		gridHeight: pct(gridHit, gridH), allHeight: pct(allHit, allN), gridNoise: pct(gridNoiseHit, gridH),
		ours: holesOurs, bedrock: holesBedrock, both: holesBoth, clearHeight: pct(clearHit, clearN), feat: feat,
	}
	type pair struct {
		k [2]int
		v int
	}
	for range 4 {
		var best pair
		for k, v := range confused {
			if v > best.v {
				best = pair{k, v}
			}
		}
		if best.v == 0 {
			break
		}
		delete(confused, best.k)
		r.notes = append(r.notes, fmt.Sprintf("biome id %d where Bedrock has %d: %d cells", best.k[0], best.k[1], best.v))
	}
	return r
}

const (
	minY = -64
	maxY = 319
)

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b) * 100
}

// readMaps reads a numpy zip: height, biome and mask arrays, with x0 and z0.
func readMaps(path string) (*refMaps, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	m := &refMaps{}
	for _, f := range z.File {
		r, err := f.Open()
		if err != nil {
			return nil, err
		}
		raw, err := io.ReadAll(r)
		r.Close()
		if err != nil {
			return nil, err
		}
		shape, data, err := npyBody(raw)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f.Name, err)
		}
		switch f.Name {
		case "height.npy":
			m.h, m.w = shape[0], shape[1]
			m.height = int16s(data)
		case "biome.npy":
			m.bh, m.bw = shape[0], shape[1]
			m.biome = int16s(data)
		case "mask.npy":
			m.mask = make([]bool, len(data))
			for i, v := range data {
				m.mask[i] = v != 0
			}
		case "x0.npy":
			m.x0 = int(int64(binary.LittleEndian.Uint64(data)))
		case "z0.npy":
			m.z0 = int(int64(binary.LittleEndian.Uint64(data)))
		}
	}
	if m.height == nil || m.biome == nil || m.mask == nil {
		return nil, fmt.Errorf("%s: arrays missing", path)
	}
	return m, nil
}

var shapeRe = regexp.MustCompile(`'shape': \(([0-9]*),? ?([0-9]*),?\)`)

// npyBody splits a numpy file into its shape and data.
func npyBody(raw []byte) (shape [2]int, data []byte, err error) {
	if len(raw) < 10 || string(raw[1:6]) != "NUMPY" {
		return shape, nil, fmt.Errorf("not a numpy file")
	}
	headerLen := int(binary.LittleEndian.Uint16(raw[8:10]))
	header := raw[10 : 10+headerLen]
	if s := shapeRe.FindSubmatch(header); s != nil {
		shape[0], _ = strconv.Atoi(string(s[1]))
		shape[1], _ = strconv.Atoi(string(s[2]))
	}
	return shape, raw[10+headerLen:], nil
}

func int16s(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}
