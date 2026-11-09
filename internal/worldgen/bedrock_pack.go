package worldgen

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Bedrock's feature data (features, feature rules, biomes) is read at startup from a local Bedrock
// install the operator points at, by the owner's decision of 2026-11-04, as structures and loot are.
// Nothing of Mojang's is in this repo. Without an install the Bedrock profile generates its features
// with the built-in, Java-based placement, which is approximate.
//
// How the data is put together follows the study of BDS 1.26.60.6 (NOTES-bedrock-exact section 3):
// the vanilla behaviour packs are read newest version first, each pack's feature_rules archive in
// archive order, and a rule's identifier keeps the copy of the newest pack. The order of a pass's
// rules is the iteration order of a libc++ unordered_map keyed by the FNV-1 64 hash of the
// identifier, filled in that order (libcxxOrder). The server's own definitions folder adds the rules
// and features no pack has; their rules go at the end of their pass.

// BedrockData is the feature data of one install, read once and shared by every generator.
type BedrockData struct {
	Dir string // The folder the data was read from.

	rules    map[string]*packRule
	order    map[string][]string // Pass name to rule identifiers, in the pass's order.
	features map[string]packFeature
	biomes   map[string]*packBiome

	// Compiled for the generator.
	fc                   *featCompiler
	javaKeep             map[string]bool // Java placements kept for Bedrock rules not written yet.
	magmaLive            bool            // The underwater magma rule runs Bedrock's features (L20): Java's placement stops.
	passes               [len(bedrockPasses)][]*bedrockRule
	underground, surface []*bedrockRule
	carvers              []caveCarver
	ores                 map[string]*bedrockOre // By rule identifier, for tests.
	lastOre              *bedrockOre
	surfaceBiomes        [len(biomeTableNames)]surfaceBiome
	subsurface           [len(biomeTableNames)]*gradientDef // A biome's noise_gradient subsurface builder.
	// Skipped counts the rules of the passes used that are not written yet, by reason.
	Skipped map[string]int
	// Problems names the rules whose data is broken (unreadable values, unknown blocks, absurd
	// counts), each once; they are skipped.
	Problems    []string
	problemSeen map[string]bool
	// climate is each of our biomes' temperature from its pack file, where the file gives one.
	climate    [len(biomeTableNames)]float32
	climateSet [len(biomeTableNames)]bool
	// counts are the iteration ranges of rules whose features are not written yet, for the Java
	// placements that stand in for them (dataCountMod).
	counts map[string][2]int
	sum    hash.Hash // Of every file read, in the order read.
	// Warnings says what the install lacks for an exact world (a game's packs with no definitions).
	Warnings []string
	// fossils are the fossil feature's templates (bedrock_fossil.go), read once at load.
	fossilOnce    sync.Once
	fossils       [8]*fossilTemplate
	netherFossils [14]*fossilTemplate
	// crust is the coral crust's templates (bedrock_coralcrust.go), read on first use.
	crust crustData
}

type packRule struct {
	Description struct {
		Identifier    string `json:"identifier"`
		PlacesFeature string `json:"places_feature"`
	} `json:"description"`
	Conditions struct {
		Pass   string          `json:"placement_pass"`
		Filter json.RawMessage `json:"minecraft:biome_filter"`
	} `json:"conditions"`
	Distribution map[string]json.RawMessage `json:"distribution"`
}

// packFeature is a feature file's one top-level entry: its kind and body.
type packFeature struct {
	kind string
	body json.RawMessage
}

type packBiome struct {
	tags       map[string]bool
	components map[string]json.RawMessage
}

var vanillaPack = regexp.MustCompile(`^vanilla(_[0-9.]+)?$`)

// packVersion orders "vanilla" before every "vanilla_x.y.z".
func packVersion(name string) []int {
	if name == "vanilla" {
		return nil
	}
	var v []int
	for _, p := range strings.Split(strings.TrimPrefix(name, "vanilla_"), ".") {
		n, _ := strconv.Atoi(p)
		v = append(v, n)
	}
	return v
}

// readBrarchive reads a pack archive: a 16-byte header with the entry count at 8, then 256-byte
// entries (name length, name, data offset at 248, size at 252), then the data.
func readBrarchive(path string) ([][2]string, error) {
	d, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(d) < 16 {
		return nil, fmt.Errorf("%s: too short for an archive", path)
	}
	n := int(binary.LittleEndian.Uint32(d[8:]))
	base := 16 + n*256
	if base > len(d) {
		return nil, fmt.Errorf("%s: bad entry count %d", path, n)
	}
	out := make([][2]string, 0, n)
	for i := range n {
		e := d[16+i*256 : 16+(i+1)*256]
		name := string(e[1 : 1+int(e[0])])
		off, size := int(binary.LittleEndian.Uint32(e[248:])), int(binary.LittleEndian.Uint32(e[252:]))
		if base+off+size > len(d) {
			return nil, fmt.Errorf("%s: entry %s out of range", path, name)
		}
		out = append(out, [2]string{name, string(d[base+off : base+off+size])})
	}
	return out, nil
}

// packsDir finds the behaviour packs folder and the definitions folder under dir: a BDS folder, its
// behavior_packs folder, or a game's behavior_packs folder.
func packsDir(dir string) (packs, defs string, err error) {
	for _, p := range []string{filepath.Join(dir, "behavior_packs"), dir} {
		if st, e := os.Stat(filepath.Join(p, "vanilla")); e == nil && st.IsDir() {
			packs = p
			break
		}
	}
	if packs == "" {
		return "", "", fmt.Errorf("%s holds no vanilla behaviour pack (looked in it and its behavior_packs)", dir)
	}
	if st, e := os.Stat(filepath.Join(filepath.Dir(packs), "definitions")); e == nil && st.IsDir() {
		defs = filepath.Join(filepath.Dir(packs), "definitions")
	}
	return packs, defs, nil
}

// LoadBedrockData reads the vanilla feature data from a Bedrock install.
func LoadBedrockData(dir string) (*BedrockData, error) {
	packs, defs, err := packsDir(dir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(packs)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && vanillaPack.MatchString(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.SortFunc(names, func(a, b string) int { return slices.Compare(packVersion(b), packVersion(a)) }) // newest first
	bd := &BedrockData{Dir: dir, sum: sha256.New(), rules: map[string]*packRule{}, order: map[string][]string{}, features: map[string]packFeature{},
		biomes: map[string]*packBiome{}, Skipped: map[string]int{}, ores: map[string]*bedrockOre{}}
	maps := map[string]*libcxxOrder{}
	for _, pack := range names {
		br := filepath.Join(packs, pack, "__brarchive")
		if list, err := readArchiveIfAny(filepath.Join(br, "feature_rules.brarchive")); err != nil {
			return nil, err
		} else {
			for _, e := range list {
				bd.sum.Write([]byte(pack + "/" + e[0] + "\x00" + e[1] + "\x00"))
				r, err := parseRule(e[1])
				if err != nil {
					return nil, fmt.Errorf("%s %s: %w", pack, e[0], err)
				}
				id := r.Description.Identifier
				if _, ok := bd.rules[id]; ok {
					continue // A newer pack's copy is kept, and so is its place in the map.
				}
				bd.rules[id] = r
				m := maps[r.Conditions.Pass]
				if m == nil {
					m = &libcxxOrder{}
					maps[r.Conditions.Pass] = m
				}
				m.insert(id, fnv1Hash(id))
			}
		}
		if list, err := readArchiveIfAny(filepath.Join(br, "features.brarchive")); err != nil {
			return nil, err
		} else {
			for _, e := range list {
				bd.sum.Write([]byte(pack + "/" + e[0] + "\x00" + e[1] + "\x00"))
				if err := bd.addFeature(e[1]); err != nil {
					return nil, fmt.Errorf("%s %s: %w", pack, e[0], err)
				}
			}
		}
		if list, err := readArchiveIfAny(filepath.Join(br, "biomes.brarchive")); err != nil {
			return nil, err
		} else {
			for _, e := range list {
				bd.sum.Write([]byte(pack + "/" + e[0] + "\x00" + e[1] + "\x00"))
				if err := bd.addBiome(e[1]); err != nil {
					return nil, fmt.Errorf("%s %s: %w", pack, e[0], err)
				}
			}
		}
	}
	for pass, m := range maps {
		bd.order[pass] = m.keys()
	}
	if defs != "" {
		if err := bd.readDefinitions(defs); err != nil {
			return nil, err
		}
	}
	if len(bd.rules) == 0 || len(bd.biomes) == 0 {
		return nil, errors.New("the vanilla packs hold no feature rules or biomes")
	}
	if err := bd.compile(); err != nil {
		return nil, err
	}
	if defs == "" {
		missing := 0
		for k, n := range bd.Skipped {
			if strings.Contains(k, "unknown feature") || strings.Contains(k, "legacy feature") {
				missing += n
			}
		}
		var rules []string
		for _, id := range []string{"minecraft:overworld_surface_clay_feature", "minecraft:roofed_forest_surface_roofed_tree_feature_rules", "minecraft:overworld_extra_cave_carver_feature"} {
			if bd.rules[id] == nil {
				rules = append(rules, strings.TrimPrefix(id, "minecraft:"))
			}
		}
		bd.Warnings = append(bd.Warnings, fmt.Sprintf("no definitions folder next to %s: the rules and features only a Bedrock Dedicated "+
			"Server's definitions folder holds are missing (rules %s; %d rules skipped for features no pack has, among them the trees the legacy "+
			"tree table chooses), so the world will not match Bedrock's. Point BedrockDataDir at a Bedrock Dedicated Server folder.",
			packs, strings.Join(rules, ", "), missing))
	}
	bd.fossilTemplates()
	return bd, nil
}

func readArchiveIfAny(path string) ([][2]string, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	return readBrarchive(path)
}

func parseRule(text string) (*packRule, error) {
	var f struct {
		R *packRule `json:"minecraft:feature_rules"`
	}
	if err := json.Unmarshal(jsonText(text), &f); err != nil {
		return nil, err
	}
	if f.R == nil {
		return nil, errors.New("no minecraft:feature_rules")
	}
	return f.R, nil
}

// addFeature keeps a feature file unless one with its identifier was read first.
func (bd *BedrockData) addFeature(text string) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(jsonText(text), &top); err != nil {
		return err
	}
	for kind, body := range top {
		if kind == "format_version" {
			continue
		}
		var d struct {
			Description struct {
				Identifier string `json:"identifier"`
			} `json:"description"`
		}
		if err := json.Unmarshal(body, &d); err != nil {
			return err
		}
		id := d.Description.Identifier
		if _, ok := bd.features[id]; ok {
			continue // Newest pack first, then the definitions: the first copy seen wins.
		}
		bd.features[id] = packFeature{kind, body}
	}
	return nil
}

func (bd *BedrockData) addBiome(text string) error {
	var f struct {
		B struct {
			Description struct {
				Identifier string `json:"identifier"`
			} `json:"description"`
			Components map[string]json.RawMessage `json:"components"`
		} `json:"minecraft:biome"`
	}
	if err := json.Unmarshal(jsonText(text), &f); err != nil {
		return err
	}
	name := strings.TrimPrefix(f.B.Description.Identifier, "minecraft:")
	if _, ok := bd.biomes[name]; ok || name == "" {
		return nil
	}
	b := &packBiome{tags: map[string]bool{}, components: f.B.Components}
	var t struct {
		Tags []string `json:"tags"`
	}
	if raw, ok := f.B.Components["minecraft:tags"]; ok {
		json.Unmarshal(raw, &t)
	}
	for _, tag := range t.Tags {
		b.tags[tag] = true
	}
	bd.biomes[name] = b
	return nil
}

// readDefinitions adds the server's own feature_rules and features that no pack has.
func (bd *BedrockData) readDefinitions(defs string) error {
	for _, sub := range []string{"features", "feature_rules"} {
		files, _ := filepath.Glob(filepath.Join(defs, sub, "*.json"))
		sort.Strings(files)
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				return err
			}
			bd.sum.Write([]byte(filepath.Base(f) + "\x00" + string(raw) + "\x00"))
			if sub == "features" {
				if err := bd.addFeature(string(raw)); err != nil {
					return fmt.Errorf("%s: %w", f, err)
				}
				continue
			}
			r, err := parseRule(string(raw))
			if err != nil {
				return fmt.Errorf("%s: %w", f, err)
			}
			id := r.Description.Identifier
			if _, ok := bd.rules[id]; ok {
				continue
			}
			bd.rules[id] = r
			bd.order[r.Conditions.Pass] = append(bd.order[r.Conditions.Pass], id)
		}
	}
	return nil
}

// libcxxOrder models the iteration order of a libc++ unordered_map with unique keys and maximum
// load factor 1 (NOTES-bedrock-exact section 3): a key whose bucket is empty goes to the front of
// the list, a key whose bucket has nodes goes in front of that bucket's first node, and growing (to
// the next prime at or above 2n+1 when the size passes the bucket count) regroups the list by bucket
// keeping the relative order.
type libcxxOrder struct {
	list    []string
	hash    map[string]uint64
	buckets uint64
}

func (m *libcxxOrder) bucket(h uint64) uint64 {
	if m.buckets&(m.buckets-1) == 0 {
		return h & (m.buckets - 1)
	}
	return h % m.buckets
}

func isPrime(n uint64) bool {
	if n < 2 {
		return false
	}
	for p := uint64(2); p*p <= n; p++ {
		if n%p == 0 {
			return false
		}
	}
	return true
}

func (m *libcxxOrder) insert(key string, h uint64) {
	if m.hash == nil {
		m.hash = map[string]uint64{}
	}
	if _, ok := m.hash[key]; ok {
		return
	}
	n := uint64(len(m.list))
	if m.buckets == 0 || n+1 > m.buckets {
		pow2 := m.buckets > 2 && m.buckets&(m.buckets-1) == 0
		want := 2 * m.buckets
		if !pow2 {
			want++
		}
		want = max(want, n+1)
		if want == 1 {
			want = 2
		} else if want&(want-1) != 0 {
			for !isPrime(want) {
				want++
			}
		}
		if want > m.buckets {
			m.regroup(want)
		}
	}
	m.hash[key] = h
	b := m.bucket(h)
	for i, k := range m.list {
		if m.bucket(m.hash[k]) == b {
			m.list = slices.Insert(m.list, i, key)
			return
		}
	}
	m.list = slices.Insert(m.list, 0, key)
}

// regroup is libc++'s rehash: the list is walked once; a node whose bucket is new starts a group
// where it stands, and a node of a bucket already seen moves to just after that bucket's place.
func (m *libcxxOrder) regroup(nb uint64) {
	m.buckets = nb
	if len(m.list) == 0 {
		return
	}
	out := []string{m.list[0]}
	before := map[uint64]int{m.bucket(m.hash[m.list[0]]): -1}
	prev := m.bucket(m.hash[m.list[0]])
	for _, k := range m.list[1:] {
		b := m.bucket(m.hash[k])
		if b == prev {
			out = append(out, k)
			continue
		}
		at, seen := before[b]
		if !seen {
			before[b] = len(out) - 1
			out = append(out, k)
			prev = b
			continue
		}
		pos := at + 1
		out = slices.Insert(out, pos, k)
		for kb, v := range before {
			if v >= pos {
				before[kb] = v + 1
			}
		}
	}
	m.list = out
}

func (m *libcxxOrder) keys() []string { return slices.Clone(m.list) }

// biomeFilter evaluates a rule's minecraft:biome_filter over a biome's tags: a list is all of its
// entries; any_of, all_of and none_of nest; a test is has_biome_tag with == or !=. ok is false for
// a test this reader does not know.
func biomeFilter(raw json.RawMessage, tags map[string]bool) (match, ok bool) {
	raw = json.RawMessage(strings.TrimSpace(string(raw)))
	if len(raw) == 0 || string(raw) == "null" {
		return false, true // No filter: the rule never runs (as the deprecated rules).
	}
	if raw[0] == '[' {
		var list []json.RawMessage
		if json.Unmarshal(raw, &list) != nil {
			return false, false
		}
		for _, e := range list {
			m, ok := biomeFilter(e, tags)
			if !ok {
				return false, false
			}
			if !m {
				return false, true
			}
		}
		return true, true
	}
	var node map[string]json.RawMessage
	if json.Unmarshal(raw, &node) != nil {
		return false, false
	}
	for _, k := range []string{"any_of", "all_of", "none_of"} {
		v, has := node[k]
		if !has {
			continue
		}
		v = json.RawMessage(strings.TrimSpace(string(v)))
		var list []json.RawMessage
		if len(v) > 0 && v[0] == '[' {
			json.Unmarshal(v, &list)
		} else {
			list = []json.RawMessage{v}
		}
		any, all := false, true
		for _, e := range list {
			m, ok := biomeFilter(e, tags)
			if !ok {
				return false, false
			}
			any, all = any || m, all && m
		}
		switch k {
		case "any_of":
			return any, true
		case "all_of":
			return all, true
		}
		return !any, true
	}
	var t struct {
		Test, Operator string
		Value          string
	}
	if json.Unmarshal(raw, &t) != nil || t.Test != "has_biome_tag" {
		return false, false
	}
	has := tags[t.Value]
	switch t.Operator {
	case "", "==", "equals":
		return has, true
	case "!=", "not":
		return !has, true
	}
	return false, false
}

// blockName of a feature's block descriptor: a name, or a name with states, the legacy stone_type
// and dirt_type states naming their own blocks.
func packBlockName(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimPrefix(s, "minecraft:"), true
	}
	var d struct {
		Name   string         `json:"name"`
		States map[string]any `json:"states"`
	}
	if json.Unmarshal(raw, &d) != nil || d.Name == "" {
		return "", false
	}
	n := strings.TrimPrefix(d.Name, "minecraft:")
	if v, ok := d.States["stone_type"].(string); ok && v != "stone" {
		return v, true
	}
	if v, ok := d.States["dirt_type"].(string); ok && v == "coarse" {
		return "coarse_dirt", true
	}
	return n, true
}

// compile builds what the generator runs from the data.
func (bd *BedrockData) compile() error {
	bd.compileRules()
	bd.counts = map[string][2]int{}
	for name, rule := range map[string]string{"underwater_magma": "minecraft:overworld_underwater_magma_feature",
		"glow_lichen": "minecraft:overworld_underground_glow_lichen_feature"} {
		if r := bd.rules[rule]; r != nil {
			var it string
			if json.Unmarshal(r.Distribution["iterations"], &it) == nil {
				if m := randomRange.FindStringSubmatch(it); m != nil {
					lo, _ := strconv.Atoi(m[1])
					hi, _ := strconv.Atoi(m[2])
					bd.counts[name] = [2]int{lo, hi}
				}
			}
		}
	}
	for _, id := range bd.order["pregeneration_pass"] {
		r := bd.rules[id]
		f, ok := bd.features[r.Description.PlacesFeature]
		if !ok || f.kind != "minecraft:cave_carver_feature" {
			continue
		}
		if set := bd.ruleBiomes(r); set == ([2]uint64{}) {
			continue
		}
		var c struct {
			Skip   int32      `json:"skip_carve_chance"`
			Height int32      `json:"height_limit"`
			YScale [2]float32 `json:"y_scale"`
			H      [2]float32 `json:"horizontal_radius_multiplier"`
			V      [2]float32 `json:"vertical_radius_multiplier"`
			Floor  [2]float32 `json:"floor_level"`
			Width  float32    `json:"width_modifier"`
		}
		if err := json.Unmarshal(f.body, &c); err != nil {
			return fmt.Errorf("%s: %w", r.Description.PlacesFeature, err)
		}
		if c.Skip < 0 || c.Skip > 1<<16 || c.Height < 0 || c.Height > maxY-minY+1 {
			bd.problem(id, fmt.Sprintf("cave carver values out of range (skip %d, height %d)", c.Skip, c.Height))
			continue
		}
		bd.carvers = append(bd.carvers, caveCarver{skip: c.Skip, heightLimit: c.Height, yScale: c.YScale, roomH: c.H, roomV: c.V,
			floor: c.Floor, width: c.Width})
	}
	return bd.compileSurfaceBiomes()
}

// ruleBiomes is the set of our biomes a rule's filter passes.
func (bd *BedrockData) ruleBiomes(r *packRule) (set [2]uint64) {
	if f := strings.TrimSpace(string(r.Conditions.Filter)); f == "" || f == "null" {
		// A rule with no minecraft:biome_filter is attached to every biome, the Nether's too, and draws
		// its Math.Random there (NOTES-bedrock-exact T47: underwater magma).
		for i := range biomeTableNames {
			set[i>>6] |= 1 << (i & 63)
		}
		return set
	}
	for i := range biomeTableNames {
		b := bd.biomes[biomeRowDefs[i].String()]
		if b == nil {
			continue
		}
		if m, ok := biomeFilter(r.Conditions.Filter, b.tags); ok && m {
			set[i>>6] |= 1 << (i & 63)
		}
	}
	return set
}

// bedrockPasses are the decoration passes in the order a chunk runs them (dsim's PASSES).
var bedrockPasses = [7]string{"first_pass", "before_underground_pass", "underground_pass", "after_underground_pass",
	"before_surface_pass", "surface_pass", "after_surface_pass"}

// compileRules builds the passes' rules.
func (bd *BedrockData) compileRules() {
	bd.fc = &featCompiler{bd: bd, slots: map[string]*featSlot{}, ores: map[string]*bedrockOre{}}
	bd.javaKeep = map[string]bool{}
	for i, pass := range bedrockPasses {
		bd.passes[i] = bd.compilePass(pass)
	}
	bd.underground, bd.surface = bd.passes[2], bd.passes[5]
}

// problem notes data that cannot be used, once per rule, for the server log.
func (bd *BedrockData) problem(id, why string) {
	if bd.problemSeen == nil {
		bd.problemSeen = map[string]bool{}
	}
	if bd.problemSeen[id] {
		return
	}
	bd.problemSeen[id] = true
	bd.Problems = append(bd.Problems, id+": "+why)
}

// randomRange reads an iteration count written as Math.Random(lo, hi).
var randomRange = regexp.MustCompile(`(?i)^\s*math\.random(?:_integer)?\(\s*(\d+)\s*,\s*(\d+)\s*\)\s*$`)

// maxOreCount is the largest ore count read (vanilla's largest is 64).
const maxOreCount = 256

// compilePass makes the rules of a pass whose features are written, in the pass's order.
func (bd *BedrockData) compilePass(pass string) []*bedrockRule {
	var out []*bedrockRule
	for _, id := range bd.order[pass] {
		if rule := bd.compileRule(pass, id); rule != nil {
			out = append(out, rule)
		}
	}
	return out
}

// compileRule builds one rule. A rule whose data is broken (an error that is not just "not written
// yet", or a panic while reading it) is skipped and named in Problems.
func (bd *BedrockData) compileRule(pass, id string) (rule *bedrockRule) {
	skip := func(why string, err error) *bedrockRule {
		if isUnimpl(err) {
			bd.Skipped[pass+": "+why+shortReason(err.Error())]++
		} else {
			bd.Skipped[pass+": broken data"]++
			bd.problem(id, err.Error())
		}
		// A rule BDS never runs keeps no Java placement (NOTES-leftovers L24): its filter passes no biome
		// (has_biome_tag "deprecated") or it has 0 iterations (the legacy emerald rule, L29).
		if r := bd.rules[id]; r == nil || !bd.deadRule(r) {
			bd.keepJava(id)
		}
		return nil
	}
	defer func() {
		if p := recover(); p != nil {
			rule = skip("", fmt.Errorf("cannot be read: %v", p))
		}
	}()
	r := bd.rules[id]
	bd.lastOre = nil
	feat, err := bd.fc.feature(r.Description.PlacesFeature)
	if err != nil {
		rule := skip("", err)
		// A rule whose own scatter calls math.random still draws from the chunk's Random in BDS (T21):
		// keep it for its draws, placing nothing, so the stream stays where BDS has it.
		if isUnimpl(err) && distributionDraws(r.Distribution) {
			if sc, err := parseScatterParams(r.Distribution, bd); err == nil {
				rule = &bedrockRule{ident: id, hash: uint32(fnv1Hash(id)), sc: sc, biomes: bd.ruleBiomes(r)}
			}
		}
		return rule
	}
	sc, err := parseScatterParams(r.Distribution, bd)
	if err != nil {
		return skip("scatter ", err)
	}
	rule = &bedrockRule{ident: id, hash: uint32(fnv1Hash(id)), sc: sc, feat: feat}
	if id == "minecraft:overworld_underwater_magma_feature" {
		bd.magmaLive = true
	}
	if o := bd.fc.ores[r.Description.PlacesFeature]; o != nil {
		bd.ores[id] = o
	}
	rule.biomes = bd.ruleBiomes(r)
	return rule
}

// deadRule says whether BDS never places anything for a rule: no biome passes its filter, or its
// iteration count is the number 0.
func (bd *BedrockData) deadRule(r *packRule) bool {
	if bd.ruleBiomes(r) == ([2]uint64{}) {
		return true
	}
	if it, ok := r.Distribution["iterations"]; ok {
		var n float64
		if json.Unmarshal(it, &n) == nil && n == 0 {
			return true
		}
	}
	return false
}

// distributionDraws says whether a rule's scatter evaluates math.random.
func distributionDraws(dist map[string]json.RawMessage) bool {
	for _, v := range dist {
		if strings.Contains(strings.ToLower(string(v)), "math.random") {
			return true
		}
	}
	return false
}

// shortReason keeps the innermost part of a reason, for the Skipped counts.
func shortReason(s string) string {
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}

// oreFeature reads an ore_feature file (section 4).
func (bd *BedrockData) oreFeature(body json.RawMessage) (func(d *decoration, r *bmt, p pos), string) {
	var o struct {
		Count   float64 `json:"count"`
		Discard float32 `json:"discard_chance_on_air_exposure"`
		Rules   []struct {
			Places json.RawMessage   `json:"places_block"`
			May    []json.RawMessage `json:"may_replace"`
		} `json:"replace_rules"`
	}
	if json.Unmarshal(body, &o) != nil {
		return nil, "ore file"
	}
	if o.Count < 0 || o.Count > maxOreCount {
		return nil, fmt.Sprintf("ore count %v out of range", o.Count)
	}
	var rules []oreRule
	for _, q := range o.Rules {
		pb, ok := packBlockName(q.Places)
		if !ok {
			return nil, "ore block"
		}
		if _, err := safeState(pb); err != nil {
			return nil, "unknown ore block " + pb
		}
		var may []string
		for _, m := range q.May {
			n, ok := packBlockName(m)
			if !ok {
				return nil, "ore block"
			}
			if _, err := safeState(n); err != nil {
				return nil, "unknown ore block " + n
			}
			may = append(may, n)
		}
		rules = append(rules, oreRule{pb, strings.Join(may, " ")})
	}
	bd.lastOre = newBedrockOre(int(o.Count), o.Discard, rules)
	return bd.lastOre.place, ""
}

// compileSurfaceBiomes reads each of our biomes' surface builder (phase 5, S1).
func (bd *BedrockData) compileSurfaceBiomes() error {
	str := func(m map[string]json.RawMessage, k string) string {
		if raw, ok := m[k]; ok {
			if n, ok := packBlockName(raw); ok {
				return n
			}
		}
		return ""
	}
	for i := range biomeTableNames {
		name := biomeRowDefs[i].String()
		b := bd.biomes[name]
		if b == nil && optionalBiome(i) {
			continue // An older install: the profile keeps Java's biome table (NewWithData).
		}
		if b == nil {
			return fmt.Errorf("the packs have no biome %s", name)
		}
		var sb struct {
			Builder map[string]json.RawMessage `json:"builder"`
		}
		json.Unmarshal(b.components["minecraft:surface_builder"], &sb)
		var kind string
		json.Unmarshal(sb.Builder["type"], &kind)
		var sub struct {
			Builder map[string]json.RawMessage `json:"builder"`
		}
		json.Unmarshal(b.components["minecraft:subsurface_builder"], &sub)
		var subKind string
		json.Unmarshal(sub.Builder["type"], &subKind)
		if subKind == "minecraft:noise_gradient" {
			gd, err := parseGradient(sub.Builder)
			if err != nil {
				return fmt.Errorf("biome %s: %w", name, err)
			}
			bd.subsurface[i] = gd
		}
		var depth int
		json.Unmarshal(sb.Builder["sea_floor_depth"], &depth)
		var cl struct {
			T float32 `json:"temperature"`
		}
		if raw, ok := b.components["minecraft:climate"]; ok && json.Unmarshal(raw, &cl) == nil {
			bd.climate[i], bd.climateSet[i] = cl.T, true
		}
		row := surfaceRow{biome: name, kind: strings.TrimPrefix(kind, "minecraft:"), top: str(sb.Builder, "top_material"),
			mid: str(sb.Builder, "mid_material"), floor: str(sb.Builder, "sea_floor_material"),
			foundation: str(sb.Builder, "foundation_material"), sea: str(sb.Builder, "sea_material"), seaDepth: depth, temperature: cl.T}
		json.Unmarshal(sb.Builder["bryce_pillars"], &row.bryce)
		_, row.partiallyFrozen = b.components["minecraft:partially_frozen"]
		json.Unmarshal(sb.Builder["max_puddle_depth_below_sea_level"], &row.puddleDepth)
		json.Unmarshal(sb.Builder["has_forest"], &row.forest)
		var mp struct {
			Steep map[string]json.RawMessage `json:"steep_material_adjustment"`
		}
		json.Unmarshal(b.components["minecraft:mountain_parameters"], &mp)
		if mp.Steep != nil {
			row.steep.material = str(mp.Steep, "material")
			flag := func(k string) bool { var v bool; json.Unmarshal(mp.Steep[k], &v); return v }
			row.steep.north, row.steep.south, row.steep.west, row.steep.east = flag("north_slopes"), flag("south_slopes"), flag("west_slopes"), flag("east_slopes")
		}
		var adj struct {
			A []struct {
				M     map[string]json.RawMessage `json:"materials"`
				Scale float32                    `json:"noise_frequency_scale"`
				Range [2]float32                 `json:"noise_range"`
				H     json.RawMessage            `json:"height_range"`
			} `json:"adjustments"`
		}
		json.Unmarshal(b.components["minecraft:surface_material_adjustments"], &adj)
		for _, a := range adj.A {
			if len(a.H) > 0 {
				if isNetherBiome(uint8(i)) {
					continue // The Nether's surface reads its own (nether_surface.go).
				}
				return fmt.Errorf("biome %s: height ranges in surface adjustments are not written yet", name)
			}
			row.adjust = append(row.adjust, surfaceAdjust{a.Scale, a.Range[0], a.Range[1], str(a.M, "top_material"),
				str(a.M, "mid_material"), str(a.M, "sea_floor_material"), str(a.M, "foundation_material"), str(a.M, "sea_material")})
		}
		bd.surfaceBiomes[i] = row.compile()
	}
	return nil
}

// Hash names the data read: a SHA-256 of every file's name and text in the order read. Two installs
// with the same vanilla data give the same hash, and so the same world.
func (bd *BedrockData) Hash() string {
	if bd.sum == nil {
		return ""
	}
	return hex.EncodeToString(bd.sum.Sum(nil))
}

// Summary is one line on what was read, for the server log.
func (bd *BedrockData) Summary() string {
	n := 0
	for _, p := range bd.passes {
		n += len(p)
	}
	return fmt.Sprintf("%d feature rules, %d features, %d biomes; %d rules written (%d underground, %d surface), %d cave carvers",
		len(bd.rules), len(bd.features), len(bd.biomes), n, len(bd.underground), len(bd.surface), len(bd.carvers))
}

// defaultBedrockData is what New and NewWithProfile use.
var defaultBedrockData *BedrockData

// SetDefaultBedrockData sets the feature data New and NewWithProfile use; nil for none.
func SetDefaultBedrockData(bd *BedrockData) { defaultBedrockData = bd }

// jsonText drops the comments Bedrock's JSON files may carry (// to the end of a line, /* */), outside
// strings, and a byte-order mark.
func jsonText(text string) []byte {
	text = strings.TrimPrefix(text, "\uFEFF")
	out := make([]byte, 0, len(text))
	inStr, esc := false, false
	for i := 0; i < len(text); i++ {
		c := text[i]
		if inStr {
			out = append(out, c)
			switch {
			case esc:
				esc = false
			case c == 92: // a backslash
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch {
		case c == '"':
			inStr = true
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
			out = append(out, '\n')
			continue
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			i += 2
			for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
				i++
			}
			i++
			continue
		}
		out = append(out, c)
	}
	return out
}
