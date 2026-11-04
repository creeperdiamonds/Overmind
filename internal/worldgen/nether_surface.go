package worldgen

import (
	"encoding/json"
	"regexp"
	"strconv"
)

// The Nether's surface (Task 10), read from the code (NOTES-bedrock-nether N3, R2-1, R2-2). The biome
// files give each Nether biome's builder: hell, crimson and warped forests use the default
// ("overworld") builder against the fixed lava sea, soul sand valley and basalt deltas the capped one.
// Both run per column, x outer, on the bedrock rows' MT after its 512 draws, with the generator's
// 4-octave surface simplex as the depth noise.

const netherSea = 32

// netherAdjust is a surface adjustment with an optional height range.
type netherAdjust struct {
	surfaceAdj
	yLo, yHi int
	hasRange bool
}

// netherSurfaceBiome is a Nether biome's builder as read.
type netherSurfaceBiome struct {
	ok       bool
	mats     surfaceMats
	seaDepth int32
	adj      []netherAdjust
	capped   *cappedBuilder // The capped builder; nil for the default one.
}

// cappedBuilder is CappedSurfaceBuilder (build 0xccee4f0) for one biome: floor and ceiling materials,
// each with its one-octave noise, and the beach (R2-1). The noises are per world: floor i from an MT
// of the level seed + i, ceiling j from seed + #floor + j, the beach from the seed itself.
type cappedBuilder struct {
	floor, ceil []bs
	floorN      []legacyOctave
	ceilN       []legacyOctave
	beach       bs // Air for none.
	beachN      legacyOctave
}

func newCappedBuilder(seed int64, floor, ceil []bs, beach bs) *cappedBuilder {
	s := bedrockLevelSeed(seed)
	c := &cappedBuilder{floor: floor, ceil: ceil, beach: beach}
	oct := func(k uint32) legacyOctave { return newLegacyOctave(newBMT(s + k)) }
	c.beachN = oct(0)
	for i := range floor {
		c.floorN = append(c.floorN, oct(uint32(i)))
	}
	for j := range ceil {
		c.ceilN = append(c.ceilN, oct(uint32(len(floor)+j)))
	}
	return c
}

// pickCapped is the material whose noise is highest at the column (strictly, from -1); one material
// is taken without a sample.
func pickCapped(mats []bs, ns []legacyOctave, x, z float32) bs {
	if len(mats) == 1 {
		return mats[0]
	}
	best, m := float32(-1), mats[0]
	for i := range mats {
		if v := ns[i].improved(x, netherSea, z); best < v {
			best, m = v, mats[i]
		}
	}
	return m
}

var seaLevelExpr = regexp.MustCompile(`^\s*variable\.sea_level\s*(?:([+-])\s*(\d+))?\s*$`)

func seaLevelOffset(s string) (int, bool) {
	m := seaLevelExpr.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	v := netherSea
	if m[1] != "" {
		n, _ := strconv.Atoi(m[2])
		if m[1] == "-" {
			n = -n
		}
		v += n
	}
	return v, true
}

// readNetherSurfaces reads the Nether biomes' builders from the data.
func readNetherSurfaces(seed int64, bd *BedrockData) (out [5]netherSurfaceBiome) {
	if bd == nil {
		return out
	}
	str := func(m map[string]json.RawMessage, k string) string {
		if raw, ok := m[k]; ok {
			if n, ok := packBlockName(raw); ok {
				return n
			}
		}
		return ""
	}
	for i := range out {
		b := bd.biomes[biomeRowDefs[bNetherWastes+uint8(i)].String()]
		if b == nil {
			continue
		}
		var sb struct {
			Builder map[string]json.RawMessage `json:"builder"`
		}
		json.Unmarshal(b.components["minecraft:surface_builder"], &sb)
		var kind string
		json.Unmarshal(sb.Builder["type"], &kind)
		if kind == "minecraft:capped" {
			list := func(k string) (l []bs) {
				var names []json.RawMessage
				json.Unmarshal(sb.Builder[k], &names)
				for _, raw := range names {
					if n, ok := packBlockName(raw); ok {
						l = append(l, surfaceState(n))
					}
				}
				return l
			}
			fl, ce := list("floor_materials"), list("ceiling_materials")
			if len(fl) == 0 || len(ce) == 0 {
				continue
			}
			out[i] = netherSurfaceBiome{ok: true, capped: newCappedBuilder(seed, fl, ce, surfaceState(str(sb.Builder, "beach_material")))}
			continue
		}
		if kind != "minecraft:overworld" {
			continue
		}
		var depth int32
		json.Unmarshal(sb.Builder["sea_floor_depth"], &depth)
		ns := &out[i]
		ns.ok, ns.seaDepth = true, depth
		ns.mats = surfaceMats{surfaceState(str(sb.Builder, "top_material")), surfaceState(str(sb.Builder, "mid_material")),
			surfaceState(str(sb.Builder, "sea_floor_material")), surfaceState(str(sb.Builder, "foundation_material")),
			surfaceState(str(sb.Builder, "sea_material"))}
		var adj struct {
			A []struct {
				M     map[string]json.RawMessage `json:"materials"`
				Scale *float32                   `json:"noise_frequency_scale"`
				Range [2]float32                 `json:"noise_range"`
				H     []string                   `json:"height_range"`
			} `json:"adjustments"`
		}
		json.Unmarshal(b.components["minecraft:surface_material_adjustments"], &adj)
		for _, a := range adj.A {
			scale := float32(1) // The default when the file gives none (crimson forest's wart band, R2-2).
			if a.Scale != nil {
				scale = *a.Scale
			}
			na := netherAdjust{surfaceAdj: surfaceAdj{scale: scale, lo: a.Range[0], hi: a.Range[1]}}
			if len(a.H) == 2 {
				lo, ok1 := seaLevelOffset(a.H[0])
				hi, ok2 := seaLevelOffset(a.H[1])
				if !ok1 || !ok2 {
					continue
				}
				na.yLo, na.yHi, na.hasRange = lo, hi, true
			}
			for k, key := range [5]string{"top_material", "mid_material", "sea_floor_material", "foundation_material", "sea_material"} {
				n := str(a.M, key)
				if n == "" {
					continue
				}
				na.has[k] = true
				s := surfaceState(n)
				switch k {
				case 0:
					na.mats.top = s
				case 1:
					na.mats.mid = s
				case 2:
					na.mats.floor = s
				case 3:
					na.mats.foundation = s
				case 4:
					na.mats.sea = s
				}
			}
			ns.adj = append(ns.adj, na)
		}
	}
	return out
}

// netherColumn builds one column with its biome's builder.
func (n *netherGen) netherColumn(out *[netherHeight * 256]bs, r *bmt, x, z int, nsb *netherSurfaceBiome, depthN float32) {
	if nsb.capped != nil {
		n.cappedColumn(out, r, x, z, nsb.capped, depthN)
		return
	}
	n.defaultColumn(out, r, x, z, nsb, depthN)
}

// defaultColumn is the default builder in the Nether (N3, R2-2): from Y 127 down to 1, past bedrock and
// any block other than netherrack (lava included), a run of depth blocks under each opening. A run's
// materials are chosen at its first block: the biome's, with the adjustments whose noise and height
// range hold there. At or above Y 31 (the sea's top) that block takes the top material; below, the sea
// floor material deeper than the sea floor depth plus depth under the sea, else the mid one.
func (n *netherGen) defaultColumn(out *[netherHeight * 256]bs, r *bmt, x, z int, nsb *netherSurfaceBiome, depthN float32) {
	lx, lz := x&15, z&15
	f := r.nextFloat()
	if !nsb.ok {
		return
	}
	depth := int32(float32(float32(f*0.25)+float32(depthN/3)) + 3)
	hit := make([]bool, len(nsb.adj))
	for k := range nsb.adj {
		a := &nsb.adj[k]
		v := n.adjustValue(&a.surfaceAdj, x, z)
		hit[k] = a.lo <= v && v < a.hi
	}
	run := int32(-1)
	midM := nsb.mats.mid
	for y := netherHeight - 1; y > 0; y-- {
		i := y<<8 | lz<<4 | lx
		blk := out[i]
		switch {
		case blk == sBedrock:
			continue
		case blk.isAir():
			run = -1
			continue
		case blk != sNetherrack:
			continue
		}
		if run > 0 {
			out[i] = midM
			run--
			continue
		}
		if run == 0 {
			continue
		}
		cur := nsb.mats
		for k := range nsb.adj {
			a := &nsb.adj[k]
			if !hit[k] || a.hasRange && (y < a.yLo || y > a.yHi) {
				continue
			}
			if a.has[0] {
				cur.top = a.mats.top
			}
			if a.has[1] {
				cur.mid = a.mats.mid
			}
			if a.has[2] {
				cur.floor = a.mats.floor
			}
			if a.has[3] {
				cur.foundation = a.mats.foundation
			}
		}
		top, mid := cur.top, cur.mid
		midM = mid
		if depth <= 0 {
			top, mid = sAir, cur.foundation
		}
		run = depth
		switch {
		case y >= netherSea-1:
			out[i] = top
		case int32(y) < netherSea-(nsb.seaDepth+depth):
			out[i] = cur.floor
		default:
			out[i] = mid
		}
	}
}

// cappedColumn is CappedSurfaceBuilder for one column (R2-1): two floats from the MT, the floor and
// ceiling materials by noise, the beach by noise; then up the column, under every top of solid ground
// (solid: neither air nor lava) up to d1 blocks of floor (beach from Y 27 to 32), over every bottom up
// to d2 blocks of ceiling, each run stopping at the first open block.
func (n *netherGen) cappedColumn(out *[netherHeight * 256]bs, r *bmt, x, z int, c *cappedBuilder, depthN float32) {
	lx, lz := x&15, z&15
	r1, r2 := r.nextFloat(), r.nextFloat()
	fx, fz := float32(float32(x)*0.0625), float32(float32(z)*0.0625)
	floor := pickCapped(c.floor, c.floorN, fx, fz)
	ceil := pickCapped(c.ceil, c.ceilN, fx, fz)
	beach := !c.beach.isAir() && c.beachN.improved(float32(float32(x)*0.03125), 109, float32(float32(z)*0.03125)) > 0
	d1 := int(float32(float32(float32(r1*0.25)+float32(depthN/3)) + 3))
	d2 := int(float32(r2*0.25) + float32(float32(depthN/3)+3))
	solid := func(y int) bool { s := out[y<<8|lz<<4|lx]; return !s.isAir() && !s.has(fLava) }
	prev := solid(0)
	for y := range netherHeight {
		cur := solid(y)
		if cur == prev {
			continue
		}
		if !cur {
			lo := max(y-d1-1, -1)
			for yy := y - 1; yy > lo && solid(yy); yy-- {
				m := floor
				if beach && yy >= netherSea-5 && yy <= netherSea {
					m = c.beach
				}
				out[yy<<8|lz<<4|lx] = m
			}
		} else {
			hi := min(y+d2, netherHeight)
			for yy := y; yy < hi && solid(yy); yy++ {
				out[yy<<8|lz<<4|lx] = ceil
			}
		}
		prev = cur
	}
}

// adjustValue is the adjustment noise at a column: the Overworld builder's formula (S1) on the Nether
// generator's own 3-octave simplex (R2-2).
func (n *netherGen) adjustValue(a *surfaceAdj, x, z int) float32 {
	return n.adj.adjustValue(a, x, z)
}

// netherSurfacesBuiltIn is the Nether biomes' builders as the install's biome files give them (the
// study's reading, NOTES-bedrock-nether R2-1 and R2-2), for a world with no install read: the
// Overjava profile, or the Bedrock profile without one. TestNetherSurfacesBuiltInMatchData holds it to
// the files.
func netherSurfacesBuiltIn(seed int64) (out [5]netherSurfaceBiome) {
	st := surfaceState
	mats := func(top, mid, floor string) surfaceMats {
		return surfaceMats{st(top), st(mid), st(floor), st("netherrack"), st("lava")}
	}
	band := func(scale, lo, hi float32, yLo, yHi int, ranged bool, m surfaceMats, has [5]bool) netherAdjust {
		return netherAdjust{surfaceAdj: surfaceAdj{scale: scale, lo: lo, hi: hi, mats: m, has: has}, yLo: yLo, yHi: yHi, hasRange: ranged}
	}
	topFloor := [5]bool{true, false, true}
	out[bNetherWastes-bNetherWastes] = netherSurfaceBiome{ok: true, mats: mats("netherrack", "netherrack", "netherrack"), seaDepth: 7, adj: []netherAdjust{
		band(0.03125, -1, -0.4, netherSea-3, netherSea+2, true, surfaceMats{top: st("gravel"), floor: st("gravel")}, topFloor),
		band(0.03125, 0.4, 1, netherSea-3, netherSea+2, true, surfaceMats{top: st("soul_sand"), mid: st("soul_sand"), floor: st("soul_sand")}, [5]bool{true, true, true}),
	}}
	out[bCrimsonForest-bNetherWastes] = netherSurfaceBiome{ok: true, mats: mats("crimson_nylium", "netherrack", "crimson_nylium"), seaDepth: 1, adj: []netherAdjust{
		band(0.1, 0.4, 1, 0, 0, false, surfaceMats{top: st("netherrack"), floor: st("netherrack")}, topFloor),
		band(1, -0.75, -0.5, 0, 0, false, surfaceMats{top: st("nether_wart_block"), floor: st("nether_wart_block")}, topFloor),
	}}
	out[bWarpedForest-bNetherWastes] = netherSurfaceBiome{ok: true, mats: mats("warped_nylium", "netherrack", "warped_nylium"), seaDepth: 1, adj: []netherAdjust{
		band(0.1, 0.4, 1, 0, 0, false, surfaceMats{top: st("netherrack"), floor: st("netherrack")}, topFloor),
	}}
	soul := []bs{st("soul_sand"), st("soul_soil")}
	out[bSoulSandValley-bNetherWastes] = netherSurfaceBiome{ok: true, capped: newCappedBuilder(seed, soul, soul, st("gravel"))}
	out[bBasaltDeltas-bNetherWastes] = netherSurfaceBiome{ok: true, capped: newCappedBuilder(seed, []bs{st("basalt"), st("blackstone")}, []bs{st("basalt")}, sAir)}
	return out
}
