package worldgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		"cycle":     brokenRule("t:cycle", "t:loop", good),
		"cycle2":    brokenRule("t:cycle2", "t:q", good),
		"wide":      brokenRule("t:wide", "t:widepatch", good),
		"tall":      brokenRule("t:tall", "t:tallnest", good),
		"search":    brokenRule("t:search", "t:widesearch", good),
	}
	features := map[string]string{
		"ore":     oreFile("t:ore", "8", "minecraft:coal_ore"),
		"hugeore": oreFile("t:hugeore", "100000000", "minecraft:coal_ore"),
		"badore":  oreFile("t:badore", "8", "minecraft:no_such_block_at_all"),
		// A feature that names itself, and a cycle through a feature that fails (P = [Q, unknown], Q = [P]).
		"loop": composite("aggregate", "t:loop", `["t:loop"]`),
		"p":    composite("sequence", "t:p", `["t:q", "t:no_such_feature"]`),
		"q":    composite("aggregate", "t:q", `["t:p"]`),
		// A size far past any vanilla value.
		"widepatch": `{"format_version":"1.13.0","minecraft:vegetation_patch_feature":{"description":{"identifier":"t:widepatch"},
			"replaceable_blocks":["minecraft:stone"],"ground_block":"minecraft:dirt","vegetation_feature":"t:ore","surface":"floor",
			"depth":2,"vertical_range":5,"vegetation_chance":0.5,"horizontal_radius":{"range_min":100000,"range_max":100001}}}`,
		"tallnest": scatterFile("t:tallnest", "t:ore", "100000000"),
		// A search volume of 1025 x 1025 x 1025 (each number within its own limit).
		"widesearch": searchFile("t:widesearch", "t:ore", "[-512,-512,-512]", "[512,512,512]"),
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
	for _, id := range []string{"t:order", "t:upper", "t:twice", "t:huge", "t:negative", "t:count", "t:block", "t:exprorder",
		"t:cycle", "t:cycle2", "t:wide", "t:tall", "t:search"} {
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

func searchFile(id, feature, lo, hi string) string {
	return `{"format_version":"1.13.0","minecraft:search_feature":{"description":{"identifier":"` + id + `"},"places_feature":"` + feature +
		`","search_volume":{"min":` + lo + `,"max":` + hi + `},"search_axis":"+y","required_successes":1}}`
}

// neverFile is a feature that never places: a block it may only put over bedrock, in the open air.
func neverFile(id string) string {
	return `{"format_version":"1.13.0","minecraft:single_block_feature":{"description":{"identifier":"` + id +
		`"},"places_block":"minecraft:stone","enforce_placement_rules":false,"enforce_survivability_rules":false,"may_replace":["minecraft:bedrock"]}}`
}

func composite(kind, id, list string) string {
	return `{"format_version":"1.13.0","minecraft:` + kind + `_feature":{"description":{"identifier":"` + id + `"},"features":` + list + `}}`
}

func scatterFile(id, feature, iterations string) string {
	return `{"format_version":"1.13.0","minecraft:scatter_feature":{"description":{"identifier":"` + id + `"},"places_feature":"` + feature +
		`","iterations":` + iterations + `,"x":{"extent":[0,16]},"y":{"extent":[0,64]},"z":{"extent":[0,16]}}}`
}

// TestNestedScattersFinish decorates chunks with data whose scatters nest three deep at 400 iterations
// each: 64 million placements a rule, within every number's limit. The placement budget must make the
// chunks finish, without a panic.
func TestNestedScattersFinish(t *testing.T) {
	rules := map[string]string{"nest": brokenRule("t:nest", "t:n1", `{"iterations":400,"x":{"extent":[0,16]},"y":{"extent":[0,64]},"z":{"extent":[0,16]}}`),
		// The widest search the limits allow (129 x 1025 x 129 tries a placement), whose feature never
		// places, 400 times: its tries are charged to the same budget.
		"search": brokenRule("t:search", "t:s1", `{"iterations":400,"x":{"extent":[0,16]},"y":{"extent":[0,64]},"z":{"extent":[0,16]}}`)}
	features := map[string]string{
		"s1":    searchFile("t:s1", "t:never", "[-64,-512,-64]", "[64,512,64]"),
		"never": neverFile("t:never"),
		"ore":   oreFile("t:ore", "4", "minecraft:coal_ore"),
		"n1":    scatterFile("t:n1", "t:n2", "400"),
		"n2":    scatterFile("t:n2", "t:n3", "400"),
		"n3":    scatterFile("t:n3", "t:ore", "400"),
	}
	bd := brokenData(t, rules, features)
	if err := bd.compile(); err != nil {
		t.Fatal(err)
	}
	if len(bd.passes[2]) != 2 {
		t.Fatalf("the nested and search rules were not both kept: %q", bd.Problems)
	}
	g := NewWithData(5, ProfileBedrock, bd)
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("decorating panicked: %v", r)
			}
		}()
		generate(g, 0, 0)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Minute):
		t.Fatal("decorating did not finish in 5 minutes")
	}
	if m := mostPlacements.Load(); m > placementBudget {
		t.Errorf("a source made %d placements, over the budget of %d", m, placementBudget)
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
