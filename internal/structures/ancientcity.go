package structures

import (
	"strings"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Ancient cities (N2 1.3, 1.7, 1.9): the legacy jigsaw placer from city_center, maximum depth 7,
// maximum distance 128, every piece at the constant start height -27.

type poolEntry struct {
	weight int
	name   string // Element name after "minecraft:ancient_city_"; "" is the empty element; a full name is a feature.
}

const (
	ancientMaxDepth = 7   // N2 1.3.
	ancientMaxDist  = 128 // N2 1.3.
	ancientY        = -27 // AncientCityPiece start height (N2 1.3); also the biome test's Y (N1 2.2).
	ancientReach    = 10  // Chunks from the start a city's pieces can reach: 128 blocks plus a piece.
)

// pools builds the ancient city's pools from the install's templates, once.
func (d *Data) ancientPools() map[string]*pool {
	d.ancientOnce.Do(func() {
		byName := map[string]string{}
		for path := range d.templates {
			if strings.HasPrefix(path, "ancient_city/") {
				byName[strings.ReplaceAll(path, "/", "_")] = path
			}
		}
		els := map[string]*element{}
		d.ancient = map[string]*pool{"minecraft:empty": {}}
		for _, p := range ancientPools {
			pl := &pool{fallback: p.fallback}
			for _, e := range p.entries {
				var el *element
				switch {
				case e.name == "":
				case strings.HasPrefix(e.name, "minecraft:"):
					el = &element{name: e.name} // A feature element: the sculk patch is placed (sculkElement).
				default:
					if el = els[e.name]; el == nil {
						path, ok := byName["ancient_city_"+e.name]
						var t *Template
						if ok {
							t = d.Template(path)
						}
						if t == nil {
							d.problem("ancient city element %s: no template", e.name)
							continue
						}
						el = &element{name: "minecraft:ancient_city_" + e.name, template: t, bedrockLegacy: true, rules: ancientListOf(e.name)}
						els[e.name] = el
					}
				}
				for range e.weight {
					pl.elems = append(pl.elems, el)
				}
			}
			d.ancient[p.name] = pl
		}
	})
	return d.ancient
}

// ancientCity assembles a city from its start (N2 1.3).
func (s *Structures) ancientCity(st Start) []*piece {
	pools := s.data.ancientPools()
	r := st.random()
	rot := int(r.NextIntN(4))
	centre := pools["minecraft:ancient_city/city_center"]
	if len(centre.elems) == 0 {
		return nil
	}
	start := centre.elems[r.NextIntN(int32(len(centre.elems)))]
	return assemble(r, pools, start, st.CX*16, st.CZ*16, rot, "minecraft:city_anchor", ancientMaxDepth, ancientMaxDist,
		func(int, int) int { return ancientY })
}

// The city's sculk feature elements (sculk_catalyst_with_patches_city), NOTES-loot-trial-bastion T4: each
// runs on a fresh MT seeded from its position (Mth.getSeed with 64-bit products, low 32 bits), so it is
// independent of the chunk. The start test, the catalyst and the shriekers follow T4. The spreader
// (Java's world generation sculk spreader on that MT) is not BDS-stable (NOTES-bedrock-exact T37), and
// here it is our own simple deterministic stand-in, labelled: 10 cursors of charge 32 each walk the 18
// edge neighbours (nextInt(18)) inside the element's chunk, turning a sculk-replaceable block with an
// open face into sculk; no veins.
const sculkCityFeature = "minecraft:sculk_catalyst_with_patches_city"

var sculkCityReplaceable = nameSet("deepslate cobbled_deepslate polished_deepslate deepslate_bricks cracked_deepslate_bricks " +
	"deepslate_tiles cracked_deepslate_tiles tuff stone granite diorite andesite dirt grass_block gravel sand calcite smooth_basalt clay")

// sculkElementRandom is T4's seed: Mth.getSeed of the position with 64-bit products, low 32 bits.
func sculkElementRandom(x, y, z int) *worldgen.BedrockRandom {
	l := int64(x)*3129871 ^ int64(z)*116129781 ^ int64(y)
	return worldgen.NewBedrockRandom(uint32(uint64((l*42317861+11)*l) >> 16))
}

func (s *Structures) sculkElement(w *writer, p [3]int) {
	solid := func(x, y, z int) bool { return w.in(x, y, z) && fullCube(w.chunk().Block(uint8(x&15), int16(y), uint8(z&15), 0)) }
	name := func(x, y, z int) string {
		if !w.in(x, y, z) {
			return ""
		}
		return w.blockName(x, y, z)
	}
	if n := name(p[0], p[1], p[2]); n != "sculk" && n != "sculk_vein" {
		if n != "air" {
			return
		}
		ok := false
		for _, d := range [6][3]int{{0, 1, 0}, {0, -1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if solid(p[0]+d[0], p[1]+d[1], p[2]+d[2]) {
				ok = true
				break
			}
		}
		if !ok {
			return
		}
	}
	r := sculkElementRandom(p[0], p[1], p[2])
	var edge [][3]int
	for z := -1; z <= 1; z++ {
		for y := -1; y <= 1; y++ {
			for x := -1; x <= 1; x++ {
				if (x == 0 || y == 0 || z == 0) && !(x == 0 && y == 0 && z == 0) {
					edge = append(edge, [3]int{x, y, z})
				}
			}
		}
	}
	sculk := ridNamed("sculk", nil)
	open := func(x, y, z int) bool {
		for _, d := range [6][3]int{{0, 1, 0}, {0, -1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}} {
			if name(x+d[0], y+d[1], z+d[2]) == "air" {
				return true
			}
		}
		return false
	}
	for range 10 {
		c := p
		for range 32 {
			d := edge[r.NextIntN(int32(len(edge)))]
			q := [3]int{c[0] + d[0], c[1] + d[1], c[2] + d[2]}
			switch n := name(q[0], q[1], q[2]); {
			case n == "sculk":
				c = q
			case sculkCityReplaceable[n] && open(q[0], q[1], q[2]):
				w.set(q[0], q[1], q[2], sculk, false)
				c = q
			}
		}
	}
	if r.NextFloat() <= 0.5 && solid(p[0], p[1]-1, p[2]) && w.in(p[0], p[1], p[2]) {
		w.set(p[0], p[1], p[2], ridNamed("sculk_catalyst", nil), false)
	}
	for range 1 + int(r.NextIntN(3)) {
		dx, dz := int(r.NextIntN(5)), int(r.NextIntN(5))
		q := [3]int{p[0] + dx - 2, p[1], p[2] + dz - 2}
		if name(q[0], q[1], q[2]) == "air" && solid(q[0], q[1]-1, q[2]) {
			w.set(q[0], q[1], q[2], ridNamed("sculk_shrieker", map[string]any{"can_summon": true}), false)
		}
	}
}
