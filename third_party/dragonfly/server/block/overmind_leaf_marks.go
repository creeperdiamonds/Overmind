package block

import (
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
)

// Overmind fork: Bedrock checks leaves for decay only when a log or a leaf near them is removed. Removing a
// log marks every leaf in the 9 x 9 x 9 cube around it, and removing a leaf every leaf in the 3 x 3 x 3 cube
// around it; nothing else marks a leaf. Measured on a Bedrock Dedicated Server 1.26.60 with leaves and logs set
// by console commands (Task W report). Java's leaves are marked by any change beside them (NeighbourUpdateTick).

// Replaced marks the leaves around a removed log, on Bedrock.
func (Log) Replaced(pos cube.Pos, with world.Block, tx *world.Tx) {
	if !logLike(with) {
		markLeavesAround(pos, 4, tx)
	}
}

// Replaced marks the leaves around a removed wood block, on Bedrock.
func (Wood) Replaced(pos cube.Pos, with world.Block, tx *world.Tx) {
	if !logLike(with) {
		markLeavesAround(pos, 4, tx)
	}
}

// Replaced marks the leaves around a removed leaf, on Bedrock.
func (Leaves) Replaced(pos cube.Pos, with world.Block, tx *world.Tx) {
	if _, ok := with.(Leaves); !ok {
		markLeavesAround(pos, 1, tx)
	}
}

// logLike reports whether b keeps leaves from decaying, so that replacing a log with it (stripping it) is no
// removal.
func logLike(b world.Block) bool {
	switch b.(type) {
	case Log, Wood:
		return true
	}
	return false
}

// markLeavesAround marks for a decay check every leaf that may decay in the cube of radius r around pos, on
// Bedrock. The marks are set without block updates, as they change nothing a neighbour reads, and unseen,
// as they change nothing a client shows: a cut log would otherwise send up to 728 leaves to every viewer.
func markLeavesAround(pos cube.Pos, r int, tx *world.Tx) {
	if tx.World().Mechanics().JavaLeafDecay {
		return
	}
	for x := -r; x <= r; x++ {
		for y := -r; y <= r; y++ {
			for z := -r; z <= r; z++ {
				p := pos.Add(cube.Pos{x, y, z})
				if p.OutOfBounds(tx.Range()) {
					continue
				}
				if l, ok := tx.Block(p).(Leaves); ok && !l.Persistent && !l.ShouldUpdate {
					l.ShouldUpdate = true
					tx.SetBlock(p, l, &world.SetOpts{DisableBlockUpdates: true, Unseen: true})
				}
			}
		}
	}
}
