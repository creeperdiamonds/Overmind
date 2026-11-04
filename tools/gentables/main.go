// Command gentables builds internal/tables/java261 from a minecraft-data checkout.
//
// Usage: go run ./tools/gentables -data <checkout outside the repo>
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type state struct {
	Name      string   `json:"name"`
	Type      string   `json:"type"`
	NumValues int      `json:"num_values"`
	Values    []string `json:"values,omitempty"`
}

type block struct {
	Name         string  `json:"name"`
	DefaultState int     `json:"defaultState"`
	MinStateID   int     `json:"minStateId"`
	MaxStateID   int     `json:"maxStateId"`
	States       []state `json:"states"`
}

type item struct {
	ID          int32  `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

func main() {
	data := flag.String("data", "", "minecraft-data checkout (outside the repo)")
	azalea := flag.String("azalea", "", "azalea-registry/src of azalea-rs/azalea at its 26.1 commit (outside the repo): menu and block entity type IDs, and tags")
	azaleaCommit := flag.String("azalea-commit", "eeaf1435e81d9cbd8daa0efa22029c1f259a64b5", "the azalea commit those files are from, for the tables' header")
	out := flag.String("out", "internal/tables/java261", "package directory to write")
	flag.Parse()
	if *data == "" {
		fatal(fmt.Errorf("-data is required"))
	}
	commit, err := exec.Command("git", "-C", *data, "rev-parse", "HEAD").Output()
	if err != nil {
		fatal(fmt.Errorf("reading commit of %s: %w", *data, err))
	}
	g := &gen{
		pc:     filepath.Join(*data, "data", "pc", "26.1"),
		be:     filepath.Join(*data, "data", "bedrock", "1.26.30"),
		out:    *out,
		header: "# source: PrismarineJS/minecraft-data " + strings.TrimSpace(string(commit)) + " (pc 26.1, bedrock 1.26.30)",
	}
	g.blocks()
	g.items()
	g.biomes()
	g.entities()
	g.packets()
	g.components()
	g.metadata()
	g.registries()
	g.recipes()
	g.dimensions()
	if *azalea != "" {
		g.azalea(*azalea, *azaleaCommit)
	}
}

type gen struct {
	pc, be, out, header string
	dynamic             map[string][]string // registry ID to entry keys, as sent in configuration
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "gentables:", err)
	os.Exit(1)
}

func readJSON(path string, v any) {
	b, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(b, v)
	}
	if err != nil {
		fatal(err)
	}
}

// writeLines stores lines gzip-compressed under data/, led by the source header.
func (g *gen) writeLines(name string, lines []string) {
	var buf bytes.Buffer
	zw, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	zw.Write([]byte(g.header + "\n" + strings.Join(lines, "\n") + "\n"))
	zw.Close()
	g.write(filepath.Join("data", name+".gz"), buf.Bytes())
}

func (g *gen) write(rel string, b []byte) {
	p := filepath.Join(g.out, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		fatal(err)
	}
	if err := os.WriteFile(p, b, 0o644); err != nil {
		fatal(err)
	}
}

func (g *gen) writeJSON(rel string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		fatal(err)
	}
	g.write(rel, b)
}

// resolver turns Java "name[prop=value]" strings into state IDs.
type resolver map[string]*block

func (r resolver) stateID(s string) (int, bool) {
	name, props, _ := strings.Cut(strings.TrimSuffix(s, "]"), "[")
	b := r[strings.TrimPrefix(name, "minecraft:")]
	if b == nil {
		return 0, false
	}
	idx := make([]int, len(b.States))
	rem := b.DefaultState - b.MinStateID
	for i := len(b.States) - 1; i >= 0; i-- {
		idx[i] = rem % b.States[i].NumValues
		rem /= b.States[i].NumValues
	}
	if props != "" {
		for _, kv := range strings.Split(props, ",") {
			k, v, _ := strings.Cut(kv, "=")
			found := false
			for i, st := range b.States {
				if st.Name != k {
					continue
				}
				vi := valueIndex(st, v)
				if vi < 0 {
					return 0, false
				}
				idx[i], found = vi, true
			}
			if !found {
				return 0, false
			}
		}
	}
	id := b.MinStateID
	stride := 1
	for i := len(b.States) - 1; i >= 0; i-- {
		id += idx[i] * stride
		stride *= b.States[i].NumValues
	}
	return id, true
}

// valueIndex: bool states list true first, as the game numbers them.
func valueIndex(st state, v string) int {
	if st.Type == "bool" {
		switch v {
		case "true":
			return 0
		case "false":
			return 1
		}
		return -1
	}
	for i, x := range st.Values {
		if x == v {
			return i
		}
	}
	return -1
}

// stairShapes are the Bedrock minecraft:corner values; each is the same word as the Java stairs
// shape, except that none is straight. The pinned mapping gives every stair the single shape
// outer_right and has no corner property, so without these a straight stair would turn into a corner.
var stairShapes = map[string]string{"none": "straight", "inner_left": "inner_left", "inner_right": "inner_right", "outer_left": "outer_left", "outer_right": "outer_right"}

// cornerVariants adds, for a stairs mapping, one key per corner value.
func cornerVariants(r resolver, key, java string) []string {
	if !strings.Contains(java, "shape=outer_right") {
		return nil
	}
	name, props, _ := strings.Cut(strings.TrimSuffix(key, "]"), "[")
	var out []string
	for corner, shape := range stairShapes {
		id, ok := r.stateID(strings.Replace(java, "shape=outer_right", "shape="+shape, 1))
		if !ok {
			continue
		}
		parts := append([]string{"minecraft:corner=" + corner}, strings.Split(props, ",")...)
		if props == "" {
			parts = parts[:1]
		}
		sort.Strings(parts)
		out = append(out, name+"["+strings.Join(parts, ",")+"]	"+strconv.Itoa(id))
	}
	sort.Strings(out)
	return out
}

func (g *gen) blocks() {
	var bl []block
	readJSON(filepath.Join(g.pc, "blocks.json"), &bl)
	r := resolver{}
	for i := range bl {
		r[bl[i].Name] = &bl[i]
	}
	g.writeJSON("testdata/blocks.json", bl)
	var b2j map[string]string
	readJSON(filepath.Join(g.be, "blocksB2J.json"), &b2j)
	keys := make([]string, 0, len(b2j))
	for k := range b2j {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	bad := 0
	for _, k := range keys {
		id, ok := r.stateID(b2j[k])
		if !ok {
			bad++
			continue
		}
		lines = append(lines, k+"\t"+strconv.Itoa(id))
		lines = append(lines, cornerVariants(r, k, b2j[k])...)
	}
	g.writeLines("blocks", lines)
	// Default state of every Java block, for blocks whose exact state has no mapping.
	defaults := make([]string, 0, len(bl))
	for _, b := range bl {
		defaults = append(defaults, fmt.Sprintf("minecraft:%s\t%d\t%d\t%d", b.Name, b.MinStateID, b.MaxStateID, b.DefaultState))
	}
	g.writeLines("defaults", defaults)
	// The properties of every Java block with properties, in the order the state
	// IDs count them (the last varies fastest), each with its values in order: a
	// bool is "true,false", as the game numbers it (valueIndex).
	var props []string
	for _, b := range bl {
		if len(b.States) == 0 {
			continue
		}
		l := fmt.Sprintf("minecraft:%s\t%d", b.Name, b.MinStateID)
		for _, st := range b.States {
			vals := st.Values
			if st.Type == "bool" {
				vals = []string{"true", "false"}
			}
			if len(vals) != st.NumValues {
				fatal(fmt.Errorf("%s.%s: %d values listed, %d counted", b.Name, st.Name, len(vals), st.NumValues))
			}
			l += "\t" + st.Name + "=" + strings.Join(vals, ",")
		}
		props = append(props, l)
	}
	g.writeLines("properties", props)
	fmt.Printf("java blocks: %d, bedrock mappings: %d written, %d unresolvable, %d with properties\n", len(bl), len(lines), bad, len(props))
}

func (g *gen) items() {
	var ji, bi []item
	readJSON(filepath.Join(g.pc, "items.json"), &ji)
	readJSON(filepath.Join(g.be, "items.json"), &bi)
	var b2j map[string]string
	readJSON(filepath.Join(g.be, "blocksB2J.json"), &b2j)
	javaID := map[string]int32{}
	for _, it := range ji {
		javaID[it.Name] = it.ID
	}
	// A block item may be named differently on the Java side (snow_layer / snow).
	// The state with no properties wins; otherwise the first key in sorted order.
	keys := make([]string, 0, len(b2j))
	for k := range b2j {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	blockItem := map[string]string{}
	for _, k := range keys {
		bn, props, _ := strings.Cut(strings.TrimPrefix(k, "minecraft:"), "[")
		jn, _, _ := strings.Cut(strings.TrimPrefix(b2j[k], "minecraft:"), "[")
		if _, ok := javaID[jn]; !ok {
			continue
		}
		if _, seen := blockItem[bn]; !seen || props == "]" {
			blockItem[bn] = jn
		}
	}
	g.writeJSON("testdata/items.json", javaID)
	// Display names of both lists, for the tests: a Bedrock item and the Java item it maps to
	// should be the same thing.
	names := map[string]map[string]string{"bedrock": {}, "java": {}}
	for _, it := range bi {
		names["bedrock"][it.Name] = it.DisplayName
	}
	for _, it := range ji {
		names["java"][it.Name] = it.DisplayName
	}
	g.writeJSON("testdata/itemnames.json", names)
	// Java item names and IDs, for data that names Java items (recipes).
	javaNames := make([]string, 0, len(ji))
	for _, it := range ji {
		javaNames = append(javaNames, it.Name+"\t"+strconv.Itoa(int(it.ID)))
	}
	sort.Strings(javaNames)
	g.writeLines("javaitems", javaNames)
	sort.Slice(bi, func(i, j int) bool { return bi[i].Name < bi[j].Name })
	javaDisplay := map[string]string{}
	for _, it := range ji {
		javaDisplay[it.Name] = it.DisplayName
	}
	sameThing := func(be item, java string) bool { return strings.EqualFold(be.DisplayName, javaDisplay[java]) }
	var entries []itemEntry
	for _, it := range bi {
		if it.Name == "unknown" || strings.HasPrefix(it.Name, "item.") || it.Name == "bed" || it.Name == "banner" {
			continue
		}
		jn := it.Name
		block, isBlock := blockItem[it.Name]
		_, javaHasName := javaID[jn]
		switch {
		case !javaHasName:
			jn = block
		case isBlock && block != jn:
			// The Bedrock name is a different Java item's (Bedrock's stone_stairs are
			// the cobblestone stairs, Java's stone_stairs the stone ones): the block
			// item is the item of its block. Both lists' display names must agree.
			if !sameThing(it, block) {
				fatal(fmt.Errorf("item %s (%q): its block is Java %s (%q), but the name is Java's %s (%q)", it.Name, it.DisplayName, block, javaDisplay[block], jn, javaDisplay[jn]))
			}
			fmt.Printf("item %s is Java %s, not Java %s (%q, %q)\n", it.Name, block, jn, it.DisplayName, javaDisplay[jn])
			jn = block
		}
		if alias, ok := itemAliases[it.Name]; ok {
			if _, has := javaID[jn]; has || !sameThing(it, alias) {
				fatal(fmt.Errorf("item alias %s -> %s: already mapped, or the display names %q and %q differ", it.Name, alias, it.DisplayName, javaDisplay[alias]))
			}
			jn = alias
		}
		if id, ok := javaID[jn]; ok {
			entries = append(entries, itemEntry{key: it.Name, java: jn, id: id})
		}
	}
	// Beds and banners carry their colour as meta and are one Java item per colour. The banner
	// meta is inverted: Bedrock counts banner colours from black.
	for i, c := range itemColours {
		for _, base := range []string{"bed", "banner"} {
			meta := i
			if base == "banner" {
				meta = len(itemColours) - 1 - i
			}
			if id, ok := javaID[c+"_"+base]; ok {
				entries = append(entries, itemEntry{key: fmt.Sprintf("%s#%d", base, meta), java: c + "_" + base, id: id})
			}
		}
	}
	// Variants Java carries as components on a single item: any meta gives the same ID.
	for _, name := range componentItems {
		if id, ok := javaID[name]; ok {
			entries = append(entries, itemEntry{key: name + "#*", java: name, id: id})
		}
	}
	// A tipped arrow is its own Java item.
	if id, ok := javaID["tipped_arrow"]; ok {
		entries = append(entries, itemEntry{key: "arrow#+", java: "tipped_arrow", id: id})
	}
	// One Bedrock name per Java item for the reverse direction: the one named like the Java
	// item if there is one, else the first. Lit, double-slab and standing or wall block names
	// win only where there is no other (Java's jack_o_lantern is Bedrock's lit_pumpkin).
	rank := func(e itemEntry) int {
		switch {
		case e.key == e.java:
			return 0
		case strings.HasPrefix(e.key, "lit_") || strings.HasSuffix(e.key, "_double_slab") ||
			strings.HasPrefix(e.key, "standing_") || strings.HasPrefix(e.key, "wall_"):
			return 2
		}
		return 1
	}
	best := map[int32]int{}
	for i, e := range entries {
		if last := e.key[len(e.key)-1]; last == '*' || last == '+' {
			continue
		}
		if b, ok := best[e.id]; !ok || rank(e) < rank(entries[b]) {
			best[e.id] = i
		}
	}
	lines := make([]string, 0, len(entries))
	for i, e := range entries {
		l := e.key + "\t" + strconv.Itoa(int(e.id))
		if b, ok := best[e.id]; ok && b == i {
			l += "\tr"
		}
		lines = append(lines, l)
	}
	sort.Strings(lines)
	g.writeLines("items", lines)
	fmt.Printf("java items: %d, bedrock items: %d, lines: %d, reverse: %d\n", len(ji), len(bi), len(lines), len(best))
}

// itemEntry is one forward item mapping: the Bedrock key (name, name#meta, name#* for any meta,
// name#+ for any non-zero meta), the Java name and the Java ID.
type itemEntry struct {
	key, java string
	id        int32
}

// itemColours lists the colours in Bedrock meta order, white first. The test checks the order
// against Dragonfly.
var itemColours = []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray", "light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"}

// itemAliases are Bedrock items named unlike their Java item and not block items, so neither
// the name nor the block mapping finds them. Each is the Java item of the same display name in
// minecraft-data (the generator checks it): Bedrock's netherbrick is "Nether Brick", Java's
// nether_brick; frame and glow_frame are "Item Frame" and "Glow Item Frame"; empty_map is
// "Empty Map", Java's map; zombie_pigman_spawn_egg is "Zombified Piglin Spawn Egg".
var itemAliases = map[string]string{
	"netherbrick":             "nether_brick",
	"frame":                   "item_frame",
	"glow_frame":              "glow_item_frame",
	"empty_map":               "map",
	"zombie_pigman_spawn_egg": "zombified_piglin_spawn_egg",
}

// componentItems keep the one Java item whatever the Bedrock meta.
var componentItems = []string{"potion", "splash_potion", "lingering_potion", "goat_horn", "suspicious_stew", "firework_star"}

func (g *gen) biomes() {
	var bs []item
	readJSON(filepath.Join(g.pc, "biomes.json"), &bs)
	var lines []string
	for _, b := range bs {
		lines = append(lines, b.Name+"\t"+strconv.Itoa(int(b.ID)))
	}
	g.writeLines("biomes", lines)
	fmt.Printf("biomes: %d\n", len(bs))
}

func (g *gen) entities() {
	var es []item
	readJSON(filepath.Join(g.pc, "entities.json"), &es)
	var lines []string
	for _, e := range es {
		lines = append(lines, e.Name+"\t"+strconv.Itoa(int(e.ID)))
	}
	sort.Strings(lines)
	g.writeLines("entities", lines)
	fmt.Printf("entities: %d\n", len(es))

	g.bedrockEntities()
}

// bedrockEntities pairs Bedrock entity names with Java ones. minecraft-data has
// no Bedrock entity list for 1.26.60: its dataPaths give bedrock/1.21.80's
// entities.json, which stops before the allay. So the Bedrock names come from
// the three files dataPaths gives for 1.26.60 that name entities: entities.json
// (name and display name), language.json (entity.<name>.name keys, with their
// English names; it has the 1.19 to 1.21 mobs) and items.json (<name>_spawn_egg,
// the only one with the 2025 mobs: happy ghast, copper golem, nautilus and the
// rest). A Bedrock name pairs with, in order, the Java entity of the same name,
// of the same name less "_projectile" (Bedrock's wind_charge_projectile), or of
// the same display name (language.json's, then entities.json's first), when
// only one Java entity has it. A spawn egg's name is not always the entity's
// (evoker_spawn_egg spawns evocation_illager), so it pairs only by the same name
// and only for a Java entity nothing else pairs with.
func (g *gen) bedrockEntities() {
	dataDir := filepath.Dir(filepath.Dir(g.be))
	var paths map[string]map[string]map[string]string
	readJSON(filepath.Join(dataDir, "dataPaths.json"), &paths)
	be := paths["bedrock"][bedrockVersion]
	if be == nil {
		fatal(fmt.Errorf("dataPaths.json: no bedrock %s", bedrockVersion))
	}
	type named struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	}
	var java, ents, items []named
	readJSON(filepath.Join(g.pc, "entities.json"), &java)
	readJSON(filepath.Join(dataDir, be["entities"], "entities.json"), &ents)
	readJSON(filepath.Join(dataDir, be["items"], "items.json"), &items)
	var lang map[string]string
	readJSON(filepath.Join(dataDir, be["language"], "language.json"), &lang)

	javaName, displays := map[string]string{}, map[string][]string{}
	for _, e := range java {
		javaName[e.Name] = e.DisplayName
		displays[e.DisplayName] = append(displays[e.DisplayName], e.Name)
	}
	// Every Bedrock name with its display names: language.json's, then entities.json's first.
	display := map[string][]string{}
	var names []string
	add := func(name, d string) {
		if _, ok := display[name]; !ok {
			names = append(names, name)
			display[name] = nil
		}
		if d != "" {
			display[name] = append(display[name], d)
		}
	}
	for k, v := range lang {
		n, ok := strings.CutPrefix(k, "entity.")
		if n, ok2 := strings.CutSuffix(n, ".name"); ok && ok2 && !strings.ContainsAny(n, ".:") && n != "unknown" {
			add(n, v)
		}
	}
	seen := map[string]bool{}
	for _, e := range ents {
		if !seen[e.Name] {
			seen[e.Name] = true
			add(e.Name, e.DisplayName)
		}
	}
	sort.Strings(names)
	pair := map[string]string{}
	taken := map[string]bool{}
	for _, n := range names {
		java := ""
		if _, ok := javaName[n]; ok {
			java = n
		} else if s, ok := strings.CutSuffix(n, "_projectile"); ok && javaName[s] != "" {
			java = s
		} else {
			for _, d := range display[n] {
				if len(displays[d]) == 1 {
					java = displays[d][0]
					break
				}
			}
		}
		if java != "" {
			pair[n] = java
			taken[java] = true
		}
	}
	var eggs []string
	for _, it := range items {
		if n, ok := strings.CutSuffix(it.Name, "_spawn_egg"); ok {
			eggs = append(eggs, n)
			if _, ok := javaName[n]; ok && !taken[n] {
				pair[n], taken[n] = n, true
				fmt.Printf("bedrock entity %s: from its spawn egg only\n", n)
			}
		}
	}
	var lines []string
	for n, j := range pair {
		lines = append(lines, n+"\t"+j)
	}
	sort.Strings(lines)
	g.writeLines("bedrockentities", lines)
	sort.Strings(eggs)
	// The Bedrock names and display names as read, for the completeness test.
	g.writeJSON("testdata/entitynames.json", map[string]any{"java": javaName, "bedrock": display, "eggs": eggs})
	fmt.Printf("bedrock entities: %d of %d named for Java\n", len(lines), len(display))
}

// bedrockVersion is the Bedrock version the server speaks (Dragonfly 0.11.6), whose
// minecraft-data files dataPaths names.
const bedrockVersion = "1.26.60"

// packetMappings finds the packet-name mapper of a protocol direction.
func packetMappings(dir map[string]any) map[string]any {
	types, _ := dir["types"].(map[string]any)
	pk, _ := types["packet"].([]any)
	if len(pk) < 2 {
		return nil
	}
	fields, _ := pk[1].([]any)
	for _, f := range fields {
		fm, _ := f.(map[string]any)
		if fm["name"] != "name" {
			continue
		}
		t, _ := fm["type"].([]any)
		if len(t) == 2 && t[0] == "mapper" {
			m, _ := t[1].(map[string]any)
			mm, _ := m["mappings"].(map[string]any)
			return mm
		}
	}
	return nil
}

func (g *gen) packets() {
	var proto map[string]any
	readJSON(filepath.Join(g.pc, "protocol.json"), &proto)
	ref := map[string]map[string]any{}
	var lines []string
	for _, st := range []string{"handshaking", "status", "login", "configuration", "play"} {
		for _, d := range []string{"toClient", "toServer"} {
			sm, _ := proto[st].(map[string]any)
			dm, _ := sm[d].(map[string]any)
			m := packetMappings(dm)
			ref[st+"/"+d] = m
			for hex, name := range m {
				id, err := strconv.ParseInt(strings.TrimPrefix(hex, "0x"), 16, 32)
				if err != nil {
					fatal(err)
				}
				lines = append(lines, fmt.Sprintf("%s\t%s\t%s\t%d", st, d, name, id))
			}
		}
	}
	sort.Strings(lines)
	g.writeLines("packets", lines)
	g.writeJSON("testdata/packets.json", ref)
	fmt.Printf("packets: %d\n", len(lines))
}

// components writes the item component types, by the SlotComponentType mapper of
// protocol.json: the varint each component is known by in an item slot.
func (g *gen) components() {
	var proto struct {
		Types map[string]json.RawMessage `json:"types"`
	}
	readJSON(filepath.Join(g.pc, "protocol.json"), &proto)
	var mapper []json.RawMessage
	if err := json.Unmarshal(proto.Types["SlotComponentType"], &mapper); err != nil || len(mapper) != 2 {
		fatal(fmt.Errorf("protocol.json: SlotComponentType is not a mapper"))
	}
	var m struct {
		Type     string            `json:"type"`
		Mappings map[string]string `json:"mappings"`
	}
	if err := json.Unmarshal(mapper[1], &m); err != nil || m.Type != "varint" {
		fatal(fmt.Errorf("protocol.json: SlotComponentType is not a varint mapper"))
	}
	var lines []string
	for id, name := range m.Mappings {
		lines = append(lines, name+"\t"+id)
	}
	sort.Strings(lines)
	g.writeLines("components", lines)
	fmt.Printf("item components: %d\n", len(lines))
}

// metadata writes each entity's metadata keys with their indexes (entities.json
// lists them in index order) and the metadata value types by ID, from
// protocol.json's entityMetadataEntry type mapper.
func (g *gen) metadata() {
	var es []struct {
		Name string   `json:"name"`
		Keys []string `json:"metadataKeys"`
	}
	readJSON(filepath.Join(g.pc, "entities.json"), &es)
	var lines []string
	for _, e := range es {
		for i, k := range e.Keys {
			lines = append(lines, "key\t"+e.Name+"\t"+k+"\t"+strconv.Itoa(i))
		}
	}
	var proto struct {
		Types map[string]json.RawMessage `json:"types"`
	}
	readJSON(filepath.Join(g.pc, "protocol.json"), &proto)
	var entry []json.RawMessage
	var fields []struct {
		Name string          `json:"name"`
		Type json.RawMessage `json:"type"`
	}
	if json.Unmarshal(proto.Types["entityMetadataEntry"], &entry) != nil || len(entry) != 2 || json.Unmarshal(entry[1], &fields) != nil {
		fatal(fmt.Errorf("protocol.json: entityMetadataEntry is not a container"))
	}
	types := 0
	for _, f := range fields {
		var mapper []json.RawMessage
		if f.Name != "type" || json.Unmarshal(f.Type, &mapper) != nil || len(mapper) != 2 {
			continue
		}
		var m struct {
			Type     string            `json:"type"`
			Mappings map[string]string `json:"mappings"`
		}
		if json.Unmarshal(mapper[1], &m) != nil || m.Type != "varint" {
			fatal(fmt.Errorf("protocol.json: entityMetadataEntry type is not a varint mapper"))
		}
		for id, name := range m.Mappings {
			lines = append(lines, "type\t"+name+"\t"+id)
			types++
		}
	}
	if types == 0 {
		fatal(fmt.Errorf("protocol.json: no entity metadata types"))
	}
	sort.Strings(lines)
	g.writeLines("metadata", lines)
	fmt.Printf("metadata: %d lines, %d types\n", len(lines), types)
}

func (g *gen) registries() {
	var login struct {
		Codec json.RawMessage `json:"dimensionCodec"`
	}
	readJSON(filepath.Join(g.pc, "loginPacket.json"), &login)
	// Key order matters, so walk the object by token.
	dec := json.NewDecoder(bytes.NewReader(login.Codec))
	dec.Token()
	var lines []string
	for dec.More() {
		k, _ := dec.Token()
		var reg struct {
			Entries []struct {
				Key string `json:"key"`
			} `json:"entries"`
		}
		if err := dec.Decode(&reg); err != nil {
			fatal(err)
		}
		if g.dynamic == nil {
			g.dynamic = map[string][]string{}
		}
		parts := []string{k.(string)}
		for _, e := range reg.Entries {
			parts = append(parts, e.Key)
		}
		lines = append(lines, strings.Join(parts, "\t"))
	}
	g.writeLines("registries", lines)
	fmt.Printf("registries: %d\n", len(lines))
}
