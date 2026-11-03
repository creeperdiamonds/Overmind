package worldgen

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Golden tests of the stages after noise compare with fixtures from a real Java server. The fixtures
// are not in this repo: set OVERMIND_GOLDEN_FEATURES to their folder (the study's golden-features), or
// the tests skip. OVERMIND_GOLDEN_STEP samples every Nth chunk each way; the default is 3.

func goldenFeaturesDir(t testing.TB) string {
	dir := os.Getenv("OVERMIND_GOLDEN_FEATURES")
	if dir == "" {
		t.Skip("OVERMIND_GOLDEN_FEATURES is not set: point it at the golden-features folder to run the Java block-exact tests")
	}
	if _, err := os.Stat(filepath.Join(dir, "README.md")); err != nil {
		t.Fatalf("OVERMIND_GOLDEN_FEATURES=%s holds no fixtures: %v", dir, err)
	}
	return dir
}

func goldenStep() int {
	if v, err := strconv.Atoi(os.Getenv("OVERMIND_GOLDEN_STEP")); err == nil && v > 0 {
		return v
	}
	return 3
}

const (
	goldenMinCX, goldenMaxCX = 100, 163
	goldenMinCZ, goldenMaxCZ = -220, -157
)

// readStage reads one stage of a chunk file as block states, by the file's index order.
func readStage(path, stage string) ([]bs, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	r := bufio.NewReaderSize(z, 1<<20)
	line := func() (string, error) {
		s, err := r.ReadString('\n')
		return strings.TrimRight(s, "\r\n"), err
	}
	for {
		h, err := line()
		if err != nil {
			return nil, fmt.Errorf("%s: no stage %s: %w", path, stage, err)
		}
		if !strings.HasPrefix(h, "stage ") {
			continue
		}
		if strings.TrimPrefix(h, "stage ") != stage {
			continue
		}
		p, err := line()
		if err != nil {
			return nil, err
		}
		n, err := strconv.Atoi(strings.TrimPrefix(p, "palette "))
		if err != nil {
			return nil, err
		}
		pal := make([]bs, n)
		for i := range pal {
			name, err := line()
			if err != nil {
				return nil, err
			}
			pal[i] = stateOf(strings.TrimPrefix(name, "minecraft:"))
		}
		runs, err := line()
		if err != nil {
			return nil, err
		}
		out := make([]bs, 0, chunkCells)
		for _, run := range strings.Fields(strings.TrimPrefix(runs, "runs ")) {
			a, b, _ := strings.Cut(run, "x")
			id, _ := strconv.Atoi(a)
			c, _ := strconv.Atoi(b)
			for range c {
				out = append(out, pal[id])
			}
		}
		return out, nil
	}
}

func chunkFile(dir string, seed int64, cx, cz int) string {
	return filepath.Join(dir, fmt.Sprintf("seed_%d_nostruct", seed), "chunks", fmt.Sprintf("c.%d.%d.txt.gz", cx, cz))
}

// diffCounter tallies differences between states for a report.
type diffCounter struct {
	sync.Mutex
	pairs map[string]int
}

func (d *diffCounter) add(got, want bs) {
	d.Lock()
	if d.pairs == nil {
		d.pairs = map[string]int{}
	}
	d.pairs[got.name()+" want "+want.name()]++
	d.Unlock()
}

func (d *diffCounter) top(n int) string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range d.pairs {
		all = append(all, kv{k, v})
	}
	for i := range all {
		for j := i + 1; j < len(all); j++ {
			if all[j].v > all[i].v {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	var b strings.Builder
	for i, e := range all {
		if i == n {
			break
		}
		fmt.Fprintf(&b, "  %d x %s\n", e.v, e.k)
	}
	return b.String()
}

// compareStage runs a stage on sampled chunks of every seed and counts blocks that differ from Java.
func compareStage(t *testing.T, dir, stage string, stageOf baseStage, profile *Profile) {
	step := goldenStep()
	for _, seed := range goldenSeeds {
		g := NewWithProfile(seed, profile)
		type job struct{ cx, cz int }
		jobs := make(chan job)
		var wg sync.WaitGroup
		var chunks, badChunks, blocks, bad, tieChunks, tieBlocks atomic.Int64
		var diffs diffCounter
		var firstBad atomic.Value
		for range runtime.GOMAXPROCS(0) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				s := new(chunkScratch)
				for j := range jobs {
					want, err := readStage(chunkFile(dir, seed, j.cx, j.cz), stage)
					if err != nil {
						t.Error(err)
						continue
					}
					if len(want) != chunkCells {
						t.Errorf("chunk %d,%d stage %s: %d blocks in the fixture", j.cx, j.cz, stage, len(want))
						continue
					}
					g.buildBase(&s.base, s, j.cx, j.cz, stageOf, 0, 15, 0, 15)
					miss := int64(0)
					for i, w := range want {
						if s.base.b[i] != w {
							miss++
						}
					}
					chunks.Add(1)
					blocks.Add(chunkCells)
					if miss > 0 {
						// A difference is allowed only as the result of a biome tie: with the fixture's
						// stored biomes in place of our cold search, the stage must be exact.
						tieChunks.Add(1)
						tieBlocks.Add(miss)
						useFixtureBiomes(t, s, g, dir, seed, j.cx, j.cz)
						g.buildBase(&s.base, s, j.cx, j.cz, stageOf, 0, 15, 0, 15)
						miss = 0
						for i, w := range want {
							if got := s.base.b[i]; got != w {
								miss++
								diffs.add(got, w)
								firstBad.CompareAndSwap(nil, fmt.Sprintf("chunk %d,%d x=%d y=%d z=%d: %s, want %s", j.cx, j.cz, i&15, i>>8+minY, i>>4&15, got.name(), w.name()))
							}
						}
						clear(s.quarts[:])
					}
					if miss > 0 {
						badChunks.Add(1)
						bad.Add(miss)
					}
				}
			}()
		}
		for cx := goldenMinCX; cx <= goldenMaxCX; cx += step {
			for cz := goldenMinCZ; cz <= goldenMaxCZ; cz += step {
				jobs <- job{cx, cz}
			}
		}
		close(jobs)
		wg.Wait()
		if chunks.Load() == 0 {
			t.Fatalf("seed %d: no chunks read", seed)
		}
		t.Logf("%s seed %d: %d chunks; pure from the seed %d exact, %d of %d blocks differ; with the fixture's biome ties %d blocks differ",
			stage, seed, chunks.Load(), chunks.Load()-tieChunks.Load(), tieBlocks.Load(), blocks.Load(), bad.Load())
		if float64(tieBlocks.Load())/float64(blocks.Load()) > 1e-5 {
			t.Errorf("%s seed %d: %d blocks differ from biome ties, more than the ties explain", stage, seed, tieBlocks.Load())
		}
		if bad.Load() > 0 {
			t.Errorf("%s seed %d: %d blocks differ in %d chunks; first: %v\n%s", stage, seed, bad.Load(), badChunks.Load(), firstBad.Load(), diffs.top(12))
		}
	}
}

// useFixtureBiomes puts a chunk's and its neighbours' stored biomes from the fixtures into the scratch.
func useFixtureBiomes(t *testing.T, s *chunkScratch, g *Generator, dir string, seed int64, cx, cz int) {
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			nx, nz := cx+dx, cz+dz
			if nx < goldenMinCX || nx > goldenMaxCX || nz < goldenMinCZ || nz > goldenMaxCZ {
				continue
			}
			ids, err := readBiomes(chunkFile(dir, seed, nx, nz))
			if err != nil {
				t.Error(err)
				return
			}
			for qz := range 4 {
				for qx := range 4 {
					q := s.quartColumnAt(g, nx*4+qx, nz*4+qz)
					for qy := range q.ids {
						q.ids[qy] = ids[qy*16+qz*4+qx]
					}
				}
			}
		}
	}
}

func TestGoldenNoiseStage(t *testing.T) {
	compareStage(t, goldenFeaturesDir(t), "noise", stageNoise, ProfileOverjava)
}

func TestGoldenSurfaceStage(t *testing.T) {
	compareStage(t, goldenFeaturesDir(t), "surface", stageSurface, ProfileOverjava)
}

// readBiomes reads a chunk file's stored biomes as table indexes, by qy*16 + qz*4 + qx.
func readBiomes(path string) ([]uint8, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	r := bufio.NewReaderSize(z, 1<<16)
	line := func() string {
		s, _ := r.ReadString('\n')
		return strings.TrimRight(s, "\r\n")
	}
	for {
		h := line()
		if h == "" {
			return nil, fmt.Errorf("%s: no biomes", path)
		}
		if h != "stage biomes" {
			continue
		}
		n, _ := strconv.Atoi(strings.TrimPrefix(line(), "palette "))
		pal := make([]uint8, n)
		for i := range pal {
			pal[i] = biomeIdx[strings.TrimPrefix(line(), "minecraft:")]
		}
		var out []uint8
		for _, run := range strings.Fields(strings.TrimPrefix(line(), "runs ")) {
			a, b, _ := strings.Cut(run, "x")
			id, _ := strconv.Atoi(a)
			c, _ := strconv.Atoi(b)
			for range c {
				out = append(out, pal[id])
			}
		}
		return out, nil
	}
}

// TestGoldenBiomeStage compares stored biomes. Our lookup is the cold search; a difference here is an
// exact tie that the game's per-thread remembered result decided the other way, and is logged, not
// failed, unless it passes the study's measured rate of about 0.05%.
func TestGoldenBiomeStage(t *testing.T) {
	dir := goldenFeaturesDir(t)
	step := goldenStep()
	for _, seed := range goldenSeeds {
		g := NewWithProfile(seed, ProfileOverjava)
		s := new(chunkScratch)
		total, miss := 0, 0
		first := ""
		for cx := goldenMinCX; cx <= goldenMaxCX; cx += step {
			for cz := goldenMinCZ; cz <= goldenMaxCZ; cz += step {
				want, err := readBiomes(chunkFile(dir, seed, cx, cz))
				if err != nil {
					t.Fatal(err)
				}
				if len(want) != 96*16 {
					t.Fatalf("chunk %d,%d: %d biomes in the fixture", cx, cz, len(want))
				}
				for qy := range 96 {
					for qz := range 4 {
						for qx := range 4 {
							total++
							got := s.quartColumnAt(g, cx*4+qx, cz*4+qz).ids[qy]
							if w := want[qy*16+qz*4+qx]; got != w {
								miss++
								if first == "" {
									first = fmt.Sprintf("chunk %d,%d q %d,%d,%d: %s, want %s", cx, cz, qx, qy, qz, biomeTableNames[got], biomeTableNames[w])
								}
							}
						}
					}
				}
			}
		}
		t.Logf("biomes seed %d: %d of %d quarts differ. %s", seed, miss, total, first)
		if float64(miss)/float64(total) > 0.001 {
			t.Errorf("seed %d: %d of %d quarts differ, over the tie rate", seed, miss, total)
		}
	}
}
