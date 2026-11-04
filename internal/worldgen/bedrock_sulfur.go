package worldgen

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
)

// Bedrock's biome table and sulfur caves (Task 4e round 5), for the Bedrock profile with the data
// read. In our own Go from the study of BDS 1.26.60.6 (NOTES-bedrock-exact T7 and T10).
//
// Table (T10, read from OverworldBiomeBuilder's list in the code). BDS builds Java's list with three
// differences, all for the underground rows, which it pushes after the surface biomes in this order:
// dripstone caves (humidity below 0.7, where Java's is the full range), lush caves, sulfur caves (a
// row Java does not have), deep dark. A cave row is added only when the data registers its biome
// (an install with no sulfur_caves file gets no row). And BDS's endpoints are the truncated double
// product of each float with 10000, where Java's are the float product: ten values come out one less
// in magnitude (bedrockEndpoints). The search tree is built from the list as Java's is.
//
// Builder: NoiseGradientSurfaceBuilder (T7). A NormalNoise made like Java's
// NormalNoise.create(randomState.fromHashOf(name), first_octave, amplitudes), sampled at block
// coordinates and clamped to [-1, 1]; the first specifier whose range (min, max] holds the value sets
// the block, and none leaves it. Air, liquids and the non-replaceable blocks are left alone, and so is
// the top block of each run of the biome in a column: the walk starts one under the run's top. The
// runs are the subsurface pass's (T12, applySubsurface).

// sulfurBiome is sulfur caves' index into biomeTableNames.
var sulfurBiome = func() uint8 {
	for i, n := range biomeTableNames {
		if n == "sulfur_caves" {
			return uint8(i)
		}
	}
	panic("worldgen: no sulfur_caves in the biome table")
}()

// bedrockEndpoints are the endpoints of Java's table that BDS's differ from (T10: -0.45f gives -4499
// in BDS, -4500 in Java), quantized as the table is (10000 is 1.0). Every other endpoint is the same.
var bedrockEndpoints = map[int16]int16{-4500: -4499, -3500: -3499, -2225: -2224, 4500: 4499, -10500: -10499, -1900: -1899,
	-1100: -1099, 300: 299, 7000: 6999, 9000: 8999}

// Bedrock's underground rows where they differ from Java's (T10), in Java's units before
// bedrockEndpoints: temperature, humidity, continentalness, erosion, depth, weirdness.
var (
	bedrockDripstoneHumidity = [2]int16{-10000, 7000}
	bedrockSulfurRow         = [6][2]int16{{-10000, 10000}, {-10000, 7000}, {-1900, 5500}, {4500, 10000}, {2000, 9000}, {-11000, -8500}}
)

// bedrockLeaves is BDS's list: Java's rows with dripstone caves' humidity, sulfur caves before deep
// dark if asked, dappled forest in its plains rows if asked, and BDS's endpoints. biomeLeaves gives
// fresh leaves, so changing them here changes no other list.
//
// Dappled forest (T11, read from _populateBiomeArrays' id tables): BDS's MIDDLE_BIOMES_VARIANT[1][0] is
// dappled_forest where Java's is none (plains), so every plains row with temperature T1 (-0.45..-0.15),
// humidity H0 (-1..-0.35) and a weirdness range reaching 0 or above is dappled forest.
func bedrockLeaves(sulfur, dappled bool) []*biomeNode {
	var list []*biomeNode
	for _, n := range biomeLeaves() {
		switch n.id {
		case biomeIdx["plains"]:
			if dappled && n.bounds[0] == [2]int16{-4500, -1500} && n.bounds[1] == [2]int16{-10000, -3500} && n.bounds[5][1] >= 0 {
				n.id = biomeIdx["dappled_forest"]
			}
		case biomeIdx["dripstone_caves"]:
			n.bounds[1] = bedrockDripstoneHumidity
		case biomeIdx["deep_dark"]:
			if sulfur {
				list = append(list, &biomeNode{id: sulfurBiome, bounds: bedrockSulfurRow})
			}
		}
		list = append(list, n)
	}
	for i, n := range list {
		n.row = int32(i)
		for d := range n.bounds {
			for e := range 2 {
				if v, ok := bedrockEndpoints[n.bounds[d][e]]; ok {
					n.bounds[d][e] = v
				}
			}
		}
	}
	return list
}

// bedrockBiomeTrees are the Bedrock profile's trees, by which of sulfur caves (bit 0) and dappled
// forest (bit 1) the data registers.
var bedrockBiomeTrees = sync.OnceValue(func() (t [4]*biomeNode) {
	for i := range t {
		t[i] = buildBiomeTree(bedrockLeaves(i&1 != 0, i&2 != 0))
	}
	return t
})

// optionalBiome says whether a biome of the table is one BDS adds only when the data registers it.
func optionalBiome(i int) bool { return i == int(sulfurBiome) || i == int(biomeIdx["dappled_forest"]) }

// gradientSpec is one of a noise_gradient builder's noise_block_specifiers.
type gradientSpec struct {
	lo, hi float64
	s      bs
}

// gradientDef is a noise_gradient builder as read from a biome file.
type gradientDef struct {
	name  string
	first int
	amps  []float64
	keep  bnameSet // non_replaceable_blocks
	specs []gradientSpec
	noise *normalNoise // Made per world (withNoise).
}

// parseGradient reads a builder of type minecraft:noise_gradient.
func parseGradient(b map[string]json.RawMessage) (*gradientDef, error) {
	var nd struct {
		Name  string    `json:"name"`
		First int       `json:"first_octave"`
		Amps  []float64 `json:"amplitudes"`
	}
	if err := json.Unmarshal(b["noise_descriptor"], &nd); err != nil || nd.Name == "" || len(nd.Amps) == 0 || len(nd.Amps) > 16 {
		return nil, fmt.Errorf("noise_gradient: bad noise_descriptor")
	}
	if nd.First < -32 || nd.First > 0 {
		return nil, fmt.Errorf("noise_gradient: first_octave %d out of range", nd.First)
	}
	g := &gradientDef{name: nd.Name, first: nd.First, amps: nd.Amps}
	var keep []json.RawMessage
	json.Unmarshal(b["non_replaceable_blocks"], &keep)
	for _, k := range keep {
		n, ok := packBlockName(k)
		if !ok {
			return nil, fmt.Errorf("noise_gradient: bad non_replaceable_blocks")
		}
		g.keep.add(internBName(strings.TrimPrefix(n, "minecraft:")))
	}
	var specs []struct {
		Range struct {
			Min float64 `json:"min"`
			Max float64 `json:"max"`
		} `json:"range"`
		Block json.RawMessage `json:"block"`
	}
	if err := json.Unmarshal(b["noise_block_specifiers"], &specs); err != nil {
		return nil, fmt.Errorf("noise_gradient: bad noise_block_specifiers")
	}
	for _, sp := range specs {
		d, err := parseBDesc(sp.Block)
		if err != nil {
			return nil, err
		}
		s, err := javaStateFor(d)
		if err != nil {
			return nil, unimpl("noise_gradient block %s", d.name)
		}
		g.specs = append(g.specs, gradientSpec{sp.Range.Min, sp.Range.Max, s})
	}
	return g, nil
}

// withNoise gives a copy of the builder with its noise for a world.
func (g *gradientDef) withNoise(w seedFactory) *gradientDef {
	c := *g
	r := w.named(g.name)
	c.noise = newNormalNoise(&r, g.first, g.amps)
	return &c
}

// pick is the block for a position, or 0 and false when no specifier matches.
func (g *gradientDef) pick(x, y, z int) (bs, bool) {
	return g.pickValue(g.noise.at(float64(x), float64(y), float64(z)))
}

// pickValue is the block for a noise value: clamped to [-1, 1], the first specifier with min < v <= max.
func (g *gradientDef) pickValue(v float64) (bs, bool) {
	v = math.Max(-1, math.Min(1, v))
	for _, sp := range g.specs {
		if sp.lo < v && v <= sp.hi {
			return sp.s, true
		}
	}
	return 0, false
}

// applySubsurface is the subsurface pass (T12, OverworldGenerator::loadChunk), after the surface pass
// and before the carvers, run only when the chunk's biome list has a biome with a subsurface builder.
// Each column, up to its preliminary surface - 8 (the default builder's low end), is cut into runs of
// one biome; each run whose biome has a builder is built.
func (g *Generator) applySubsurface(b *baseChunk, s *chunkScratch, x0, x1, z0, z1 int) {
	if !g.bedrockBiomes {
		return
	}
	topSec := 1 << 10
	if x0 == 0 && x1 == 15 && z0 == 0 && z1 == 15 {
		topSec = bedrockTopSection(&b.wgTop)
	}
	any := false
	s.blist = s.bedrockBiomeList(g, b.cx, b.cz, min(topSec, (maxY-minY)>>4), s.blist)
	for _, id := range s.blist {
		any = any || g.bsurf.sub[id] != nil
	}
	if !any {
		return
	}
	for lx := x0; lx <= x1; lx++ {
		for lz := z0; lz <= z1; lz++ {
			x, z := b.cx*16+lx, b.cz*16+lz
			top := min(max(g.preliminarySurface(&s.prelims, x, z)-8, minY), maxY-1)
			g.subsurfaceColumn(b, lx, lz, top-minY, func(i int) uint8 { return s.bedrockBiome(g, x, storedY(minY+i, topSec), z) })
		}
	}
}

// subsurfaceColumn cuts one column's indexes 0..n (Y - minY) into runs of one biome, given the biome at
// each index, and runs each run's builder from one under its top down to its bottom (T12). A run still
// open at n ends there; one that starts at n is dropped.
func (g *Generator) subsurfaceColumn(b *baseChunk, lx, lz, n int, biome func(i int) uint8) {
	cur := biome(0)
	start, end := 0, 0
	for i := 0; i <= n; i++ {
		id := biome(i)
		if id == cur {
			end = i
		}
		if i == n || id != cur {
			if gd := g.bsurf.sub[cur]; gd != nil {
				g.gradientRun(b, gd, lx, lz, start, end)
			}
			cur, start, end = id, i, i
		}
	}
}

// gradientRun is the noise_gradient builder over one run: indexes end - 1 down to start.
func (g *Generator) gradientRun(b *baseChunk, gd *gradientDef, lx, lz, start, end int) {
	x, z := b.cx*16+lx, b.cz*16+lz
	for i := end - 1; i >= start; i-- {
		y := minY + i
		blk := b.b[cellIndex(lx, y, lz)]
		if blk.isAir() || blk.has(fWater) || blk.has(fLava) || gd.keep.has(bname(blk)) {
			continue
		}
		if ns, ok := gd.pick(x, y, z); ok {
			b.set(lx, y, lz, ns)
		}
	}
}
