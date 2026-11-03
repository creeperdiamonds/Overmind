// Package rules holds the rules that belong to a player and differ by edition:
// how far it reaches and, later, how its attacks charge and hurt. A player plays
// by its own edition's rules; what the world shares follows Bedrock, which is
// Dragonfly's own behaviour.
package rules

import (
	"math"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/go-gl/mathgl/mgl64"
)

// Player is one edition's rules for its players. Bedrock is the default: it
// adds nothing to what Dragonfly already does.
type Player interface {
	// BlockRange is how far from its eyes the player reaches blocks: how far a
	// look finds one, for items the server aims itself (buckets on Java).
	BlockRange(creative bool) float64
	// CanReachBlock reports whether a player with its eyes at eye may use the
	// block at pos.
	CanReachBlock(eye mgl64.Vec3, pos cube.Pos, creative bool) bool
	Combat
}

// Default is the rules a player gets unless its edition has its own.
var Default Player = Bedrock{}

// Bedrock is Bedrock's rules, which are Dragonfly's: nothing is checked here
// that the player does not already check itself (8 blocks, 14 in creative).
type Bedrock struct{}

// BlockRange is Dragonfly's: 8 blocks, 14 in creative.
func (Bedrock) BlockRange(creative bool) float64 {
	if creative {
		return 14
	}
	return 8
}

// CanReachBlock leaves the reach to Dragonfly.
func (Bedrock) CanReachBlock(mgl64.Vec3, cube.Pos, bool) bool { return true }

// Java is Java's rules.
type Java struct{}

// Java's block interaction range (the player.block_interaction_range attribute):
// 4.5 blocks, 5 in creative. Source: minecraft.wiki, "Attribute". The client
// measures to the block's outline; the allowance on top is ours, for latency
// and for a point on the outline the server does not know.
const (
	javaBlockReach         = 4.5
	javaBlockReachCreative = 5.0
	reachAllowance         = 1.0
)

// BlockRange is Java's block interaction range.
func (Java) BlockRange(creative bool) float64 {
	if creative {
		return javaBlockReachCreative
	}
	return javaBlockReach
}

// CanReachBlock measures from the eyes to the nearest point of the block's cube.
func (j Java) CanReachBlock(eye mgl64.Vec3, pos cube.Pos, creative bool) bool {
	return distanceToBox(eye, cube.Box(0, 0, 0, 1, 1, 1).Translate(pos.Vec3())) <= j.BlockRange(creative)+reachAllowance
}

// distanceToBox is the distance from p to the nearest point of b.
func distanceToBox(p mgl64.Vec3, b cube.BBox) float64 {
	lo, hi := b.Min(), b.Max()
	var d2 float64
	for i := 0; i < 3; i++ {
		v := math.Max(lo[i]-p[i], math.Max(0, p[i]-hi[i]))
		d2 += v * v
	}
	return math.Sqrt(d2)
}
