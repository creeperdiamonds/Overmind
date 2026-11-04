package world

import "github.com/df-mc/dragonfly/server/block/cube"

// Overmind fork: the shared world rules that Bedrock and Java play differently. Overmind's world follows
// Bedrock by default and Java under its Overjava profile, so the server sets these per world.

// RandomTickSpeed is how many blocks of each sub chunk get a random tick each tick, or less than 0 for
// none (Config.RandomTickSpeed, after its default).
func (w *World) RandomTickSpeed() int { return w.conf.RandomTickSpeed }

// Age is the world's age: the ticks it has run since it was made (Settings.CurrentTick), which only go
// up, whatever its time of day is set to. Java's update_time carries it. It is safe to call without a
// transaction, except while the world's settings are held.
func (w *World) Age() int64 {
	w.set.Lock()
	defer w.set.Unlock()
	return w.set.CurrentTick
}

// Mechanics are a world's shared rules that differ by edition. The zero value is Bedrock's.
type Mechanics struct {
	// JavaLeafDecay gives leaves Java's decay: a leaf stays while a log is within 6 steps through leaves,
	// and any change beside a leaf has it checked. Otherwise, Bedrock's: 4 steps, and only the removal of a
	// log or a leaf nearby has leaves checked.
	JavaLeafDecay bool
	// JavaWeather gives the weather check of a chunk tick Java's rules: snowfall lays one layer at most, and
	// a waterlogged block beside water counts as water, so it does not let the water freeze. Otherwise,
	// Bedrock's: snow builds up to a height set by the biome, and a waterlogged block lets water freeze.
	JavaWeather bool
}

// Mechanics returns the world's rules that differ by edition.
func (w *World) Mechanics() Mechanics { return w.conf.Mechanics }

// Replacer is a block that reacts to being replaced by another block, or another state of itself, in a
// SetBlock that sends block updates. Replaced runs after the change.
type Replacer interface {
	Replaced(pos cube.Pos, with Block, tx *Tx)
}
