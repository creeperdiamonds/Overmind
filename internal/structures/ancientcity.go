package structures

import "strings"

// Ancient cities (N2 1.3, 1.7, 1.9): the legacy jigsaw placer from city_center, maximum depth 7,
// maximum distance 128, every piece at the constant start height -27.

type poolEntry struct {
	weight int
	name   string // Element name after "minecraft:ancient_city_"; "" is the empty element; a full name is a feature.
}

// ancientPools are the ancient city's pools as registered in the server (N2 1.7; the study's table
// out/anc_pools.txt, read from the registration code at 0x53ac3e0). Fallback "" is the dump's "-": the
// fallback string was not visible at the call; the study's model treats it as none.
var ancientPools = []struct {
	name, fallback string
	entries        []poolEntry
}{
	{"minecraft:ancient_city/city_center", "minecraft:empty", []poolEntry{
		{1, "city_center_city_center_1"}, {1, "city_center_city_center_2"}, {1, "city_center_city_center_3"},
	}},
	{"minecraft:ancient_city/structures", "minecraft:empty", []poolEntry{
		{7, ""}, {4, "structures_barracks"}, {4, "structures_chamber_1"}, {4, "structures_chamber_2"}, {4, "structures_chamber_3"},
		{4, "structures_sauna_1"}, {4, "structures_small_statue"}, {1, "structures_large_ruin_1"}, {1, "structures_tall_ruin_1"},
		{1, "structures_tall_ruin_2"}, {2, "structures_tall_ruin_3"}, {2, "structures_tall_ruin_4"}, {1, "structures_medium_ruin_1"},
		{1, "structures_medium_ruin_2"}, {1, "structures_small_ruin_1"}, {1, "structures_small_ruin_2"}, {1, "structures_large_pillar_1"},
		{1, "structures_medium_pillar_1"}, {1, "structures_ice_box_1"}, {1, "structures_camp_1"}, {1, "structures_camp_2"},
		{1, "structures_camp_3"},
	}},
	{"minecraft:ancient_city/sculk", "", []poolEntry{{6, "minecraft:sculk_catalyst_with_patches_city"}}},
	{"minecraft:ancient_city/walls", "", []poolEntry{
		{1, "walls_intact_corner_wall_1"}, {1, "walls_intact_intersection_wall_1"}, {1, "walls_intact_lshape_wall_1"},
		{1, "walls_intact_horizontal_wall_1"}, {1, "walls_intact_horizontal_wall_2"}, {1, "walls_intact_horizontal_wall_stairs_1"},
		{1, "walls_intact_horizontal_wall_stairs_2"}, {1, "walls_intact_horizontal_wall_stairs_3"}, {4, "walls_intact_horizontal_wall_stairs_4"},
		{3, "walls_intact_horizontal_wall_passage_1"}, {1, "walls_ruined_corner_wall_1"}, {1, "walls_ruined_corner_wall_2"},
		{2, "walls_ruined_horizontal_wall_stairs_1"}, {2, "walls_ruined_horizontal_wall_stairs_2"}, {3, "walls_ruined_horizontal_wall_stairs_3"},
		{3, "walls_ruined_horizontal_wall_stairs_4"},
	}},
	{"minecraft:ancient_city/walls/no_corners", "minecraft:empty", []poolEntry{
		{1, "walls_intact_horizontal_wall_1"}, {1, "walls_intact_horizontal_wall_2"}, {1, "walls_intact_horizontal_wall_stairs_1"},
		{1, "walls_intact_horizontal_wall_stairs_2"}, {1, "walls_intact_horizontal_wall_stairs_3"}, {1, "walls_intact_horizontal_wall_stairs_4"},
		{1, "walls_intact_horizontal_wall_bridge"},
	}},
	{"minecraft:ancient_city/city_center/walls", "minecraft:empty", []poolEntry{
		{1, "city_center_walls_bottom_1"}, {1, "city_center_walls_bottom_2"}, {1, "city_center_walls_bottom_left_corner"},
		{1, "city_center_walls_bottom_right_corner_1"}, {1, "city_center_walls_bottom_right_corner_2"}, {1, "city_center_walls_left"},
		{1, "city_center_walls_right"}, {1, "city_center_walls_top"}, {1, "city_center_walls_top_right_corner"},
		{1, "city_center_walls_top_left_corner"},
	}},
	{"minecraft:ancient_city/city/entrance", "minecraft:empty", []poolEntry{
		{1, "city_entrance_entrance_connector"}, {1, "city_entrance_entrance_path_1"}, {1, "city_entrance_entrance_path_2"},
		{1, "city_entrance_entrance_path_3"}, {1, "city_entrance_entrance_path_4"}, {1, "city_entrance_entrance_path_5"},
	}},
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
						el = &element{name: "minecraft:ancient_city_" + e.name, template: t}
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
