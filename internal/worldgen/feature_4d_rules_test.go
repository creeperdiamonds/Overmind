package worldgen

import (
	"math"
	"testing"
)

// Rules of Task 4d's untraced features, checked against Java 26.1's source and data files rather
// than against a Java run, since no traced chunk holds these biomes.

// TestIcebergDrawsLikeJava replays the iceberg's shape draws in IcebergFeature's order: an ellipse
// draws its height above water with nextInt(6), a round iceberg with nextInt(15) and maybe a tall
// extra, never both. Both the values and the generator's state after must agree.
func TestIcebergDrawsLikeJava(t *testing.T) {
	ellipses, round := 0, 0
	for seed := range int64(4000) {
		var ours, java featRand
		ours.setSeed(seed)
		java.setSeed(seed)
		got := icebergShape(&ours)

		var want icebergDims
		want.snowy = java.nextDouble() > 0.7
		want.angle = java.nextDouble() * 2 * math.Pi
		want.ea = 11 - java.nextInt(5)
		want.ec = 3 + java.nextInt(3)
		want.ellipse = java.nextDouble() > 0.7
		if want.ellipse {
			want.over = java.nextInt(6) + 6
			ellipses++
		} else {
			want.over = java.nextInt(15) + 3
			round++
		}
		if !want.ellipse && java.nextDouble() > 0.9 {
			want.over += java.nextInt(19) + 7
		}
		want.under = min(want.over+java.nextInt(11), 18)
		want.width = min(want.over+java.nextInt(7)-java.nextInt(5), 11)

		if got != want {
			t.Fatalf("seed %d: shape %+v, Java draws %+v", seed, got, want)
		}
		if ours.x != java.x {
			t.Fatalf("seed %d: the generator is not where Java's is after the shape draws", seed)
		}
	}
	if ellipses == 0 || round == 0 {
		t.Fatalf("seeds gave %d ellipses and %d round icebergs, want both kinds", ellipses, round)
	}
}

// TestMangrovesFollowJava holds both mangroves against Java 26.1's configured features mangrove and
// tall_mangrove (worldgen/configured_feature/*.json) and the block tags they name.
func TestMangrovesFollowJava(t *testing.T) {
	logsThrough := "mud muddy_mangrove_roots mangrove_roots mangrove_leaves mangrove_log mangrove_propagule moss_carpet vine" // #mangrove_logs_can_grow_through
	rootsThrough := "mud muddy_mangrove_roots mangrove_roots moss_carpet vine mangrove_propagule snow"                        // #mangrove_roots_can_grow_through
	for _, c := range []struct {
		tall          bool
		base, a, b    int
		steps, offset string
		limit         int
	}{
		{false, 2, 1, 4, "1..4/flat", "1..3/flat", 2},
		{true, 4, 1, 9, "1..6/flat", "3..7/flat", 3},
	} {
		cfg := mangroveTree(c.tall)
		tr, ok := cfg.trunk.(branchingTrunk)
		if !ok {
			t.Fatalf("tall %v: trunk is %T, want the upwards branching trunk", c.tall, cfg.trunk)
		}
		fo, ok := cfg.foliage.(spreadFoliage)
		if !ok {
			t.Fatalf("tall %v: foliage is %T, want random spread foliage", c.tall, cfg.foliage)
		}
		ro := cfg.roots
		got := []any{tr.base, tr.a, tr.b, describeInts(tr.steps), describeInts(tr.length), tr.chance,
			describeInts(fo.radius), describeInts(fo.offs), describeInts(fo.fh), fo.attempts,
			cfg.layersLimit, cfg.lower, cfg.upper, cfg.ignoreVines,
			describeInts(ro.offset), ro.aboveChance, ro.width, ro.length, ro.skew, ro.root.name(), ro.above.name(), ro.muddy.name()}
		want := []any{c.base, c.a, c.b, c.steps, "0..1/flat", float32(0.5),
			"3", "0", "2", 70,
			c.limit, 0, 2, true,
			c.offset, float32(0.5), 8, 15, float32(0.2), "mangrove_roots[waterlogged=false]", "moss_carpet", "muddy_mangrove_roots[axis=y]"}
		names := []string{"base_height", "height_rand_a", "height_rand_b", "extra_branch_steps", "extra_branch_length", "place_branch_per_log_probability",
			"radius", "offset", "foliage_height", "leaf_placement_attempts",
			"limit", "lower_size", "upper_size", "ignore_vines",
			"trunk_offset_y", "above_root_placement_chance", "max_root_width", "max_root_length", "random_skew_chance", "root_provider", "above_root_provider", "muddy_roots_provider"}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("tall %v: %s is %v, Java's is %v", c.tall, names[i], got[i], want[i])
			}
		}
		for _, set := range []struct {
			what string
			got  map[uint16]bool
			want string
		}{{"logs grow through", cfg.validExtra, logsThrough}, {"roots grow through", ro.through, rootsThrough}, {"muddy roots in", ro.muddyIn, "mud muddy_mangrove_roots"}} {
			if want := blockSet(set.want); len(set.got) != len(want) {
				t.Errorf("tall %v: %s holds %d blocks, Java's %d", c.tall, set.what, len(set.got), len(want))
			} else {
				for b := range want {
					if !set.got[b] {
						t.Errorf("tall %v: %s lacks %s", c.tall, set.what, blockName[b])
					}
				}
			}
		}
	}
}
