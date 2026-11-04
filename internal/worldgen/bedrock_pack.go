package worldgen

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
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
	underground, surface []*bedrockRule
	carvers              []caveCarver
	ores                 map[string]*bedrockOre // By rule identifier, for tests.
	lastOre              *bedrockOre
	surfaceBiomes        [len(biomeTableNames)]surfaceBiome
	// Skipped counts the rules of the passes used that are not written yet, by reason.
	Skipped map[string]int
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
	bd := &BedrockData{Dir: dir, rules: map[string]*packRule{}, order: map[string][]string{}, features: map[string]packFeature{},
		biomes: map[string]*packBiome{}, Skipped: map[string]int{}, ores: map[string]*bedrockOre{}}
	maps := map[string]*libcxxOrder{}
	for _, pack := range names {
		br := filepath.Join(packs, pack, "__brarchive")
		if list, err := readArchiveIfAny(filepath.Join(br, "feature_rules.brarchive")); err != nil {
			return nil, err
		} else {
			for _, e := range list {
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
				if err := bd.addFeature(e[1]); err != nil {
					return nil, fmt.Errorf("%s %s: %w", pack, e[0], err)
				}
			}
		}
		if list, err := readArchiveIfAny(filepath.Join(br, "biomes.brarchive")); err != nil {
			return nil, err
		} else {
			for _, e := range list {
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
	bd.underground = bd.compilePass("underground_pass")
	bd.surface = bd.compilePass("surface_pass")
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
		bd.carvers = append(bd.carvers, caveCarver{skip: c.Skip, heightLimit: c.Height, yScale: c.YScale, roomH: c.H, roomV: c.V,
			floor: c.Floor, width: c.Width})
	}
	return bd.compileSurfaceBiomes()
}

// ruleBiomes is the set of our biomes a rule's filter passes.
func (bd *BedrockData) ruleBiomes(r *packRule) (set [2]uint64) {
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

// compilePass makes the rules of a pass that place a feature written here, in the pass's order.
func (bd *BedrockData) compilePass(pass string) []*bedrockRule {
	var out []*bedrockRule
	for _, id := range bd.order[pass] {
		r := bd.rules[id]
		bd.lastOre = nil
		place, why := bd.featurePlacer(r.Description.PlacesFeature)
		if place == nil {
			bd.Skipped[pass+": "+why]++
			continue
		}
		rule, why := parseScatter(r.Distribution)
		if rule == nil {
			bd.Skipped[pass+": scatter "+why]++
			continue
		}
		rule.ident, rule.hash, rule.place = id, uint32(fnv1Hash(id)), place
		if o := bd.lastOre; o != nil {
			bd.ores[id] = o
		}
		rule.biomes = bd.ruleBiomes(r)
		out = append(out, rule)
	}
	return out
}

// featurePlacer gives a feature's place function, or nil and why not. The features registered in
// code (no file) are the dirt rule's ore (section 4b) and the disks (R5).
func (bd *BedrockData) featurePlacer(id string) (func(d *decoration, r *bmt, p pos), string) {
	switch id {
	case "minecraft:dirt_feature":
		bd.lastOre = newBedrockOre(33, 0, []oreRule{{"dirt", "stone"}})
		return bd.lastOre.place, ""
	case "minecraft:sand_feature":
		return sandDisk{block: sSand, radius: 7}.place, ""
	case "minecraft:gravel_feature":
		return sandDisk{block: sGravel, radius: 6}.place, ""
	case "minecraft:clay_feature":
		return clayDisk, ""
	}
	f, ok := bd.features[id]
	if !ok {
		return nil, "unknown feature"
	}
	if f.kind != "minecraft:ore_feature" {
		return nil, f.kind
	}
	var o struct {
		Count   float64 `json:"count"`
		Discard float32 `json:"discard_chance_on_air_exposure"`
		Rules   []struct {
			Places json.RawMessage   `json:"places_block"`
			May    []json.RawMessage `json:"may_replace"`
		} `json:"replace_rules"`
	}
	if json.Unmarshal(f.body, &o) != nil {
		return nil, "ore file"
	}
	var rules []oreRule
	for _, q := range o.Rules {
		pb, ok := packBlockName(q.Places)
		if !ok {
			return nil, "ore block"
		}
		var may []string
		for _, m := range q.May {
			n, ok := packBlockName(m)
			if !ok {
				return nil, "ore block"
			}
			may = append(may, n)
		}
		rules = append(rules, oreRule{pb, strings.Join(may, " ")})
	}
	bd.lastOre = newBedrockOre(int(o.Count), o.Discard, rules)
	return bd.lastOre.place, ""
}

// aboveTopSolidExpr is the one Molang coordinate written so far.
var aboveTopSolidExpr = regexp.MustCompile(`^\s*query\.above_top_solid\(\s*variable\.worldx\s*,\s*variable\.worldz\s*\)\s*$`)

// parseScatter reads a rule's distribution (section 3): iterations, scatter chance, coordinate order
// (default xzy, S3) and the three coordinates. It returns nil and why for what is not written yet.
func parseScatter(d map[string]json.RawMessage) (*bedrockRule, string) {
	r := &bedrockRule{order: evalOrder("xzy")}
	var it float64
	if raw, ok := d["iterations"]; ok {
		if json.Unmarshal(raw, &it) != nil {
			return nil, "iterations expression"
		}
	} else {
		it = 1
	}
	r.iterations = int(math.Round(it))
	if raw, ok := d["scatter_chance"]; ok {
		var frac struct{ Numerator, Denominator int }
		var pct float64
		switch {
		case json.Unmarshal(raw, &pct) == nil:
			r.percent, r.hasPercent = float32(pct), true
		case json.Unmarshal(raw, &frac) == nil && frac.Denominator > 0:
			r.num, r.den = frac.Numerator, frac.Denominator
		default:
			return nil, "chance expression"
		}
	}
	if raw, ok := d["coordinate_eval_order"]; ok {
		var s string
		if json.Unmarshal(raw, &s) != nil || len(s) != 3 {
			return nil, "order"
		}
		r.order = evalOrder(s)
	}
	for i, k := range []string{"x", "y", "z"} {
		raw, ok := d[k]
		if !ok {
			continue // Absent: 0.
		}
		var num float64
		var s string
		var dist struct {
			Distribution string             `json:"distribution"`
			Extent       [2]json.RawMessage `json:"extent"`
		}
		switch {
		case json.Unmarshal(raw, &num) == nil:
			v := int(math.Round(num))
			r.axes[i] = bAxis{bSingle, v, v}
		case json.Unmarshal(raw, &s) == nil:
			if !aboveTopSolidExpr.MatchString(s) {
				return nil, "coordinate expression"
			}
			r.axes[i] = bAxis{kind: bAboveTopSolid}
		case json.Unmarshal(raw, &dist) == nil:
			var lo, hi float64
			if json.Unmarshal(dist.Extent[0], &lo) != nil || json.Unmarshal(dist.Extent[1], &hi) != nil {
				return nil, "extent expression"
			}
			kind, ok := map[string]bAxisKind{"": bUniform, "uniform": bUniform, "gaussian": bGaussian,
				"inverse_gaussian": bInverseGaussian, "triangle": bTriangle}[dist.Distribution]
			if !ok {
				return nil, "distribution " + dist.Distribution
			}
			r.axes[i] = bAxis{kind, int(math.Round(lo)), int(math.Round(hi))}
		default:
			return nil, "coordinate"
		}
	}
	return r, ""
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
		if b == nil {
			return fmt.Errorf("the packs have no biome %s", name)
		}
		var sb struct {
			Builder map[string]json.RawMessage `json:"builder"`
		}
		json.Unmarshal(b.components["minecraft:surface_builder"], &sb)
		var kind string
		json.Unmarshal(sb.Builder["type"], &kind)
		var depth int
		json.Unmarshal(sb.Builder["sea_floor_depth"], &depth)
		var cl struct {
			T float32 `json:"temperature"`
		}
		json.Unmarshal(b.components["minecraft:climate"], &cl)
		row := surfaceRow{biome: name, kind: strings.TrimPrefix(kind, "minecraft:"), top: str(sb.Builder, "top_material"),
			mid: str(sb.Builder, "mid_material"), floor: str(sb.Builder, "sea_floor_material"),
			foundation: str(sb.Builder, "foundation_material"), sea: str(sb.Builder, "sea_material"), seaDepth: depth, temperature: cl.T}
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
				return fmt.Errorf("biome %s: height ranges in surface adjustments are not written yet", name)
			}
			row.adjust = append(row.adjust, surfaceAdjust{a.Scale, a.Range[0], a.Range[1], str(a.M, "top_material"),
				str(a.M, "mid_material"), str(a.M, "sea_floor_material"), str(a.M, "foundation_material"), str(a.M, "sea_material")})
		}
		bd.surfaceBiomes[i] = row.compile()
	}
	return nil
}

// Summary is one line on what was read, for the server log.
func (bd *BedrockData) Summary() string {
	return fmt.Sprintf("%d feature rules, %d features, %d biomes; %d underground and %d surface rules, %d cave carvers written",
		len(bd.rules), len(bd.features), len(bd.biomes), len(bd.underground), len(bd.surface), len(bd.carvers))
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
