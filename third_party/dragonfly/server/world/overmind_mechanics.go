package world

import "github.com/df-mc/dragonfly/server/block/cube"

// Overmind fork: the shared world rules that Bedrock and Java play differently. Overmind's world follows
// Bedrock by default and Java under its Overjava profile, so the server sets these per world.

// RandomTickSpeed is how many blocks of each sub chunk get a random tick each tick, or less than 0 for
// none (Config.RandomTickSpeed, after its default).
func (w *World) RandomTickSpeed() int { return w.conf.RandomTickSpeed }

// Mechanics are a world's shared rules that differ by edition. The zero value is Bedrock's.
type Mechanics struct {
	// JavaLeafDecay gives leaves Java's decay: a leaf stays while a log is within 6 steps through leaves,
	// and any change beside a leaf has it checked. Otherwise, Bedrock's: 4 steps, and only the removal of a
	// log or a leaf nearby has leaves checked.
	JavaLeafDecay bool
}

// Mechanics returns the world's rules that differ by edition.
func (w *World) Mechanics() Mechanics { return w.conf.Mechanics }

// Replacer is a block that reacts to being replaced by another block, or another state of itself, in a
// SetBlock that sends block updates. Replaced runs after the change.
type Replacer interface {
	Replaced(pos cube.Pos, with Block, tx *Tx)
}
