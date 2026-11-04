package worldgen

import (
	"sync"
	"time"
	"unsafe"
)

// The shared cache keeps carvers-stage bases and source results between chunks, for every worker of
// one generator (owner's ruling 26). Both are pure functions of the seed, profile and chunk position,
// so the cache changes speed and never a result (TestReuseBasesChangesNothing). It is capped by bytes,
// drops the least recently used entry first, builds each entry once however many workers ask for it
// at the same time, and empties itself once no chunk has been asked for in a short while, so an idle
// server keeps none of it.

// DefaultCacheBytes is the shared cache's cap when nothing else is set.
const DefaultCacheBytes = 30 << 20

// cacheIdleDelay is how long after the last GenerateChunk ends the cache waits before emptying. Workers
// take chunks one after another, so the count of running calls touches zero between two chunks of one
// burst; the delay keeps that gap from emptying the cache.
var cacheIdleDelay = time.Second

type cacheKind uint8

const (
	cacheBase  cacheKind = iota
	cacheLevel           // Source results: cacheLevel plus the level.
)

type cacheKey struct {
	kind   cacheKind
	dim    uint8 // The generator's dimension: the Overworld and the Nether share one cache.
	cx, cz int32
}

// cacheEntry is one kept base or source result. ready is closed once it is built; until then other
// workers wanting it wait for the one building it.
type cacheEntry struct {
	key        cacheKey
	ready      chan struct{}
	failed     bool // The build panicked; waiters build it again themselves.
	base       *compactBase
	src        *sourceResult
	bytes      int64
	prev, next *cacheEntry
}

var (
	changeBytes = int64(unsafe.Sizeof(sourceChange{}))
	resultBytes = int64(unsafe.Sizeof(sourceResult{}))
	entryBytes  = int64(unsafe.Sizeof(cacheEntry{}))
)

// sharedCache is the cache. Entries still being built hold no bytes and are never dropped.
type sharedCache struct {
	mu      sync.Mutex
	limit   int64
	used    int64
	peak    int64
	entries map[cacheKey]*cacheEntry
	lru     cacheEntry // Sentinel: lru.next is the most recently used entry, lru.prev the least.
	running int        // GenerateChunk calls under way.
	hits    [8]uint64  // Gets answered from the cache, by kind (levels past 6 share the last).
	misses  [8]uint64  // Gets that built the entry, by kind.
	calls   uint64     // GenerateChunk calls ever started, so a timer knows if one came since.
	timer   *time.Timer
}

func newSharedCache(limit int64) *sharedCache {
	c := &sharedCache{limit: limit, entries: map[cacheKey]*cacheEntry{}}
	c.lru.prev, c.lru.next = &c.lru, &c.lru
	return c
}

func (c *sharedCache) unlink(e *cacheEntry) {
	e.prev.next, e.next.prev = e.next, e.prev
	e.prev, e.next = nil, nil
}

func (c *sharedCache) pushFront(e *cacheEntry) {
	e.prev, e.next = &c.lru, c.lru.next
	c.lru.next.prev = e
	c.lru.next = e
}

// get returns the entry for key, building it with build if no worker has. build fills the entry's
// base or changes and returns its size in bytes.
func (c *sharedCache) get(key cacheKey, build func(e *cacheEntry) int64) *cacheEntry {
	for {
		c.mu.Lock()
		if e, ok := c.entries[key]; ok {
			c.hits[min(int(key.kind), 7)]++
			if e.prev != nil {
				c.unlink(e)
				c.pushFront(e)
			}
			c.mu.Unlock()
			<-e.ready
			if !e.failed {
				return e
			}
			continue
		}
		e := &cacheEntry{key: key, ready: make(chan struct{})}
		c.misses[min(int(key.kind), 7)]++
		c.entries[key] = e
		c.pushFront(e)
		c.mu.Unlock()
		c.fill(e, build)
		return e
	}
}

// fill builds an entry and charges its bytes, dropping old entries to stay under the cap.
func (c *sharedCache) fill(e *cacheEntry, build func(e *cacheEntry) int64) {
	done := false
	defer func() {
		if done {
			return
		}
		c.mu.Lock()
		e.failed = true
		if c.entries[e.key] == e {
			delete(c.entries, e.key)
		}
		if e.prev != nil {
			c.unlink(e)
		}
		c.mu.Unlock()
		close(e.ready)
	}()
	n := build(e) + entryBytes
	done = true
	c.mu.Lock()
	e.bytes = n
	c.used += n
	for q := c.lru.prev; c.used > c.limit && q != &c.lru; {
		prev := q.prev
		if q.bytes > 0 {
			c.drop(q)
		}
		q = prev
	}
	c.peak = max(c.peak, c.used)
	c.mu.Unlock()
	close(e.ready)
}

// drop removes a built entry. A worker still holding it keeps a valid copy: entries never change.
func (c *sharedCache) drop(e *cacheEntry) {
	c.unlink(e)
	delete(c.entries, e.key)
	c.used -= e.bytes
}

// begin and end bracket a GenerateChunk call. When the last running call ends, a timer starts; if no
// call has begun when it fires, the cache empties.
func (c *sharedCache) begin() {
	c.mu.Lock()
	c.running++
	c.calls++
	c.mu.Unlock()
}

func (c *sharedCache) end() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.running--
	if c.running > 0 {
		return
	}
	calls := c.calls
	if c.timer != nil {
		c.timer.Stop()
	}
	c.timer = time.AfterFunc(cacheIdleDelay, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.running == 0 && c.calls == calls {
			c.emptyLocked()
		}
	})
}

// emptyLocked drops every built entry.
func (c *sharedCache) emptyLocked() {
	for q := c.lru.prev; q != &c.lru; {
		prev := q.prev
		if q.bytes > 0 {
			c.drop(q)
		}
		q = prev
	}
}

// CacheStats reports the shared cache: bytes held now and the most it has held.
type CacheStats struct {
	Bytes, PeakBytes int64
	Entries          int
	// Hits and Misses count gets by kind: 0 bases, then source results by level from 1.
	Hits, Misses [8]uint64
}

// CacheStats returns the shared cache's figures.
func (g *Generator) CacheStats() CacheStats {
	c := g.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	return CacheStats{Bytes: c.used, PeakBytes: c.peak, Entries: len(c.entries), Hits: c.hits, Misses: c.misses}
}

// SetCacheBytes changes the shared cache's cap. 0 keeps nothing between chunks, which is slow and
// changes no result. Call it before generating.
func (g *Generator) SetCacheBytes(limit int64) {
	c := g.cache
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limit = limit
	for q := c.lru.prev; c.used > c.limit && q != &c.lru; {
		prev := q.prev
		if q.bytes > 0 {
			c.drop(q)
		}
		q = prev
	}
}

// cachedBase gives a chunk's carvers stage, compact, from the cache, building it in one of the
// worker's buffers if nobody has. Expand it to read it (compactBase.expand).
func (g *Generator) cachedBase(s *chunkScratch, cx, cz int) *compactBase {
	e := g.cache.get(cacheKey{cacheBase, g.dim, int32(cx), int32(cz)}, func(e *cacheEntry) int64 {
		b := s.takeBuffer()
		g.buildBase(b, s, cx, cz, stageCarvers, 0, 15, 0, 15)
		e.base = compact(b)
		s.giveBuffer(b)
		return e.base.bytes()
	})
	return e.base
}

// sourceDepth is how many levels of earlier neighbours a source sees (cachedLevel).
var sourceDepth = 2

// cachedSource gives what a source chunk's decoration did as the merge uses it (cachedLevel at
// sourceDepth).
func (g *Generator) cachedSource(s *chunkScratch, sx, sz int) *sourceResult {
	if g.bedrockDeco() {
		return g.cachedParity(s, sx, sz)
	}
	return g.cachedLevel(s, sx, sz, sourceDepth)
}

// cachedLevel gives what a source chunk's decoration does, from the cache. Level 0 is the source run
// alone on the carvers stage. Level L is the source run on top of what its earlier neighbours (before
// it in row-major order) do at level L-1. In Java the chunk decorated second sees the first's blocks:
// its trees meet the other's canopy through the live heightmap and fail, and the random draws after
// them change with it. A fixed number of levels keeps the result a pure function of the source and
// the chunks around it.
func (g *Generator) cachedLevel(s *chunkScratch, sx, sz, level int) *sourceResult {
	e := g.cache.get(cacheKey{cacheLevel + cacheKind(level), g.dim, int32(sx), int32(sz)}, func(e *cacheEntry) int64 {
		var pre []sourceWrite
		if level > 0 {
			for ex := sx - 1; ex <= sx; ex++ {
				for ez := sz - 1; ez <= sz+1; ez++ {
					if ex == sx && ez >= sz {
						continue
					}
					for _, c := range g.cachedLevel(s, ex, ez, level-1).changes {
						cx, cz := ex-1+int(c.chunk)/3, ez-1+int(c.chunk)%3
						pre = append(pre, sourceWrite{pos{cx*16 + int(c.cell)&15, int(c.cell)>>8 + minY, cz*16 + int(c.cell)>>4&15}, c.now})
					}
				}
			}
		}
		d := s.startDecoration(g, sx, sz)
		d.pre = pre
		e.src = s.runSourceChanges(d, sx, sz)
		d.pre = nil
		return int64(cap(e.src.changes))*changeBytes + resultBytes
	})
	return e.src
}
