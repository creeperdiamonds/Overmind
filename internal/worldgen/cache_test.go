package worldgen

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"
)

// sameIDs fails the test at the first block two chunks differ in.
func sameIDs(t *testing.T, what string, a, b []uint32) {
	t.Helper()
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("%s: differs at block %d", what, i)
		}
	}
}

// TestReuseBasesChangesNothing: the shared cache, the speed option, gives the same chunks with a cap
// far under the working set, where entries are dropped and built again all the time, as with the
// default cap, in any order and with workers racing for the same entries. (With no cache at all each
// source level would build its neighbours' levels again, which takes minutes.)
func TestReuseBasesChangesNothing(t *testing.T) {
	order := [][2]int32{{5, 5}, {6, 5}, {5, 6}, {-40, 30}, {6, 6}, {4, 5}}
	for _, profile := range []*Profile{ProfileBedrock, ProfileOverjava} {
		bare := NewWithProfile(100, profile)
		bare.SetCacheBytes(4 << 20)
		want := map[[2]int32][]uint32{}
		for _, p := range order {
			want[p] = chunkRuntimeIDs(bare, p[0], p[1])
		}
		kept := NewWithProfile(100, profile)
		for i := len(order) - 1; i >= 0; i-- {
			p := order[i]
			sameIDs(t, profile.Name+" cached, reversed order", want[p], chunkRuntimeIDs(kept, p[0], p[1]))
		}
		racing := NewWithProfile(100, profile)
		var wg sync.WaitGroup
		got := make([][]uint32, len(order))
		for i, p := range order {
			wg.Add(1)
			go func() { defer wg.Done(); got[i] = chunkRuntimeIDs(racing, p[0], p[1]) }()
		}
		wg.Wait()
		for i, p := range order {
			sameIDs(t, profile.Name+" cached, workers", want[p], got[i])
		}
	}
}

// sourceFrom runs a source on a working set centred on a chunk next to it, with nothing cached.
func sourceFrom(g *Generator, tx, tz, sx, sz int) map[[2]int32]bs {
	s := newScratch()
	d := s.startDecoration(g, tx, tz)
	out := map[[2]int32]bs{}
	for _, c := range s.runSourceChanges(d, sx, sz).changes {
		out[[2]int32{int32(c.chunk), c.cell}] = c.now
	}
	return out
}

// TestSourceSameFromEitherSide: a source's changes are the same whichever chunk asks for them, which
// is what lets the cache share them (the review's test, on the default path).
func TestSourceSameFromEitherSide(t *testing.T) {
	g := New(100)
	g.SetCacheBytes(0)
	for i := range 12 {
		sx, sz := i*3-30, (i*7)%40-20
		a, b := sourceFrom(g, sx-1, sz-1, sx, sz), sourceFrom(g, sx+1, sz+1, sx, sz)
		if len(a) != len(b) {
			t.Fatalf("source %d,%d: %d changes from one side, %d from the other", sx, sz, len(a), len(b))
		}
		for k, v := range a {
			if w, ok := b[k]; !ok || w != v {
				t.Fatalf("source %d,%d: change %v differs by the chunk asking", sx, sz, k)
			}
		}
	}
}

// TestScratchStaysSmall: after many chunks a worker holds full buffers only for the chunks a source
// can read, its 3 x 3 and 3 more each way (sourceReach), and the shared cache stays under its cap.
func TestScratchStaysSmall(t *testing.T) {
	g := New(100)
	const limit = 8 << 20
	g.SetCacheBytes(limit)
	s := newScratch()
	for i := range 20 {
		g.finalCells(s, i*2, i/3)
	}
	buffers := len(s.copies) + len(s.deco.used) // Spare, and bound in the working set.
	bytes := int64(unsafe.Sizeof(*s)) + int64(buffers)*int64(unsafe.Sizeof(baseChunk{}))
	t.Logf("scratch: %d full buffers, %.1f MB; cache peak %.1f MB", buffers, float64(bytes)/(1<<20), float64(g.CacheStats().PeakBytes)/(1<<20))
	if most := (2*sourceReach + 1) * (2*sourceReach + 1); buffers > most {
		t.Errorf("scratch holds %d buffers, more than the %d chunks a source can read", buffers, most)
	}
	if st := g.CacheStats(); st.PeakBytes > limit {
		t.Errorf("cache peaked at %d bytes, over its %d cap", st.PeakBytes, limit)
	}
}

// TestCacheEmptiesWhenIdle: once no chunk is being made the cache lets everything go.
func TestCacheEmptiesWhenIdle(t *testing.T) {
	saved := cacheIdleDelay
	cacheIdleDelay = 50 * time.Millisecond
	defer func() { cacheIdleDelay = saved }()
	g := New(100)
	chunkRuntimeIDs(g, 3, 3)
	if g.CacheStats().Bytes == 0 {
		t.Fatal("nothing was cached")
	}
	deadline := time.Now().Add(5 * time.Second)
	for g.CacheStats().Bytes != 0 {
		if time.Now().After(deadline) {
			t.Fatalf("cache still holds %d bytes", g.CacheStats().Bytes)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestCacheBuildsOnce: workers asking for one entry at once build it once and get the same one.
func TestCacheBuildsOnce(t *testing.T) {
	c := newSharedCache(1 << 20)
	var builds atomic.Int32
	var wg sync.WaitGroup
	got := make([]*cacheEntry, 16)
	for i := range got {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got[i] = c.get(cacheKey{cacheLevel, 1, 2}, func(e *cacheEntry) int64 {
				builds.Add(1)
				time.Sleep(20 * time.Millisecond)
				e.src = &sourceResult{changes: make([]sourceChange, 3)}
				return 36
			})
		}()
	}
	wg.Wait()
	if n := builds.Load(); n != 1 {
		t.Fatalf("built %d times", n)
	}
	for _, e := range got {
		if e != got[0] {
			t.Fatal("workers got different entries")
		}
	}
}

// TestCacheDropsOldest: past its cap the cache drops the least recently used entry.
func TestCacheDropsOldest(t *testing.T) {
	c := newSharedCache(3 * (100 + entryBytes))
	put := func(x int32) { c.get(cacheKey{cacheLevel, x, 0}, func(*cacheEntry) int64 { return 100 }) }
	put(1)
	put(2)
	put(3)
	put(1) // Now the most recent.
	put(4)
	if _, ok := c.entries[cacheKey{cacheLevel, 2, 0}]; ok {
		t.Fatal("the least recently used entry was kept")
	}
	if _, ok := c.entries[cacheKey{cacheLevel, 1, 0}]; !ok {
		t.Fatal("a recently used entry was dropped")
	}
	if c.used > c.limit {
		t.Fatalf("holds %d bytes, over %d", c.used, c.limit)
	}
}

// TestCompactBaseIsExact: a base packed by sections and expanded again is the same base, and it is
// smaller than the full one.
func TestCompactBaseIsExact(t *testing.T) {
	g := New(100)
	s := newScratch()
	full := int64(unsafe.Sizeof(baseChunk{}))
	var packed int64
	for _, p := range [][2]int{{0, 0}, {5, -3}, {-40, 30}, {120, 7}} {
		b := new(baseChunk)
		g.buildBase(b, s, p[0], p[1], stageCarvers, 0, 15, 0, 15)
		c := compact(b)
		packed += c.bytes()
		back := new(baseChunk)
		c.expand(back)
		if back.b != b.b || back.wgTop != b.wgTop || back.wgFloor != b.wgFloor || back.cx != b.cx || back.cz != b.cz {
			t.Fatalf("chunk %v: the compact base expands to a different chunk", p)
		}
	}
	t.Logf("compact bases: %.0f KB each against %.0f KB full", float64(packed)/4/1024, float64(full)/1024)
	if packed/4 >= full/2 {
		t.Errorf("compact bases average %d bytes, not under half of %d", packed/4, full)
	}
}
