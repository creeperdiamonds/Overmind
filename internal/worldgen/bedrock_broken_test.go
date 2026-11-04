package worldgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Broken or hostile pack data must never panic the generator: a rule whose data cannot be used is
// skipped and named in Problems, once. The data here is our own, made up for the test.

// brokenData builds feature data by hand: every biome of our table with no components and the tag
// "test", and the given rules and features.
func brokenData(t *testing.T, rules map[string]string, features map[string]string) *BedrockData {
	t.Helper()
	bd := &BedrockData{rules: map[string]*packRule{}, order: map[string][]string{}, features: map[string]packFeature{},
		biomes: map[string]*packBiome{}, Skipped: map[string]int{}, ores: map[string]*bedrockOre{}}
	for i := range biomeTableNames {
		bd.biomes[biomeRowDefs[i].String()] = &packBiome{tags: map[string]bool{"test": true}, components: map[string]json.RawMessage{}}
	}
	for id, text := range rules {
		r, err := parseRule(text)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		bd.rules[r.Description.Identifier] = r
		bd.order[r.Conditions.Pass] = append(bd.order[r.Conditions.Pass], r.Description.Identifier)
	}
	for _, text := range features {
		if err := bd.addFeature(text); err != nil {
			t.Fatal(err)
		}
	}
	return bd
}

func brokenRule(id, feature, dist string) string {
	return `{"format_version":"1.13.0","minecraft:feature_rules":{"description":{"identifier":"` + id + `","places_feature":"` + feature +
		`"},"conditions":{"placement_pass":"underground_pass","minecraft:biome_filter":[{"test":"has_biome_tag","value":"test"}]},"distribution":` + dist + `}}`
}

func oreFile(id, count, block string) string {
	return `{"format_version":"1.13.0","minecraft:ore_feature":{"description":{"identifier":"` + id + `"},"count":` + count +
		`,"replace_rules":[{"places_block":"` + block + `","may_replace":["minecraft:stone"]}]}}`
}

func TestBrokenPackDataIsSkipped(t *testing.T) {
	good := `{"iterations":2,"x":{"extent":[0,16]},"y":{"extent":[0,64]},"z":{"extent":[0,16]}}`
	rules := map[string]string{
		"good":      brokenRule("t:good", "t:ore", good),
		"order":     brokenRule("t:order", "t:ore", `{"iterations":2,"coordinate_eval_order":"abc","x":1,"y":1,"z":1}`),
		"upper":     brokenRule("t:upper", "t:ore", `{"iterations":2,"coordinate_eval_order":"XZY","x":1,"y":1,"z":1}`),
		"twice":     brokenRule("t:twice", "t:ore", `{"iterations":2,"coordinate_eval_order":"xxz","x":1,"y":1,"z":1}`),
		"huge":      brokenRule("t:huge", "t:ore", `{"iterations":100000000,"x":1,"y":1,"z":1}`),
		"negative":  brokenRule("t:negative", "t:ore", `{"iterations":-5,"x":1,"y":1,"z":1}`),
		"count":     brokenRule("t:count", "t:hugeore", good),
		"block":     brokenRule("t:block", "t:badore", good),
		"exprorder": brokenRule("t:exprorder", "t:ore", `{"iterations":"query.nonsense(1)","x":1,"y":1,"z":1}`),
	}
	features := map[string]string{
		"ore":     oreFile("t:ore", "8", "minecraft:coal_ore"),
		"hugeore": oreFile("t:hugeore", "100000000", "minecraft:coal_ore"),
		"badore":  oreFile("t:badore", "8", "minecraft:no_such_block_at_all"),
	}
	bd := brokenData(t, rules, features)
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("compiling broken data panicked: %v", r)
			}
		}()
		bd.compileRules()
	}()
	var got []string
	for _, r := range bd.passes[2] {
		got = append(got, r.ident)
	}
	if len(got) != 1 || got[0] != "t:good" {
		t.Errorf("rules kept: %v, want only t:good", got)
	}
	for _, id := range []string{"t:order", "t:upper", "t:twice", "t:huge", "t:negative", "t:count", "t:block", "t:exprorder"} {
		n := 0
		for _, p := range bd.Problems {
			if strings.Contains(p, id+":") {
				n++
			}
		}
		if n != 1 {
			t.Errorf("%s named %d times in Problems %q, want once", id, n, bd.Problems)
		}
	}
}

// TestBrokenInstallsDoNotPanic loads the reviewer's broken copies of an install (OVERMIND_BROKEN_DATA,
// a folder of installs; skipped without it): each loads or fails with an error, and never panics.
func TestBrokenInstallsDoNotPanic(t *testing.T) {
	dir := os.Getenv("OVERMIND_BROKEN_DATA")
	if dir == "" {
		t.Skip("OVERMIND_BROKEN_DATA is not set")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("panicked: %v", r)
				}
			}()
			bd, err := LoadBedrockData(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Logf("refused: %v", err)
				return
			}
			t.Logf("%s; problems %q; warnings %q", bd.Summary(), bd.Problems, bd.Warnings)
		})
	}
}

// TestMissingDefinitionsWarns: a game's behavior_packs folder has no definitions folder, so the rules and
// features only the server's definitions hold are missing; the data says so (OVERMIND_BROKEN_DATA's
// nodefs copy), and a full install has no warning.
func TestMissingDefinitionsWarns(t *testing.T) {
	if bd := testBedrockData(t); len(bd.Warnings) != 0 {
		t.Errorf("a full install warns: %q", bd.Warnings)
	}
	dir := os.Getenv("OVERMIND_BROKEN_DATA")
	if dir == "" {
		t.Skip("OVERMIND_BROKEN_DATA is not set")
	}
	bd, err := LoadBedrockData(filepath.Join(dir, "nodefs"))
	if err != nil {
		t.Fatal(err)
	}
	if len(bd.Warnings) == 0 || !strings.Contains(bd.Warnings[0], "definitions") {
		t.Errorf("no warning about the missing definitions: %q", bd.Warnings)
	}
	t.Log(bd.Warnings)
}

// TestPaleMossCarpetFlags: a pale moss carpet's top (bottom=false) is a thin, open block with no flags;
// only the bottom carpet holds up what is on it from below (review Minor 4).
func TestPaleMossCarpetFlags(t *testing.T) {
	top := stateOf("pale_moss_carpet[bottom=false,east=none,north=none,south=none,west=none]")
	if f := stateFlag[top]; f != 0 {
		t.Errorf("top carpet flags %b, want 0", f)
	}
	bottom := stateOf("pale_moss_carpet[bottom=true,east=none,north=none,south=none,west=none]")
	if f := stateFlag[bottom]; f != fSturdyDown {
		t.Errorf("bottom carpet flags %b, want sturdy-down only", f)
	}
}
