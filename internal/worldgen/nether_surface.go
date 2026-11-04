package worldgen

import (
	"encoding/json"
	"regexp"
	"strconv"
)

// The Nether's surface (Task 10). The biome files give each Nether biome's builder: hell, crimson and
// warped forests use the default ("overworld") builder with the lava sea, soul sand valley and basalt
// deltas the "capped" one. NOT READ from the code: how the default builder runs in the Nether. It is
// taken to be the Overworld's (S1) with the Nether's sea (Y 32, lava), its column float drawn from the
// bedrock rows' stream after the roof, and the generator's 3-octave surface simplex as the depth noise;
// the capped builder is not written, so those biomes keep plain netherrack. TestNetherMatchesBDS
// measures the result.

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

// readNetherSurfaces reads the Nether biomes' default builders from the data.
func readNetherSurfaces(bd *BedrockData) (out [5]netherSurfaceBiome) {
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
				Scale float32                    `json:"noise_frequency_scale"`
				Range [2]float32                 `json:"noise_range"`
				H     []string                   `json:"height_range"`
			} `json:"adjustments"`
		}
		json.Unmarshal(b.components["minecraft:surface_material_adjustments"], &adj)
		for _, a := range adj.A {
			na := netherAdjust{surfaceAdj: surfaceAdj{scale: a.Scale, lo: a.Range[0], hi: a.Range[1]}}
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

// netherColumn builds one column with the default builder: top material on the first rock under air,
// mid material for depth blocks, sea floor material under the lava sea.
func (n *NetherGenerator) netherColumn(out *[netherHeight * 256]bs, r *bmt, x, z int, nsb *netherSurfaceBiome, depthN float32) {
	lx, lz := x&15, z&15
	f := r.nextFloat()
	if !nsb.ok {
		return
	}
	depth := int32(float32(float32(f*0.25)+float32(depthN/3.0)) + 3.0)
	run := int32(-1)
	water := int32(noSeaLevel)
	for y := netherHeight - 1; y >= 0; y-- {
		i := y<<8 | lz<<4 | lx
		blk := out[i]
		if blk.isAir() {
			run, water = -1, noSeaLevel
			continue
		}
		if blk == sBedrock {
			continue
		}
		if blk != sNetherrack {
			if blk.has(fLava) {
				water = max(water, int32(y)+1)
			}
			continue
		}
		cur := nsb.mats
		for k := range nsb.adj {
			a := &nsb.adj[k]
			if a.hasRange && (y < a.yLo || y > a.yHi) {
				continue
			}
			if v := n.adjustValue(&a.surfaceAdj, x, z); a.lo <= v && v < a.hi {
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
		}
		yy := int32(y)
		switch {
		case run == -1:
			run = depth
			topM, midM := cur.top, cur.mid
			if depth <= 0 {
				topM, midM = sAir, cur.foundation
			}
			switch {
			case yy >= water-1:
				if !topM.isAir() {
					out[i] = topM
				}
			case yy < water-(nsb.seaDepth+depth):
				out[i] = cur.floor
			default:
				out[i] = midM
			}
		case run > 0:
			out[i] = cur.mid
			run--
		}
	}
}

// adjustValue is the adjustment noise at a column (the Overworld's builder's, S1).
func (n *NetherGenerator) adjustValue(a *surfaceAdj, x, z int) float32 {
	if n.adj == nil {
		return 0
	}
	return n.adj.adjustValue(a, x, z)
}
