package structures

import (
	"bufio"
	"compress/gzip"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// bdsWorld is a whole-world export of a BDS world (golden-bedrock/whole, the format worldgen's golden
// tests read): block names per cell (y, z, x order) and the mask of cells BDS does not reproduce.
type bdsWorld struct {
	seed           int64
	x0, x1, z0, z1 int
	names          []string
	chunks         map[[2]int]*bdsChunk
}

type bdsChunk struct {
	ids      []uint16
	unstable []byte
}

const chunkCells = 16 * 16 * 384

func (c *bdsChunk) masked(cell int) bool { return c.unstable[cell>>3]>>(cell&7)&1 != 0 }

func cellOf(x, y, z int) int { return (y-minY)<<8 | (z&15)<<4 | x&15 }

var bdsCache sync.Map

func loadWorld(t testing.TB, path string) *bdsWorld {
	if w, ok := bdsCache.Load(path); ok {
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
	io.ReadFull(r, magic)
	w := &bdsWorld{chunks: map[[2]int]*bdsChunk{}}
	var count int32
	switch string(magic) {
	case "OMBW1":
		var h [4]int32
		binary.Read(r, binary.LittleEndian, &h)
		w.seed, w.x0, w.x1, w.z0, w.z1, count = int64(h[0]), int(h[1]), int(h[2]), int(h[1]), int(h[2]), h[3]
	case "OMBW2":
		var h [6]int32
		binary.Read(r, binary.LittleEndian, &h)
		w.seed, w.x0, w.x1, w.z0, w.z1, count = int64(h[0]), int(h[1]), int(h[2]), int(h[3]), int(h[4]), h[5]
	default:
		t.Fatalf("%s: not a whole-world file", path)
	}
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
			c := &bdsChunk{ids: make([]uint16, chunkCells), unstable: make([]byte, chunkCells/8)}
			binary.Read(r, binary.LittleEndian, c.ids)
			io.ReadFull(r, c.unstable)
			io.CopyN(io.Discard, r, chunkCells*2) // Biomes.
			w.chunks[[2]int{cx, cz}] = c
		}
	}
	bdsCache.Store(path, w)
	return w
}

// nameAt is a generated block's Bedrock name without "minecraft:".
func nameAt(c *chunk.Chunk, x, y, z int) string {
	b, _ := world.BlockByRuntimeID(c.Block(uint8(x&15), int16(y), uint8(z&15), 0))
	n, _ := b.EncodeBlock()
	return strings.TrimPrefix(n, "minecraft:")
}

// boxScore compares the cells inside boxes of one chunk, ours against BDS's, on the stable mask.
func boxScore(w *bdsWorld, c *chunk.Chunk, cx, cz int, boxes []box) (same, total int, conf map[[2]string]int) {
	conf = map[[2]string]int{}
	bc := w.chunks[[2]int{cx, cz}]
	if bc == nil {
		return
	}
	seen := map[int]bool{}
	for _, b := range boxes {
		for x := max(b[0], cx*16); x <= min(b[3], cx*16+15); x++ {
			for z := max(b[2], cz*16); z <= min(b[5], cz*16+15); z++ {
				for y := max(b[1], minY); y <= min(b[4], maxY-1); y++ {
					cell := cellOf(x, y, z)
					if seen[cell] || bc.masked(cell) {
						continue
					}
					seen[cell] = true
					total++
					real, mine := w.names[bc.ids[cell]], nameAt(c, x, y, z)
					if real == mine {
						same++
					} else {
						conf[[2]string{real, mine}]++
					}
				}
			}
		}
	}
	return
}

func topConfusions(conf map[[2]string]int, n int) string {
	type kv struct {
		k [2]string
		v int
	}
	var all []kv
	for k, v := range conf {
		all = append(all, kv{k, v})
	}
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].v > all[j-1].v; j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	var out []string
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, fmt.Sprintf("%s<-%s:%d", all[i].k[0], all[i].k[1], all[i].v))
	}
	return strings.Join(out, ", ")
}

// TestAncientCityMatchesBDS checks seed 100's ancient city (start chunk (1, -13), N2 1.9): its chests
// (position and loot seed) against the scan, and its blocks inside the piece boxes against the whole
// world export (chunks -10..9, so the city's chunks x 0..8, z -10..-5), with and without structures.
func TestAncientCityMatchesBDS(t *testing.T) {
	ev := loadEvidence(t)
	g, s := worldFor(t, 100)
	st, ok := s.StartIn(AncientCity, 1, -13)
	if !ok {
		t.Fatal("no ancient city start at (1, -13)")
	}
	pieces := s.ancientCity(st)
	var boxes, early []box
	ext := pieces[0].box
	for i, p := range pieces {
		if p.elem.template != nil {
			boxes = append(boxes, p.box)
			if i < 44 {
				early = append(early, p.box) // The pieces the study's model found right (N2 1.9).
			}
		}
		for i := range 3 {
			ext[i], ext[i+3] = min(ext[i], p.box[i]), max(ext[i+3], p.box[i+3])
		}
	}
	t.Logf("%d pieces, extent %v", len(pieces), ext)
	want := map[[3]int]int32{}
	for _, l := range ev.loot("100") {
		if strings.Contains(l.table, "ancient_city") && l.x >= ext[0]-8 && l.x <= ext[3]+8 && l.z >= ext[2]-8 && l.z <= ext[5]+8 {
			want[[3]int{l.x, l.y, l.z}] = l.seed
		}
	}
	chunks := map[[2]int]bool{}
	for _, b := range boxes {
		for cx := floorDiv(b[0], 16); cx <= floorDiv(b[3], 16); cx++ {
			for cz := floorDiv(b[2], 16); cz <= floorDiv(b[5], 16); cz++ {
				chunks[[2]int{cx, cz}] = true
			}
		}
	}
	w := loadWorld(t, filepath.Join(goldenDir(t), "whole", "seed100.bin.gz"))
	bare := worldgen.NewWithData(100, worldgen.ProfileBedrock, s.bd)
	var mu sync.Mutex
	var wg sync.WaitGroup
	got := map[[3]int]int32{}
	var same, sameBare, total, sameE, totalE int
	conf := map[[2]string]int{}
	sem := make(chan struct{}, 4)
	for c := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer func() { <-sem; wg.Done() }()
			col := generate(g, c[0], c[1])
			var sm, tt, smb int
			var cf map[[2]string]int
			if _, ok := w.chunks[c]; ok {
				sm, tt, cf = boxScore(w, col.Chunk, c[0], c[1], boxes)
				se, te, _ := boxScore(w, col.Chunk, c[0], c[1], early)
				mu.Lock()
				sameE, totalE = sameE+se, totalE+te
				mu.Unlock()
				plain := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
				bare.GenerateChunk(world.ChunkPos{int32(c[0]), int32(c[1])}, plain)
				smb, _, _ = boxScore(w, plain, c[0], c[1], boxes)
			}
			mu.Lock()
			defer mu.Unlock()
			for _, be := range col.BlockEntities {
				got[[3]int(be.Pos)] = be.Data["LootTableSeed"].(int32)
			}
			same, sameBare, total = same+sm, sameBare+smb, total+tt
			for k, v := range cf {
				conf[k] += v
			}
		}()
	}
	wg.Wait()
	pos, seeds := 0, 0
	for p, sd := range want {
		if g, ok := got[p]; ok {
			pos++
			if g == sd {
				seeds++
			}
		}
	}
	t.Logf("chests: BDS %d in the city, ours %d; %d at BDS positions, %d of them with BDS's loot seed", len(want), len(got), pos, seeds)
	t.Logf("blocks in piece boxes (chunks -10..9): with structures %d/%d = %.2f%%, without %d/%d = %.2f%%", same, total,
		100*float64(same)/float64(max(total, 1)), sameBare, total, 100*float64(sameBare)/float64(max(total, 1)))
	t.Logf("  in the boxes of pieces 0..43 only: %d/%d = %.2f%%", sameE, totalE, 100*float64(sameE)/float64(max(totalE, 1)))
	t.Logf("  top confusions (BDS<-ours): %s", topConfusions(conf, 12))
	if pos < 5 || seeds != pos {
		t.Errorf("chests at BDS positions %d (the study's model had 5), with BDS's seed %d", pos, seeds)
	}
}
