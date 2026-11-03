package worldgen

import (
	"strings"
	"testing"
)

// placedFamilies are the blocks of the features written in Task 4d. Every state of them the tables
// know must map to a Dragonfly block: a state with no block panics under test (unknownState), and an
// own block with a state Bedrock does not have panics when it is registered.
var placedFamilies = strings.Fields(`moss_block moss_carpet clay azalea flowering_azalea short_grass tall_grass
	cave_vines cave_vines_plant big_dripleaf big_dripleaf_stem small_dripleaf spore_blossom rooted_dirt hanging_roots
	azalea_leaves flowering_azalea_leaves oak_log vine dripstone_block pointed_dripstone
	sculk sculk_vein sculk_catalyst sculk_sensor sculk_shrieker
	wildflowers pink_petals leaf_litter short_dry_grass tall_dry_grass firefly_bush allium oxeye_daisy pink_tulip
	tube_coral tube_coral_block tube_coral_fan tube_coral_wall_fan horn_coral horn_coral_wall_fan packed_ice blue_ice snow_block`)

func TestEveryPlacedStateHasABlock(t *testing.T) {
	NewWithProfile(1, ProfileOverjava)
	NewWithProfile(1, ProfileBedrock)
	want := map[string]bool{}
	for _, n := range placedFamilies {
		want[n] = true
	}
	// A bare name of a block with properties is a lookup key for tags and matches, never placed.
	stateLock.Lock()
	withProps := map[string]bool{}
	for name := range stateIndex {
		if base, props, _ := strings.Cut(name, "["); props != "" {
			withProps[base] = true
		}
	}
	var list []bs
	for name, s := range stateIndex {
		if base, props, _ := strings.Cut(name, "["); want[base] && (props != "" || !withProps[base]) {
			list = append(list, s)
		}
	}
	stateLock.Unlock()
	seen := map[string]bool{}
	for _, s := range list {
		name := s.name()
		seen[stateBase(name)] = true
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("%s: %v", name, r)
				}
			}()
			runtimeOf(s)
		}()
	}
	for _, n := range placedFamilies {
		if !seen[n] {
			t.Errorf("%s: no state of it in the tables", n)
		}
	}
}
