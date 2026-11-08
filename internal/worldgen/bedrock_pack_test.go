package worldgen

import (
	"os"
	"slices"
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
	// left out (0 iterations or no filter). It is the Overworld's: the Nether's biomes joined our table in
	// Task 10 (709823d), and since then the pass also holds the Nether's one underground rule, the soul
	// sand blobs (NOTES-bedrock-nether R2-4a), attached to soul sand valley only. That rule is checked on
	// its own below and left out of the Overworld's list.
	const soulSand = "minecraft:nether_soul_sand_underground_feature_rules"
	var netherOnly [2]uint64
	for i := range biomeTableNames {
		if isNetherBiome(uint8(i)) {
			netherOnly[i>>6] |= 1 << (i & 63)
		}
	}
	want := []string{"redstone_ore_lower", "lapis_ore", "iron_ore_small", "iron_ore_middle", "gravel_ore", "granite_upper",
		"redstone_ore", "emerald_ore", "dirt", "tuff", "granite_lower", "diorite_upper", "diorite_lower", "diamond_ore_large",
		"copper_ore", "diamond_ore_buried", "lapis_ore_buried", "gold_ore", "coal_ore_upper", "andesite_upper",
		"mountains_underground_coal_ore", "andesite_lower", "coal_ore_lower", "mesa_underground_gold_ore",
		"dripstone_caves_underground_copper_ore", "diamond_ore", "gold_ore_lower", "iron_ore_upper",
		"lush_caves_underground_clay_ore", "diamond_ore_square"}
	var got []string
	soulSandSeen := false
	for _, r := range bd.underground {
		if r.ident == soulSand {
			soulSandSeen = true
			var only [2]uint64
			only[bSoulSandValley>>6] = 1 << (bSoulSandValley & 63)
			if r.biomes != only || r.iterations() != 12 {
				t.Errorf("%s: biomes %x, %d iterations; want soul sand valley only, 12", soulSand, r.biomes, r.iterations())
			}
			continue
		}
		if r.iterations() == 0 || r.biomes == ([2]uint64{}) {
			continue
		}
		if r.biomes[0]&^netherOnly[0] == 0 && r.biomes[1]&^netherOnly[1] == 0 {
			t.Errorf("%s: an underground rule of the Nether's biomes only, not the soul sand rule", r.ident)
			continue
		}
		got = append(got, r.ident)
	}
	if !soulSandSeen {
		t.Errorf("%s is not in the underground pass", soulSand)
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

// TestBedrockValuesFromData: with the data read, biome temperatures and the counts of the Java
// placements that stand in for unwritten rules come from the data, not from our tables.
func TestBedrockValuesFromData(t *testing.T) {
	bd := testBedrockData(t)
	g := NewWithData(1, ProfileBedrock, bd)
	for _, name := range []string{"savanna", "meadow", "lush_caves", "snowy_plains", "frozen_peaks"} {
		i := biomeIdx[name]
		if !bd.climateSet[i] {
			t.Fatalf("%s: the pack gives no temperature", name)
		}
		if got := g.climateT.temperature(i, 0, 64, 0); got != bd.climate[i] {
			t.Errorf("%s temperature %v, want the pack's %v", name, got, bd.climate[i])
		}
	}
	if got := bd.counts["underwater_magma"]; got != [2]int{18, 30} {
		t.Errorf("underwater magma count %v, want the rule's Math.Random(18, 30)", got)
	}
	if got := bd.counts["glow_lichen"]; got != [2]int{104, 157} {
		t.Errorf("glow lichen count %v, want the rule's 104..157", got)
	}
	if NewWithData(1, ProfileBedrock, nil).climateT != ProfileBedrock.climate {
		t.Error("with no data the profile's table is not the fallback")
	}
}

// TestBedrockMangroveRates: the Bedrock profile is to run BDS's own mangrove swamp rules, 25 trees and 2
// with a bee nest and 2 tall ones with a bee nest a chunk (BEDROCK-NOTES row 9's "25 + 2 + 2"), and
// not Java's trees_mangrove (25), which stands in only while a rule is not written. The tree
// feature's mangrove parts (mangrove_roots, mangrove_trunk, mangrove_canopy; NOTES-bedrock-exact T17,
// T27) are written since Task 9 round 17 (bedrock_mangrove.go); the skip stays for a pack without them.
func TestBedrockMangroveRates(t *testing.T) {
	bd := testBedrockData(t)
	var kept []string
	for n := range bd.javaKeep {
		kept = append(kept, n)
	}
	slices.Sort(kept)
	t.Logf("Java placements kept for Bedrock rules not written: %v", kept)
	want := map[string]int{"minecraft:mangrove_swamp_mangrove_tree_feature": 25, "minecraft:mangrove_swamp_mangrove_tree_with_beenest_feature": 2,
		"minecraft:mangrove_swamp_tall_mangrove_tree_with_beenest_feature": 2}
	for _, r := range bd.surface {
		if n, ok := want[r.ident]; ok {
			if r.feat == nil || r.iterations() != n {
				t.Errorf("%s: feature %v, %d iterations, want %d placed", r.ident, r.feat != nil, r.iterations(), n)
			}
			delete(want, r.ident)
		}
	}
	if len(want) == 3 && bd.javaKeep["trees_mangrove"] {
		t.Skip("Bedrock's mangrove tree is not written: its rules are skipped and Java's trees_mangrove (25 a chunk) stands in")
	}
	for id := range want {
		t.Errorf("%s: not a live surface rule", id)
	}
	if bd.javaKeep["trees_mangrove"] {
		t.Error("Java's trees_mangrove is kept beside Bedrock's rules")
	}
	if !bd.javaReplaced(ProfileBedrock, &placement{name: "trees_mangrove", step: vegetationStep}) {
		t.Error("Java's trees_mangrove still runs in the Bedrock profile")
	}
}
