package worldgen

import (
	"os"
	"sync"
	"testing"
)

// Tests that need Bedrock's feature data read it from OVERMIND_BEDROCK_DATA: a BDS folder or its
// behavior_packs folder (the study's bds60/behavior_packs). They skip without it. When it is set,
// every generator the tests make with the default data uses it.

var testData = sync.OnceValues(func() (*BedrockData, error) {
	dir := os.Getenv("OVERMIND_BEDROCK_DATA")
	if dir == "" {
		return nil, nil
	}
	return LoadBedrockData(dir)
})

func TestMain(m *testing.M) {
	if bd, err := testData(); err == nil && bd != nil {
		SetDefaultBedrockData(bd)
	}
	os.Exit(m.Run())
}

// testBedrockData gives the data, skipping the test without it.
func testBedrockData(t testing.TB) *BedrockData {
	t.Helper()
	bd, err := testData()
	if err != nil {
		t.Fatalf("OVERMIND_BEDROCK_DATA: %v", err)
	}
	if bd == nil {
		t.Skip("OVERMIND_BEDROCK_DATA is not set: point it at a Bedrock install's behavior_packs folder to run this")
	}
	return bd
}

// TestBedrockDataReads checks what the reader makes of the install against the study's reading of
// the same data: the underground pass in the order of its model (section 3), the carvers and disks.
func TestBedrockDataReads(t *testing.T) {
	bd := testBedrockData(t)
	t.Log(bd.Summary(), bd.Skipped)
	// The study's model's underground order, from its tools/rule_order.json, rules that place nothing
	// left out (0 iterations or no filter).
	want := []string{"redstone_ore_lower", "lapis_ore", "iron_ore_small", "iron_ore_middle", "gravel_ore", "granite_upper",
		"redstone_ore", "emerald_ore", "dirt", "tuff", "granite_lower", "diorite_upper", "diorite_lower", "diamond_ore_large",
		"copper_ore", "diamond_ore_buried", "lapis_ore_buried", "gold_ore", "coal_ore_upper", "andesite_upper",
		"mountains_underground_coal_ore", "andesite_lower", "coal_ore_lower", "mesa_underground_gold_ore",
		"dripstone_caves_underground_copper_ore", "diamond_ore", "gold_ore_lower", "iron_ore_upper",
		"lush_caves_underground_clay_ore", "diamond_ore_square"}
	var got []string
	for _, r := range bd.underground {
		if r.iterations() == 0 || r.biomes == ([2]uint64{}) {
			continue
		}
		got = append(got, r.ident)
	}
	if len(got) != len(want) {
		t.Fatalf("%d live underground rules, want %d: %v", len(got), len(want), got)
	}
	for i, w := range want {
		full := "minecraft:overworld_underground_" + w + "_feature"
		if w == "diamond_ore_square" {
			full = "minecraft:overworld_underground_diamond_ore_feature_square"
		} else if len(w) > 9 && (w[:9] == "mountains" || w[:4] == "mesa" || w[:9] == "dripstone" || w[:4] == "lush") {
			full = "minecraft:" + w + "_feature"
		}
		if got[i] != full {
			t.Errorf("underground rule %d: %s, want %s", i, got[i], full)
		}
	}
	if len(bd.carvers) != 2 || bd.carvers[0].skip != 66 || bd.carvers[1].skip != 33 {
		t.Errorf("cave carvers %+v, want the extra carver (skip 66) then the main one (33)", bd.carvers)
	}
	disks := 0
	for _, r := range bd.surface {
		switch r.ident {
		case "minecraft:overworld_surface_gravel_feature", "minecraft:minecraft:overworld_surface_sand_feature", "minecraft:overworld_surface_clay_feature":
			disks++
		}
	}
	if disks != 3 {
		t.Errorf("%d disk rules in the surface pass, want 3", disks)
	}
}

// TestNoDataFallsBack checks the Bedrock profile without data: built-in features, no Bedrock passes.
func TestNoDataFallsBack(t *testing.T) {
	g := NewWithData(100, ProfileBedrock, nil)
	if g.bedrockDeco() || g.bsurf != nil {
		t.Fatal("a generator with no data uses Bedrock's passes")
	}
	generate(g, 0, 0)
}
