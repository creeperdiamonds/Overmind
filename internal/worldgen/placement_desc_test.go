package worldgen

import (
	"fmt"
	"math"
	"strings"
)

// describePlacement writes a placed feature's modifiers the way the game's placed feature files list
// them, one word per modifier, so a test can hold them against Java's values. Int and height
// providers are closures here, so they are sampled: a constant prints as its value, anything else as
// its least and greatest value and "flat" (every value about as likely) or "shaped". Block predicates
// print as "filter" alone.
func describePlacement(pl *placement) string {
	var out []string
	for _, m := range pl.mods {
		out = append(out, describeModifier(m))
	}
	return strings.Join(out, " ")
}

var heightmapNames = [...]string{hmWorldSurfaceWG: "WORLD_SURFACE_WG", hmOceanFloorWG: "OCEAN_FLOOR_WG", hmWorldSurface: "WORLD_SURFACE",
	hmOceanFloor: "OCEAN_FLOOR", hmMotionBlocking: "MOTION_BLOCKING", hmMotionNoLeaves: "MOTION_BLOCKING_NO_LEAVES"}

func describeModifier(m modifier) string {
	switch m := m.(type) {
	case countMod:
		return "count:" + describeInts(m.n)
	case dataCountMod: // Its count with no Bedrock data; with the data it is the rule's.
		return "count:" + describeInts(uniInt(m.fallback[0], m.fallback[1]))
	case inSquareMod:
		return "in_square"
	case heightRangeMod:
		return "height_range:" + describeInts(intProvider(m.h))
	case heightmapMod:
		return "heightmap:" + heightmapNames[m.kind]
	case biomeMod:
		return "biome"
	case rarityMod:
		return fmt.Sprintf("rarity_filter:%d", m.chance)
	case noiseCountMod:
		return fmt.Sprintf("noise_based_count:%g:%g:%g", m.ratio, m.factor, m.offset)
	case noiseThresholdMod:
		return fmt.Sprintf("noise_threshold_count:%g:%d:%d", m.level, m.below, m.above)
	case waterDepthMod:
		return fmt.Sprintf("surface_water_depth_filter:%d", m.limit)
	case surfaceThresholdMod:
		bound := func(v int) string {
			if v <= -1<<30 || v >= 1<<30 {
				return "_"
			}
			return fmt.Sprint(v)
		}
		return "surface_relative_threshold_filter:" + heightmapNames[m.kind] + ":" + bound(m.lo) + ":" + bound(m.hi)
	case filterMod:
		return "filter"
	case scanMod:
		d := "down"
		if m.dir == dUp {
			d = "up"
		}
		return fmt.Sprintf("environment_scan:%s:%d", d, m.steps)
	case offsetMod:
		return "random_offset:" + describeInts(m.xz) + ":" + describeInts(m.y)
	}
	return fmt.Sprintf("unknown:%T", m)
}

// describeInts samples a provider: "5" for a constant, "lo..hi/flat" or "lo..hi/shaped" otherwise.
func describeInts(p intProvider) string {
	const n = 1 << 17
	var r featRand
	r.setSeed(12345)
	counts := map[int]int{}
	lo, hi := 1<<30, -1<<30
	for range n {
		v := p(&r)
		counts[v]++
		lo, hi = min(lo, v), max(hi, v)
	}
	if lo == hi {
		return fmt.Sprint(lo)
	}
	kind := "flat"
	mean := float64(n) / float64(hi-lo+1)
	slack := 6 * math.Sqrt(mean) // Six standard deviations of sampling noise.
	for v := lo; v <= hi; v++ {
		if c := float64(counts[v]); math.Abs(c-mean) > slack {
			kind = "shaped"
			break
		}
	}
	return fmt.Sprintf("%d..%d/%s", lo, hi, kind)
}
