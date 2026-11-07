package structures

import (
	"strings"
	"sync"

	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Block rules of legacy pool elements (StructurePoolBlockRule lists: the ancient city's degradation, the
// villages' decay), as LegacyStructureTemplate::place runs them (NOTES-structures-phase2 P9.1, cited P9
// below). Closed on the seed 100 city: 27,467 of 27,467 rot-candidate blocks.
//
//   - one random L = MT(the post-process seed of the chunk being written), fresh for each element placed
//     in each chunk;
//   - template blocks in the NBT list order; structure voids and jigsaw blocks are skipped with no draw;
//     a block outside the chunk is skipped with no draw (integrity is 1 for every legacy element);
//   - the rules run in list order, chained: a later rule tests the block an earlier one put;
//   - BlockMatchRandom draws nextFloat() only when the block type matches, and passes when it is < p;
//     TrueIfFound draws only when the block is in its set at its default state, and passes when <= p;
//     p >= 1 passes with no draw.

type predKind uint8

const (
	predAlways      predKind = iota
	predMatch                // The block is one of names (any state).
	predMatchRandom          // The block's type is names[0], and nextFloat() < p.
	predFound                // The block is one of names at its default state, and nextFloat() <= p.
	predExcept               // The block is none of names (default state), and (p < 1) nextFloat() <= p.
)

type rulePred struct {
	kind  predKind
	names []string // Bedrock names without "minecraft:".
	p     float32
}

type blockRule struct {
	src, tgt rulePred
	pos      *axisPos // nil: AlwaysTrue.
	result   string   // Bedrock name of the result, "" to keep the block (only its state reset).
}

// axisPos is an AxisAlignedPosition predicate (the bastion's highrampart list, NOTES-leftovers L4):
// passes when nextFloat() <= the chance lerped from minP to maxP as the distance along the axis from
// the piece's position goes from minD to maxD. Its draw is Java's AxisAlignedLinearPosTest (one
// nextFloat() each test, after the block and target tests); Bedrock's was not read.
type axisPos struct {
	minP, maxP float32
	minD, maxD int
	axis       int // 0 x, 1 y, 2 z.
}

func (a *axisPos) test(d int, r *worldgen.BedrockRandom) bool {
	t := float32(0)
	if a.maxD > a.minD {
		t = min(max(float32(abs(d)-a.minD)/float32(a.maxD-a.minD), 0), 1)
	}
	return r.NextFloat() <= a.minP+(a.maxP-a.minP)*t
}

// test runs a predicate on a block (name and runtime ID), drawing from r as P9's table says.
func (p *rulePred) test(name string, rid uint32, r *worldgen.BedrockRandom) bool {
	switch p.kind {
	case predAlways:
		return true
	case predMatch:
		return inNames(p.names, name)
	case predMatchRandom:
		if name != p.names[0] {
			return false
		}
		return chanceLess(r, p.p)
	case predFound:
		if !inNames(p.names, name) || !defaultState(rid) {
			return false
		}
		return chanceAtMost(r, p.p)
	case predExcept:
		if inNames(p.names, name) && defaultState(rid) {
			return false
		}
		return chanceAtMost(r, p.p)
	}
	return false
}

func inNames(ns []string, n string) bool {
	for _, x := range ns {
		if x == n {
			return true
		}
	}
	return false
}

// chanceLess is Random +0x70 (N2 1.1): p <= 0 false, p >= 1 true with no draw, else nextFloat() < p.
func chanceLess(r *worldgen.BedrockRandom, p float32) bool {
	if p <= 0 {
		return false
	}
	if p >= 1 {
		return true
	}
	return r.NextFloat() < p
}

// chanceAtMost is Random +0x78 (P9): p >= 1 true with no draw, p below -1.19e-7 false, else
// nextFloat() <= p.
func chanceAtMost(r *worldgen.BedrockRandom, p float32) bool {
	if p >= 1 {
		return true
	}
	if p < -1.19e-7 {
		return false
	}
	return r.NextFloat() <= p
}

// defaultProps are Bedrock's default values of the states the rule sets' blocks have (P9: TrueIfFound
// holds block pointers, which are each block's default state): pillar axis y, bottom slabs, stairs
// facing east (weirdo_direction 0) and not upside down.
var defaultProps = map[string]string{"pillar_axis": "y", "minecraft:vertical_half": "bottom", "weirdo_direction": "0",
	"upside_down_bit": "0"}

var defaultCache sync.Map

// defaultState says whether a runtime ID is its block's default state, as far as defaultProps knows.
func defaultState(rid uint32) bool {
	if v, ok := defaultCache.Load(rid); ok {
		return v.(bool)
	}
	d := true
	if b, ok := world.BlockByRuntimeID(rid); ok {
		_, props := b.EncodeBlock()
		for k, v := range props {
			if want, ok := defaultProps[k]; ok && propString(v) != want {
				d = false
			}
		}
	}
	defaultCache.Store(rid, d)
	return d
}

// ridOfName is a Bedrock block's runtime ID at its default state (the state defaultProps names, else
// Dragonfly's first registered state).
func ridOfName(name string) (uint32, bool) {
	registry.load()
	states := registry.byName[strings.TrimPrefix(name, "minecraft:")]
	for _, s := range states {
		if defaultState(s.rid) {
			return s.rid, true
		}
	}
	if len(states) > 0 {
		return states[0].rid, true
	}
	return 0, false
}

// applyRules runs a rule list on a template block about to be written at a world position.
// origin is the piece's position, which a position predicate measures from.
func (w *writer) applyRules(rules []blockRule, r *worldgen.BedrockRandom, name string, rid uint32, x, y, z int, origin [3]int) (string, uint32) {
	var worldName string
	var worldRID uint32
	worldRead := false
	for i := range rules {
		ru := &rules[i]
		if !ru.src.test(name, rid, r) {
			continue
		}
		if ru.tgt.kind != predAlways {
			if !worldRead {
				worldRID = w.chunk().Block(uint8(x&15), int16(y), uint8(z&15), 0)
				if b, ok := world.BlockByRuntimeID(worldRID); ok {
					n, _ := b.EncodeBlock()
					worldName = strings.TrimPrefix(n, "minecraft:")
				}
				worldRead = true
			}
			if !ru.tgt.test(worldName, worldRID, r) {
				continue
			}
		}
		if ru.pos != nil && !ru.pos.test([3]int{x, y, z}[ru.pos.axis]-origin[ru.pos.axis], r) {
			continue
		}
		res := ru.result
		if res == "" {
			res = name
		}
		if nr, ok := ridOfName(res); ok {
			name, rid = res, nr
		}
	}
	return name, rid
}
