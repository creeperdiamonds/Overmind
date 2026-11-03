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
		for _, line := range r.notes {
			fmt.Println("    " + line)
		}
		// The grid figures must reach what Java itself scores against Bedrock on this seed, within the
		// sample's rounding. A seed with no Java figure is reported and not judged.
		if ok := javaFigures[seed]; ok != [2]float64{} && (r.gridBiome < j[0]-figureSlack || r.gridHeight < j[1]-figureSlack) {
			fmt.Printf("    FAIL: seed %d is under the Java vs Bedrock figures %.2f / %.2f\n", seed, j[0], j[1])
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
	notes                                                 []string
}

// check generates every chunk of the window and compares it.
func check(seed int64, m *refMaps) result {
	g := worldgen.New(seed)
	g.SetReuseBases(true) // A tool sweeping a window; it changes speed and no result.
	ground := worldgen.GroundBlocks()
	jobs := make(chan [2]int)
	var mu sync.Mutex
	var allN, allHit, allB, allBHit, gridH, gridHit, gridB, gridBHit, gridNoiseHit int
	confused := map[[2]int]int{}
	var wg sync.WaitGroup
	for range runtime.NumCPU() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				cx, cz := job[0], job[1]
				c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				g.GenerateChunk(world.ChunkPos{int32(cx), int32(cz)}, c)
				var a, ah, b, bh, gh, ghit, gb, gbh, gn int
				local := map[[2]int]int{}
				for lx := range 16 {
					for lz := range 16 {
						x, z := cx*16+lx, cz*16+lz
						i, j := z-m.z0, x-m.x0
						if i < 0 || j < 0 || j >= m.w || i*m.w+j >= len(m.mask) || !m.mask[i*m.w+j] {
							continue
						}
						top := minY - 1
						for y := maxY; y >= minY; y-- {
							if ground[c.Block(uint8(lx), int16(y), uint8(lz), 0)] {
								top = y
								break
							}
						}
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
							if absInt(g.NoiseHeight(x, z)-want) <= 2 {
								gn++
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
				for k, v := range local {
					confused[k] += v
				}
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
