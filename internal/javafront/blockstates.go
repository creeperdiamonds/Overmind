package javafront

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// The Java state of a block depends on more than its Dragonfly runtime ID in
// three ways, all handled here:
//
//   - Water inside a block. Dragonfly keeps it on the chunk's second layer, as
//     Bedrock does; Java has it as the block's waterlogged property. A block
//     whose Java counterpart cannot be waterlogged shows without the water.
//   - A paired chest. Dragonfly keeps the pair in the chest's block entity
//     (pairx, pairz); Java has it as the chest's type, left or right.
//   - The colour of a bed or banner. Dragonfly keeps it in the block entity,
//     as Bedrock does; Java has a block per colour (red_bed).
//
// Connections (fences, panes, bars, fire, chorus plants, redstone wire) are in
// the runtime ID itself, since Dragonfly works them out for Bedrock 1.26, and
// java261.JavaState translates them. Walls, doors (each half carries its
// facing, hinge and open), tall plants (upper and lower) and stairs shapes
// (Dragonfly's corner) map by the tables alone.

var (
	waterOnce sync.Once
	waterRIDs []bool
)

// isWater says whether a Dragonfly runtime ID is water.
func isWater(rid uint32) bool {
	waterOnce.Do(func() {
		n := world.DefaultBlockRegistry.BlockCount()
		waterRIDs = make([]bool, n)
		for i := 0; i < n; i++ {
			if b, ok := world.BlockByRuntimeID(uint32(i)); ok {
				_, waterRIDs[i] = b.(block.Water)
			}
		}
	})
	return int(rid) < len(waterRIDs) && waterRIDs[rid]
}

// stateOf is the Java state of a block from its two Dragonfly layers and its
// block entity (nil if none): the tables' state for the first layer, made
// waterlogged when the second holds water, and a paired chest's half. fluid
// reports whether Java sees a fluid there (water, lava or a waterlogged block).
func stateOf(pos cube.Pos, rid, liquid uint32, be world.Block) (state int32, fluid bool) {
	s := java261.JavaState(rid)
	fluid = isFluid(rid)
	if isWater(liquid) {
		if w, ok := java261.WithProperty(s, "waterlogged", "true"); ok {
			s, fluid = w, true
		}
	}
	switch b := be.(type) {
	case block.Chest:
		if b.Paired() {
			s = chestHalf(s, pos, b)
		}
	case block.Bed:
		s = coloured(s, b.Colour)
	case block.Banner:
		s = coloured(s, b.Colour)
	}
	return int32(s), fluid
}

// coloured is a bed or banner state in its colour. Bedrock keeps the colour in
// the block entity and its block state has none, so the tables give the black
// block (black_bed, black_banner, black_wall_banner); Java has a block per
// colour, with the same properties.
func coloured(s uint16, c item.Colour) uint16 {
	rest, ok := strings.CutPrefix(java261.BlockName(s), "minecraft:black_")
	if !ok {
		return s
	}
	out, _ := java261.AsBlock(s, "minecraft:"+c.String()+"_"+rest)
	return out
}

// needsEntity says whether a block's Java state needs its block entity
// (stateOf): a paired chest, a bed or a banner.
func needsEntity(b world.Block) bool {
	switch v := b.(type) {
	case block.Chest:
		return v.Paired()
	case block.Bed, block.Banner:
		return true
	}
	return false
}

// chestHalf sets a paired chest's Java type. Java names the half by the side
// its partner is on, seen from the chest's facing: "left" when the partner is
// clockwise of the facing (facing east, at z+1), "right" when it is
// counterclockwise. Source: GeyserMC's DoubleChestBlockEntityTranslator
// (translateChestValue, which works the partner out from Java's facing and
// type: "case EAST -> z = z + (isLeft ? 1 : -1)"). minecraft.wiki's "Chest"
// says only "the direction the chest has a connection with".
func chestHalf(s uint16, pos cube.Pos, ch block.Chest) uint16 {
	nbt := ch.EncodeNBT()
	px, okX := nbt["pairx"].(int32)
	pz, okZ := nbt["pairz"].(int32)
	facing, okF := java261.Property(s, "facing")
	if !okX || !okZ || !okF {
		return s
	}
	d := [2]int{int(px) - pos[0], int(pz) - pos[2]}
	clockwise := map[string][2]int{"north": {1, 0}, "east": {0, 1}, "south": {-1, 0}, "west": {0, -1}}[facing]
	half := ""
	switch d {
	case clockwise:
		half = "left"
	case [2]int{-clockwise[0], -clockwise[1]}:
		half = "right"
	default:
		return s
	}
	out, _ := java261.WithProperty(s, "type", half)
	return out
}

// columnState is the Java state of the block at pos in a column.
func columnState(col *world.Column, pos cube.Pos) int32 {
	x, y, z := uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15)
	s, _ := stateOf(pos, col.Block(x, y, z, 0), col.Block(x, y, z, 1), col.BlockEntities[pos])
	return s
}

// sectionExtras are the blocks of one chunk whose Java state needs their block
// entity (paired chests, beds, banners), by section and index in it, worked
// out by stateOf.
func sectionExtras(cx, cz int32, c *chunk.Chunk, blockEntities map[cube.Pos]world.Block) map[int]map[int]int32 {
	var out map[int]map[int]int32
	minY := c.Range().Min()
	for pos, b := range blockEntities {
		if !needsEntity(b) || pos[0]>>4 != int(cx) || pos[2]>>4 != int(cz) {
			continue
		}
		x, y, z := uint8(pos[0]&15), int16(pos[1]), uint8(pos[2]&15)
		s, _ := stateOf(pos, c.Block(x, y, z, 0), c.Block(x, y, z, 1), b)
		if out == nil {
			out = map[int]map[int]int32{}
		}
		sec := (pos[1] - minY) >> 4
		if out[sec] == nil {
			out[sec] = map[int]int32{}
		}
		out[sec][int(y-int16(minY))&15<<8|int(z)<<4|int(x)] = s
	}
	return out
}

// airRID is Dragonfly's runtime ID of air.
var airRID = sync.OnceValue(func() uint32 { return world.BlockRuntimeID(block.Air{}) })

// airStates are Java's three airs, which a section's block count leaves out
// (Java counts a block when it is not air: air, cave air and void air alike).
var airStates = sync.OnceValue(func() map[int32]bool {
	m := map[int32]bool{}
	for _, n := range []string{"minecraft:air", "minecraft:cave_air", "minecraft:void_air"} {
		if s, ok := java261.DefaultState(n); ok {
			m[int32(s)] = true
		}
	}
	return m
})

func isAir(state int32) bool { return airStates()[state] }
