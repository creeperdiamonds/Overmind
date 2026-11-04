package world

import "github.com/df-mc/dragonfly/server/block/cube"

// Overmind fork: an eye of ender leads to the nearest stronghold, which only the world's generator knows.

// StrongholdLocator is a Generator that knows where its strongholds are.
type StrongholdLocator interface {
	// NearestStronghold returns the position an eye of ender thrown at pos leads to, the stronghold nearest
	// it, or false if the world has none.
	NearestStronghold(pos cube.Pos) (cube.Pos, bool)
}

// NearestStronghold returns where an eye of ender thrown at pos leads, from the world's generator, or false
// if its generator has no strongholds.
func (w *World) NearestStronghold(pos cube.Pos) (cube.Pos, bool) {
	g := w.conf.Generator
	if lg, ok := g.(*lockedGenerator); ok {
		g = lg.g // Its lock is for chunks; a stronghold's place is read only.
	}
	if l, ok := g.(StrongholdLocator); ok {
		return l.NearestStronghold(pos)
	}
	return cube.Pos{}, false
}
