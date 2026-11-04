package world

import "github.com/df-mc/dragonfly/server/block/cube"

// Overmind fork: the weather check of a chunk tick. Each ticked chunk has a 1 in 16 chance a tick of
// choosing one column for it, in both editions (minecraft.wiki "Tick", oldid 3868751): water at the top
// may freeze, and snow may fall on it. What the check does to blocks is the block package's
// (RegisterPrecipitation), which this package cannot import.
//
// The 1 in 16 is not scaled by the random tick speed. For Overjava at Java's default speed of 3 it is
// exact on average: Java (since 1.20.2) rolls a 1 in 48 chance once for each unit of the speed, so 3/48 =
// 1/16 checks a chunk a tick (it may, rarely, check two or three columns in one tick, where this checks
// one). It differs only when Config.RandomTickSpeed is set by hand. Bedrock's scaling is not known.

// precipitationChance is the 1 in n chance a tick that a ticked chunk checks a column (a variable, so
// tests can raise it).
var precipitationChance = 16

// precipitation is what the weather check does at the top of a column: top is the highest block that
// stops precipitation, or the top of the water there, and above is the cell over it.
var precipitation func(tx *Tx, top, above cube.Pos)

// RegisterPrecipitation sets what the weather check of a chunk tick does at the top of a column. The block
// package registers it in its init.
func RegisterPrecipitation(f func(tx *Tx, top, above cube.Pos)) { precipitation = f }

// tickPrecipitation runs the weather check on the ticked chunks of a dimension with weather.
func (w weather) tickPrecipitation(tx *Tx, inReach func(ChunkPos) bool) {
	if precipitation == nil || !w.w.Dimension().WeatherCycle() {
		return
	}
	var columns []cube.Pos
	for pos := range w.w.chunks {
		if !inReach(pos) || w.w.r.IntN(precipitationChance) != 0 {
			continue
		}
		v := w.w.r.Int32()
		columns = append(columns, cube.Pos{int(pos[0]<<4) + int(v&0xf), 0, int(pos[1]<<4) + int((v>>8)&0xf)})
	}
	for _, c := range columns {
		top := tx.precipitationTop(c[0], c[2])
		if top < tx.Range()[0] || top >= tx.Range()[1] {
			continue
		}
		precipitation(tx, cube.Pos{c[0], top, c[2]}, cube.Pos{c[0], top + 1, c[2]})
	}
}

// precipitationTop is the Y of the highest block in a column that stops precipitation (a full top or
// bottom face, as highestObstructingBlock) or holds a liquid.
func (tx *Tx) precipitationTop(x, z int) int {
	src := worldSource{tx: tx}
	for y := tx.highestBlock(x, z); y >= tx.Range()[0]; y-- {
		pos := cube.Pos{x, y, z}
		b := tx.block(pos)
		if _, liquid := b.(Liquid); liquid {
			return y
		}
		if m := b.Model(); m.FaceSolid(pos, cube.FaceUp, src) || m.FaceSolid(pos, cube.FaceDown, src) {
			return y
		}
	}
	return tx.Range()[0]
}
