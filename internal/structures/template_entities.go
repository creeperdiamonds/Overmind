package structures

import (
	"encoding/binary"
	"hash/fnv"
	"math"

	"github.com/df-mc/dragonfly/server/world/chunk"
)

// Template entities (NOTES-leftovers L33). A template's entity goes into the chunk whose box holds its
// block, turned with the piece, as Java's template placement does it (the box test on the turned block
// position). The install's templates hold three kinds: minecraft:cushion (224 in the abandoned camps),
// minecraft:villager (14, village villager elements) and one minecraft:item (a firefly bush in
// campsite_swamp_1, vanilla_1.26.60).
//
// A cushion is an actor in Bedrock, not a block: BDS stores minecraft:cushion actors beside its camps in
// worlds that never ticked, so it spawns them with the structure (L33, measured on seed 100's 3 and seed
// 1's 3). Each becomes a Bedrock actor record in the chunk, with BDS's fields: Pos the template's
// position turned with the piece (the block centre in x and z, the template's y offset kept, +0.9375 for
// most), Rotation [the template's yaw + 90 per clockwise quarter turn of the piece, wrapped to
// [-180, 180), 0], Variant the colour's dye item index (cushionVariant), Persistent 1, OnGround 1, Motion 0. The Dragonfly fork
// has the entity type (third_party/dragonfly/server/entity/overmind_cushion.go).
//
// Villagers are mobs (a later stage): spawn intents. The item is not placed either: whether BDS keeps a
// template's item entity, and in which form, was not measured (no BDS swamp camp); it is kept as an
// intent with its id.

// cushionVariant is a cushion colour's Variant: the dye item's index, black 0 to white 15 (15 minus the
// wool order). MEASURED: the 6 BDS cushions (seed 100's 3, seed 1's 3) are green 2, blue 4 and
// light_blue 12 where our camps put those template colours (TestCushionsMatchBDS); L33's guess, the wool
// order white 0 to black 15, gave 0 of 6. The other 13 colours follow the same order, ASSUMED (the
// conversion code was not read).
var cushionVariant = map[string]int32{"black": 0, "red": 1, "green": 2, "brown": 3, "blue": 4, "purple": 5,
	"cyan": 6, "light_gray": 7, "gray": 8, "pink": 9, "lime": 10, "yellow": 11, "light_blue": 12,
	"magenta": 13, "orange": 14, "white": 15}

// turnPoint turns a template's continuous position by rot clockwise quarter turns about its origin
// block's corner, matching rotate for the block a position lies in (Java's transform with a zero pivot).
func turnPoint(p [3]float64, rot int) [3]float64 {
	switch rot & 3 {
	case 1:
		return [3]float64{1 - p[2], p[1], p[0]}
	case 2:
		return [3]float64{1 - p[0], p[1], 1 - p[2]}
	case 3:
		return [3]float64{p[2], p[1], 1 - p[0]}
	}
	return p
}

// wrapYaw wraps a yaw into [-180, 180).
func wrapYaw(y float64) float64 {
	return y - 360*math.Floor((y+180)/360)
}

// templateEntities places piece p's template entities that fall in the writer's chunk. heights are the
// terrain heights a terrain-matching legacy piece's blocks followed (filled here for columns no block
// read).
func (w *writer) templateEntities(s *Structures, p *piece, heights map[[2]int]int) {
	for i, e := range p.elem.template.ents {
		q := rotate(e.blockPos, p.rot)
		x, y, z := p.pos[0]+q[0], p.pos[1]+q[1], p.pos[2]+q[2]
		f := turnPoint(e.pos, p.rot)
		pos := [3]float64{float64(p.pos[0]) + f[0], float64(p.pos[1]) + f[1], float64(p.pos[2]) + f[2]}
		if p.elem.terrain && p.elem.bedrockLegacy {
			if x>>4 != w.cx || z>>4 != w.cz {
				continue
			}
			h, ok := heights[[2]int{x, z}]
			if !ok {
				h = s.g.SurfaceWG(x, z)
			}
			y = h - 1 + e.blockPos[1]
			pos[1] = float64(h-1) + e.pos[1]
		}
		if !w.in(x, y, z) {
			continue
		}
		v, ok := cushionVariant[e.color]
		if e.id != "minecraft:cushion" || !ok {
			w.spawns = append(w.spawns, spawnIntent{Pos: [3]int{x, y, z}, Entity: e.id})
			continue
		}
		yaw := wrapYaw(float64(e.yaw) + 90*float64(p.rot&3))
		w.ents = append(w.ents, chunk.Entity{ID: entityID(s.g.Seed(), pos, i), Data: map[string]any{
			"identifier":  "minecraft:cushion",
			"definitions": []string{"+minecraft:cushion"},
			"Pos":         []float32{float32(pos[0]), float32(pos[1]), float32(pos[2])},
			"Rotation":    []float32{float32(yaw), 0},
			"Motion":      []float32{0, 0, 0},
			"Variant":     v,
			"Persistent":  byte(1),
			"OnGround":    byte(1),
		}})
	}
}

// entityID is a placed template entity's unique ID: a hash of the world seed, its position and its index
// in the template, so a chunk made twice gives its entities the same IDs. BDS numbers its actors as it
// makes them (UniqueID -4294967182 for one of seed 1's cushions), which is not a function of the chunk.
func entityID(seed int64, pos [3]float64, i int) int64 {
	h := fnv.New64a()
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], uint64(seed))
	h.Write(b[:])
	for _, v := range pos {
		binary.LittleEndian.PutUint64(b[:], math.Float64bits(v))
		h.Write(b[:])
	}
	binary.LittleEndian.PutUint64(b[:], uint64(i))
	h.Write(b[:])
	return int64(h.Sum64())
}
