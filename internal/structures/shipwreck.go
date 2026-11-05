package structures

import (
	"math"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Shipwrecks (ShipwreckPiece postProcess 0x5386700: NOTES-structures-phase2 P9.6, cited P96). The wreck is
// built whole by the post-process of its start chunk, with that chunk's post-process random R; each of
// our chunks writes the part of it inside itself, worked out again from the start chunk's R.
//   - beached when the biome at the anchor is 15, 16 or 26; rotation nextInt(4); template nextInt(20),
//     then nextInt(14) when beached (both draws), from P96's table; then nextInt(9) (a pair not decoded);
//   - the template turns about the anchor shifted by rot0 (0,0), rot1 (+15,0), rot2 (+15,+15),
//     rot3 (0,+15);
//   - chests below the data markers mapChest, supplyChest and treasureChest, with LootTableSeed =
//     R.nextInt() each. P96 left draws 3 to 5 unidentified and the marker order open. Here the first chest
//     takes draw 7, then treasure, map, one more draw, supply (what seed 12345's three-chest wreck shows).
//     Not settled: 3 of 9 chests get BDS's seed (TestShipwrecksMatchBDS); in the other wrecks BDS has no
//     treasure chest where our template has a marker, and its map chest takes draw 7 (draw 6 in one).
//   - y, read but not checked in P96: the mean (the minimum when beached) of the sea floor height over the
//     footprint, plus -2 (template under 14 tall) or -4, at least sea level when beached. The sea floor
//     here is the noise stage's top solid block.
//   - air and structure blocks of the template are not placed (Java's shipwreck ignores both; not read
//     for BDS).

var shipwreckTemplates = [20]string{"swwithmast", "swwithmastdegraded", "swsidewaysfull", "swsidewaysfronthalf",
	"swsidewaysbackhalf", "swsidewaysfulldegraded", "swsidewaysfronthalfdegraded", "swsidewaysbackhalfdegraded",
	"swrightsideupfull", "swrightsideupfronthalf", "swrightsideupbackhalf", "swrightsideupfulldegraded",
	"swrightsideupfronthalfdegraded", "swrightsideupbackhalfdegraded", "swupsidedownfull", "swupsidedownfronthalf",
	"swupsidedownbackhalf", "swupsidedownfulldegraded", "swupsidedownfronthalfdegraded", "swupsidedownbackhalfdegraded"}

var shipwreckShift = [4][2]int{{0, 0}, {15, 0}, {15, 15}, {0, 15}}

var shipwreckLoot = map[string]string{"mapChest": "loot_tables/chests/shipwreck.json",
	"supplyChest": "loot_tables/chests/shipwrecksupply.json", "treasureChest": "loot_tables/chests/shipwrecktreasure.json"}

const shipwreckReach = 2 // Chunks a wreck reaches from its start (28 blocks at most, from the shifted anchor).

type wreck struct {
	t      *Template
	rot    int
	origin [3]int
	chests map[int]chestSpec // Template block index of a chest to its table and seed.
}

type chestSpec struct {
	table string
	seed  int32
}

// shipwreck works out the wreck of a start chunk.
func (s *Structures) shipwreck(cx, cz int) *wreck {
	x0, z0 := cx*16, cz*16
	b := s.g.BiomeID(x0, seaLevelField, z0)
	beached := b == 15 || b == 16 || b == 26
	r := worldgen.NewBedrockRandom(s.g.DecorationSeed(cx, cz))
	rot := int(r.NextIntN(4))
	t := int(r.NextIntN(20))
	draws := 2
	if beached {
		t = int(r.NextIntN(14))
		draws++
	}
	r.NextIntN(9)
	r.Consume(7 - (draws + 1)) // The first chest takes draw 7 (see the top).
	tp := s.data.Template("shipwreck/" + shipwreckTemplates[t])
	if tp == nil {
		return nil
	}
	sh := shipwreckShift[rot]
	ox, oz := x0+sh[0], z0+sh[1]
	// The footprint's sea floor.
	e := &element{template: tp}
	fb := e.bbox([3]int{ox, 0, oz}, rot)
	sum, n, low := 0, 0, math.MaxInt
	for x := fb[0]; x <= fb[3]; x++ {
		for z := fb[2]; z <= fb[5]; z++ {
			h := s.g.NoiseHeight(x, z)
			sum += h
			low = min(low, h)
			n++
		}
	}
	y := sum / n
	if beached {
		y = low
	}
	if tp.Size[1] < 14 {
		y -= 2
	} else {
		y -= 4
	}
	if beached {
		y = max(y, seaLevelField)
	}
	wk := &wreck{t: tp, rot: rot, origin: [3]int{ox, y, oz}, chests: map[int]chestSpec{}}
	chestOf := map[string]int{}
	for _, bl := range tp.blocks {
		if tp.palette[bl.state].name != "structure_block" {
			continue
		}
		meta, _ := bl.nbt["metadata"].(string)
		if _, ok := shipwreckLoot[meta]; !ok {
			continue
		}
		below := [3]int{bl.pos[0], bl.pos[1] - 1, bl.pos[2]}
		for i, c := range tp.blocks {
			if c.pos == below {
				chestOf[meta] = i
			}
		}
	}
	for _, m := range []string{"treasureChest", "mapChest", "", "supplyChest"} {
		if m == "" {
			r.NextInt() // The measured draw between the map and supply chests.
			continue
		}
		if i, ok := chestOf[m]; ok {
			wk.chests[i] = chestSpec{shipwreckLoot[m], r.NextInt()}
		}
	}
	return wk
}

// placeShipwrecks writes the part of every wreck near the chunk that lies inside it.
func (s *Structures) placeShipwrecks(w *writer) {
	for x := w.cx - shipwreckReach; x <= w.cx+shipwreckReach; x++ {
		for z := w.cz - shipwreckReach; z <= w.cz+shipwreckReach; z++ {
			if _, ok := s.StartIn(Shipwreck, x, z); !ok {
				continue
			}
			wk := s.shipwreck(x, z)
			if wk == nil {
				continue
			}
			for i, b := range wk.t.blocks {
				q := rotate(b.pos, wk.rot)
				wx, wy, wz := wk.origin[0]+q[0], wk.origin[1]+q[1], wk.origin[2]+q[2]
				if !w.in(wx, wy, wz) {
					continue
				}
				st := wk.t.palette[b.state]
				if st.name == "structure_block" || st.name == "structure_void" || st.name == "air" || st.name == "jigsaw" {
					continue
				}
				res := convert.state(st, wk.rot)
				if !res.ok {
					continue
				}
				water := res.waterlog || w.c.Block(uint8(wx&15), int16(wy), uint8(wz&15), 0) == waterRID()
				if c, ok := wk.chests[i]; ok && res.name == "chest" {
					w.set(wx, wy, wz, res.rid, water)
					w.bes = append(w.bes, chestEntity(wx, wy, wz, c.table, c.seed))
					continue
				}
				w.set(wx, wy, wz, res.rid, water)
			}
		}
	}
}
