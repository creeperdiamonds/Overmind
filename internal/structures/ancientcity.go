package structures

import "strings"

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
					el = &element{name: e.name} // A feature element; its feature is not placed (NOTES.md).
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
