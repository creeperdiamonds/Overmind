package worldgen

import (
	"bufio"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// The trace fixtures hold a single chunk's real decoration, feature by feature, against a carvers-stage
// background. Each feature is replayed alone: start from carvers plus all earlier features' real
// changes, run ours with its seed, and compare block for block. The order of writes is not compared.

type traceChange struct {
	x, y, z  int
	old, new bs
}

type traceFeature struct {
	step, idx int
	name      string
	changes   []traceChange
}

func readTrace(t *testing.T, path string) (cx, cz int, list []traceFeature) {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	r := bufio.NewScanner(z)
	r.Buffer(make([]byte, 1<<20), 1<<20)
	r.Scan()
	first := strings.Fields(r.Text())
	cx, _ = strconv.Atoi(first[1])
	cz, _ = strconv.Atoi(first[2])
	for r.Scan() {
		line := r.Text()
		if strings.HasPrefix(line, "feature ") {
			p := strings.Fields(line)
			step, _ := strconv.Atoi(p[1])
			idx, _ := strconv.Atoi(p[2])
			list = append(list, traceFeature{step: step, idx: idx, name: p[3]})
			continue
		}
		a, b, _ := strings.Cut(line, " -> ")
		p := strings.SplitN(a, " ", 4)
		x, _ := strconv.Atoi(p[0])
		y, _ := strconv.Atoi(p[1])
		zz, _ := strconv.Atoi(p[2])
		e := &list[len(list)-1]
		e.changes = append(e.changes, traceChange{x, y, zz, stateOf(strings.TrimPrefix(p[3], "minecraft:")), stateOf(strings.TrimPrefix(b, "minecraft:"))})
	}
	return cx, cz, list
}

// featureScore tallies one placed feature over all traces.
type featureScore struct {
	runs, exact, placedRuns, placedExact, implemented int
	missBlocks, totalBlocks                           int
	first                                             string
}

func traceFiles(t *testing.T, dir string, seed int64) []string {
	files, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("trace_seed_%d", seed), "trace", "*.gz"))
	if len(files) == 0 {
		t.Fatalf("no trace files for seed %d", seed)
	}
	return files
}

// traceReplay runs the per-feature replay over every trace of every seed.
func traceReplay(t *testing.T, dir string, scores map[string]*featureScore) {
	for _, seed := range goldenSeeds {
		g := NewWithProfile(seed, ProfileOverjava)
		s := scratchPool.Get().(*chunkScratch)
		byName := map[string]*placement{}
		for _, pl := range g.profile.placements {
			byName[pl.name] = pl
		}
		for _, file := range traceFiles(t, dir, seed) {
			cx, cz, list := readTrace(t, file)
			d := s.startDecoration(g, cx, cz)
			d.cx, d.cz = cx, cz
			d.gen++
			d.r = featRand{}
			d.r.setSeed(seed)
			a, b := d.r.nextLong()|1, d.r.nextLong()|1
			d.decSeed = int64(cx*16)*a + int64(cz*16)*b ^ seed
			l := d.chunkAt(cx, cz)
			d.mapsReady(l)
			l.primed = true
			for _, tf := range list {
				e := scores[tf.name]
				if e == nil {
					e = &featureScore{}
					scores[tf.name] = e
				}
				e.runs++
				if len(tf.changes) > 0 {
					e.placedRuns++
				}
				e.totalBlocks += len(tf.changes)
				var got map[[3]int]bs
				pl := byName[tf.name]
				start := len(d.log)
				d.log = d.log[:start]
				if pl != nil && pl.feat != nil {
					e.implemented++
					if pl.step != tf.step || pl.idx != tf.idx {
						t.Fatalf("%s is step %d index %d here, %d %d in the trace", tf.name, pl.step, pl.idx, tf.step, tf.idx)
					}
					d.r.setSeed(d.decSeed + int64(tf.idx) + int64(10000*tf.step))
					d.run(pl.mods, pl.feat, pos{cx * 16, minY, cz * 16})
					got = map[[3]int]bs{}
					for _, ch := range d.log[start:] {
						lv := &d.live[ch.chunk]
						x := lv.b.cx*16 + int(ch.cell)&15
						z := lv.b.cz*16 + int(ch.cell)>>4&15
						y := int(ch.cell)>>8 + minY
						if _, seen := got[[3]int{x, y, z}]; !seen {
							got[[3]int{x, y, z}] = ch.old
						}
					}
				}
				if dbg := os.Getenv("OVERMIND_TRACE_DEBUG"); dbg != "" && dbg == fmt.Sprintf("%d,%d,%d,%s", seed, cx, cz, tf.name) {
					var lines []string
					for k := range got {
						lines = append(lines, fmt.Sprintf("ours %5d %4d %5d %s", k[1], k[0], k[2], d.block(k[0], k[1], k[2]).name()))
					}
					for _, c := range tf.changes {
						lines = append(lines, fmt.Sprintf("java %5d %4d %5d %s", c.y, c.x, c.z, c.new.name()))
					}
					sort.Strings(lines)
					t.Logf("%s", strings.Join(lines, "\n"))
				}
				miss := 0
				firstBad := ""
				want := map[[3]int]bs{}
				for _, c := range tf.changes {
					want[[3]int{c.x, c.y, c.z}] = c.new
				}
				if pl != nil && pl.feat != nil {
					for k, old := range got {
						now := d.block(k[0], k[1], k[2])
						if w, ok := want[k]; ok {
							if now != w {
								miss++
								if firstBad == "" {
									firstBad = fmt.Sprintf("%v: %s, want %s", k, now.name(), w.name())
								}
							}
						} else if now != old {
							miss++
							if firstBad == "" {
								firstBad = fmt.Sprintf("%v in %s: %s, Java left %s", k, biomeTableNames[d.biomeAt(k[0], k[1], k[2])], now.name(), old.name())
							}
						}
					}
					for k, w := range want {
						if _, ok := got[k]; !ok {
							miss++
							if firstBad == "" {
								firstBad = fmt.Sprintf("%v: not written, want %s", k, w.name())
							}
						}
					}
					if miss == 0 {
						e.exact++
						if len(tf.changes) > 0 {
							e.placedExact++
						}
					} else {
						e.missBlocks += miss
						if e.first == "" {
							e.first = fmt.Sprintf("seed %d chunk %d,%d: %s", seed, cx, cz, firstBad)
						}
					}
					// Undo ours, then apply Java's, so the next feature starts right.
					for i := len(d.log) - 1; i >= start; i-- {
						ch := d.log[i]
						d.live[ch.chunk].b.b[ch.cell] = ch.old
					}
					d.log = d.log[:start]
				}
				for _, c := range tf.changes {
					if !d.canWrite(c.x, c.y, c.z) {
						continue
					}
					d.set(c.x, c.y, c.z, c.new)
				}
			}
			d.rollback()
			d.log = d.log[:0]
		}
	}
}

func TestGoldenFeatureTraces(t *testing.T) {
	dir := goldenFeaturesDir(t)
	scores := map[string]*featureScore{}
	traceReplay(t, dir, scores)
	names := make([]string, 0, len(scores))
	for n := range scores {
		names = append(names, n)
	}
	sort.Strings(names)
	tr, te, pr, pe, unimpl := 0, 0, 0, 0, 0
	var gaps []string
	for _, n := range names {
		e := scores[n]
		if e.implemented == 0 {
			unimpl++
			gaps = append(gaps, fmt.Sprintf("%s(%d)", n, e.totalBlocks))
			continue
		}
		tr += e.runs
		te += e.exact
		pr += e.placedRuns
		pe += e.placedExact
		line := fmt.Sprintf("%-34s runs %4d exact %4d, placing %4d exact %4d, blocks %6d miss %5d", n, e.runs, e.exact, e.placedRuns, e.placedExact, e.totalBlocks, e.missBlocks)
		if e.exact != e.runs {
			t.Logf("%s | %s", line, e.first)
		}
	}
	t.Logf("implemented: %d of %d runs exact, %d of %d placing runs exact", te, tr, pe, pr)
	t.Logf("not implemented (%d, blocks in trace): %s", unimpl, strings.Join(gaps, " "))
	checkTraceFloors(t, scores)
}

// checkTraceFloors holds the features the study verified to exact: every run of them must be exact.
func checkTraceFloors(t *testing.T, scores map[string]*featureScore) {
	for n, e := range scores {
		if strings.HasPrefix(n, "ore_") && e.implemented > 0 && e.exact != e.runs {
			t.Errorf("%s: %d of %d runs exact: %s", n, e.exact, e.runs, e.first)
		}
	}
}
