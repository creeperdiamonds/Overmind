package structures

import (
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// heapNow is the live heap after a full collection, once it has settled: earlier tests' generators
// empty their caches a second after their last chunk (worldgen's idle delay), which would otherwise
// land inside a measurement.
func heapNow() int64 {
	read := func() int64 {
		runtime.GC()
		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		return int64(m.HeapAlloc)
	}
	h := read()
	for range 20 {
		time.Sleep(300 * time.Millisecond)
		n := read()
		if d := n - h; d < 256<<10 && d > -256<<10 {
			return n
		}
		h = n
	}
	return h
}

// TestTemplateMemory parses every template of the install and compares the heap they take with the
// estimate Data counts (templateBytes): the estimate must be within 20% of the heap. The ancient city's
// templates are checked on their own too. The whole set is what Data keeps at most (templateWarnBytes).
func TestTemplateMemory(t *testing.T) {
	_, d := installData(t)
	time.Sleep(1500 * time.Millisecond) // Past worldgen's idle delay: earlier tests' caches are emptied.
	paths := slices.Sorted(func(yield func(string) bool) {
		for p := range d.templates {
			if !yield(p) {
				return
			}
		}
	})
	for _, set := range []struct {
		name   string
		prefix string
	}{{"ancient city", "ancient_city/"}, {"all", ""}} {
		before := heapNow()
		var kept []*Template
		est := int64(0)
		for _, p := range paths {
			if !strings.HasPrefix(p, set.prefix) {
				continue
			}
			tp, err := readTemplate(p, d.templates[p])
			if err != nil {
				continue
			}
			kept = append(kept, tp)
			est += templateBytes(tp)
		}
		heap := heapNow() - before - int64(cap(kept))*8
		runtime.KeepAlive(kept)
		t.Logf("%s: %d templates, estimate %.2f MB, heap %.2f MB (%.2f of the estimate)", set.name, len(kept),
			float64(est)/1e6, float64(heap)/1e6, float64(heap)/float64(est))
		if len(kept) == 0 {
			t.Fatalf("%s: no templates", set.name)
		}
		if heap > est*12/10 || heap < est*8/10 {
			t.Errorf("%s: the estimate %d is not within 20%% of the heap %d", set.name, est, heap)
		}
	}
}

// TestStructureMemory measures what the structures keep outside the generator's cache: the templates the
// pools pin once every pool is built, and every assembly cache filled to its cap with large structures.
// The figures are in NOTES.md; the test fails when one grows past the figure there with a margin.
func TestStructureMemory(t *testing.T) {
	bd, d0 := installData(t)
	time.Sleep(1500 * time.Millisecond) // Past worldgen's idle delay: earlier tests' caches are emptied.
	// A fresh Data: nothing parsed yet.
	d, err := LoadData(d0.Dir)
	if err != nil {
		t.Fatal(err)
	}
	base := heapNow()
	d.villagePools()
	d.trialPools()
	d.bastionPools()
	d.ancientPools()
	d.dataPools()
	pinned := heapNow() - base
	n := 0
	d.parsed.Range(func(_, v any) bool {
		if _, ok := v.(*Template); ok {
			n++
		}
		return true
	})
	t.Logf("pools: %d templates kept, heap %.2f MB, estimate %.2f MB", n, float64(pinned)/1e6,
		float64(d.parsedBytes.Load())/1e6)

	cand := func(s *Structures, k Kind, i int) Start {
		r := worldgen.NewBedrockRandom(0)
		cx, cz := spreads[k].candidate(s, i%9-4, i/9-4, r)
		return Start{Kind: k, CX: cx, CZ: cz, rseed: r.Seeded()}
	}
	// Every other template too: what Data keeps at most. The caches below then hold only their entries.
	for p := range d.templates {
		d.Template(p)
	}
	all := heapNow() - base
	t.Logf("every template of the install kept: heap %.2f MB, estimate %.2f MB", float64(all)/1e6, float64(d.parsedBytes.Load())/1e6)

	g := worldgen.NewWithData(100, worldgen.ProfileBedrock, bd)
	s := New(g, d)
	// Warm the generator's own lazily built state (terrain reads), which is not the caches'.
	g.GenerateBase(0, 0, chunk.New(world.DefaultBlockRegistry, world.Overworld.Range()))
	s.ruinedPortalAt(cand(s, RuinedPortal, 40).CX, cand(s, RuinedPortal, 40).CZ)
	g.SetCacheBytes(0)
	g.SetCacheBytes(worldgen.DefaultCacheBytes)
	ns := New(worldgen.NewWithData(100, worldgen.ProfileBedrock, bd).Nether(), d)
	es := New(worldgen.NewWithData(100, worldgen.ProfileBedrock, bd).End(), d)
	keyCap := assemblyCacheSize * 4
	total := int64(0)
	measure := func(name string, entries int, fill func()) int64 {
		before := heapNow()
		fill()
		// The terrain reads (ruined portals, heights) fill the generator's own cache, which is capped apart:
		// empty it before measuring.
		g.SetCacheBytes(0)
		g.SetCacheBytes(worldgen.DefaultCacheBytes)
		h := heapNow() - before
		total += h
		t.Logf("%s: %d entries, heap %.2f MB", name, entries, float64(h)/1e6)
		return h
	}
	measure("mineshafts", keyCap, func() {
		for i := range keyCap {
			k := [2]int{i * 13, -i * 7}
			s.mineshafts.get(k, func() any { return s.mineshaftAtStart(k[0], k[1]) })
		}
	})
	measure("strongholds", keyCap, func() {
		for i := range keyCap {
			k := [2]int{i * 11, i * 5}
			s.strongholds.get(k, func() any { return s.strongholdAtStart(k[0], k[1]) })
		}
	})
	measure("monuments", keyCap, func() {
		for i := range keyCap {
			st := cand(s, Monument, i)
			s.monuments.get([2]int{st.CX, st.CZ}, func() any { return s.assembleMonument(st.CX, st.CZ) })
		}
	})
	measure("mansions", keyCap, func() {
		for i := range keyCap {
			st := cand(s, Mansion, i)
			s.mansions.get([2]int{st.CX, st.CZ}, func() any { return s.assembleMansion(st) })
		}
	})
	ruins := 0
	measure("ocean ruins", keyCap, func() {
		for i := 0; ruins < keyCap && i < 4000; i++ {
			st := cand(s, OceanRuin, i)
			if ps := s.oceanRuinAt(st.CX, st.CZ); ps != nil {
				s.oceanRuins.get([2]int{st.CX, st.CZ}, func() any { return ps })
				ruins++
			}
		}
	})
	// Ruined portal plans read the terrain of the chunks around them (slow): 8 are made, and the cache's 64
	// taken as 8 times that. Their blocks are checked against portalReach on the way.
	const portalsMade = 8
	ph := measure("ruined portals (8 made)", portalsMade, func() {
		for i := range portalsMade {
			st := cand(s, RuinedPortal, i)
			p := s.portals.get([2]int{st.CX, st.CZ}, func() any { return s.ruinedPortalAt(st.CX, st.CZ) }).(*rpPlan)
			if p == nil {
				continue
			}
			t.Logf("ruined portal (%d,%d): %d blocks", st.CX, st.CZ, len(p.blocks))
			for b := range p.blocks {
				if abs(b[0]>>4-st.CX) > portalReach || abs(b[2]>>4-st.CZ) > portalReach {
					t.Errorf("ruined portal (%d,%d): block %v past portalReach", st.CX, st.CZ, b)
				}
			}
		}
	})
	total += ph * int64(keyCap/portalsMade-1)
	measure("fortresses (Nether)", keyCap, func() {
		for i := range keyCap {
			st := cand(ns, Fortress, i)
			ns.fortresses.get([2]int{st.CX, st.CZ}, func() any { return ns.fortressAtStart(st) })
		}
	})
	measure("end cities (End)", keyCap, func() {
		for i := range keyCap {
			r := worldgen.NewBedrockRandom(0)
			cx, cz := spreads[EndCity].candidate(es, i%9+3, i/9+3, r)
			es.endCities.get([2]int{cx, cz}, func() any { return es.assembleEndCity(cx, cz, endCityRotation(cx, cz), 70, r) })
		}
	})
	// The assembly caches (one per dimension's Structures): the largest jigsaw structures.
	measure("assembled starts (ancient cities)", assemblyCacheSize, func() {
		for i := range assemblyCacheSize {
			s.assembled.get(cand(s, AncientCity, i), s.ancientCity)
		}
	})
	measure("assembled starts (bastions, Nether)", assemblyCacheSize, func() {
		for i := range assemblyCacheSize {
			ns.assembled.get(cand(ns, Bastion, i), ns.bastionAtStart)
		}
	})
	runtime.KeepAlive(s)
	runtime.KeepAlive(ns)
	runtime.KeepAlive(es)
	t.Logf("all caches full: %.2f MB (ruined portals taken as 64); with every template %.2f MB", float64(total)/1e6,
		float64(total+all)/1e6)
	// NOTES.md's figures with a margin of a quarter.
	if all > structTemplatesMB*5/4<<20 {
		t.Errorf("the templates take %.2f MB, past NOTES.md's %d MB", float64(all)/1e6, structTemplatesMB)
	}
	if total > structCachesMB*5/4<<20 {
		t.Errorf("full caches take %.2f MB, past NOTES.md's %d MB", float64(total)/1e6, structCachesMB)
	}
}

// The measured figures NOTES.md gives (MB), which TestStructureMemory holds.
const (
	structTemplatesMB = 55 // Every template of BDS 1.26.60's install, 43 of it the pools'.
	structCachesMB    = 14 // Every structure cache full.
)
