package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// dimensions writes each dimension type's height and sky light, from
// minecraft-data's loginPacket.json (the dimension_type registry a 26.1 server
// sent), with its sea level. minecraft-data's captured world is flat, so its
// sea level is not the dimension's. No open source gives a dimension's own sea
// level, so the three are written by hand from minecraft.wiki's "Terrain
// features" (oldid=3873202, where "Sea level" oldid=3569134 redirects):
//   - Overworld 63: "Air on altitude Y=63 and lower is replaced by water".
//   - Nether 31, written by hand: "a lava sea forms below Y=31", the page's one
//     figure for the Nether, taken as its sea level as 63 is the Overworld's.
//   - End 0, written by hand: the End has no sea of water or lava (islands in
//     the void), so its lowest Y (min_y 0), with nothing below sea level.
func (g *gen) dimensions() {
	var login struct {
		Codec map[string]struct {
			Entries []struct {
				Key   string `json:"key"`
				Value struct {
					Value map[string]struct {
						Value any `json:"value"`
					} `json:"value"`
				} `json:"value"`
			} `json:"entries"`
		} `json:"dimensionCodec"`
	}
	readJSON(filepath.Join(g.pc, "loginPacket.json"), &login)
	sea := map[string]int{"minecraft:overworld": 63, "minecraft:the_nether": 31, "minecraft:the_end": 0}
	var lines []string
	for _, e := range login.Codec["minecraft:dimension_type"].Entries {
		s, ok := sea[e.Key]
		if !ok {
			continue
		}
		v := e.Value.Value
		num := func(k string) int {
			f, ok := v[k].Value.(float64)
			if !ok {
				fatal(fmt.Errorf("%s: no %s", e.Key, k))
			}
			return int(f)
		}
		lines = append(lines, fmt.Sprintf("%s\t%d\t%d\t%d\t%d", e.Key, num("min_y"), num("height"), num("has_skylight"), s))
	}
	if len(lines) != len(sea) {
		fatal(fmt.Errorf("dimension types: %d of %d found", len(lines), len(sea)))
	}
	sort.Strings(lines)
	g.writeLines("dimensions", lines)
	fmt.Printf("dimensions: %d\n", len(lines))
}

// azaleaEnum reads one registry enum of azalea-registry's builtin.rs: its
// variants in order, by Rust name, with their resource names. azalea's
// registry! macro numbers entries in that order, which is the game's.
func azaleaEnum(src, enum string) (names []string, byVariant map[string]string) {
	start := strings.Index(src, "enum "+enum+" {")
	if start < 0 {
		fatal(fmt.Errorf("azalea: no enum %s", enum))
	}
	body := src[start:]
	body = body[:strings.Index(body, "\n}")]
	byVariant = map[string]string{}
	for _, m := range regexp.MustCompile(`(?m)^\s*(\w+) => "([^"]+)",`).FindAllStringSubmatch(body, -1) {
		names = append(names, m[2])
		byVariant[m[1]] = m[2]
	}
	return names, byVariant
}

// azalea writes the static registry IDs minecraft-data does not list (menus,
// block entity types) and the tags, from azalea-rs/azalea's azalea-registry
// (MIT) at its 26.1 commit. azalea has tags for blocks, items, fluids and
// entity types; for the other registries (damage types, enchantments, biomes
// and the rest) there is no open source, and no tags are sent for them.
func (g *gen) azalea(dir, commit string) {
	raw, err := os.ReadFile(filepath.Join(dir, "builtin.rs"))
	if err != nil {
		fatal(err)
	}
	src := strings.ReplaceAll(string(raw), "\r\n", "\n")
	header := "# source: azalea-rs/azalea " + commit + " (azalea-registry/src), MIT; names and IDs only"
	var static []string
	for _, r := range []struct{ enum, registry string }{{"MenuKind", "minecraft:menu"}, {"BlockEntityKind", "minecraft:block_entity_type"}} {
		names, _ := azaleaEnum(src, r.enum)
		for i, n := range names {
			static = append(static, r.registry+"\tminecraft:"+n+"\t"+strconv.Itoa(i))
		}
	}
	sort.Strings(static)
	g.header = header
	g.writeLines("static", static)

	// The IDs tags carry: minecraft-data's for blocks, items and entity types,
	// azalea's enum order for fluids (minecraft-data has no fluid list).
	ids := map[string]map[string]int{"block": {}, "item": {}, "entity_type": {}, "fluid": {}}
	var bl []block
	readJSON(filepath.Join(g.pc, "blocks.json"), &bl)
	var blockIDs []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	readJSON(filepath.Join(g.pc, "blocks.json"), &blockIDs)
	for _, b := range blockIDs {
		ids["block"][b.Name] = b.ID
	}
	var its []item
	readJSON(filepath.Join(g.pc, "items.json"), &its)
	for _, it := range its {
		ids["item"][it.Name] = int(it.ID)
	}
	var es []item
	readJSON(filepath.Join(g.pc, "entities.json"), &es)
	for _, e := range es {
		ids["entity_type"][e.Name] = int(e.ID)
	}
	fluids, _ := azaleaEnum(src, "Fluid")
	for i, f := range fluids {
		ids["fluid"][f] = i
	}
	var tags []string
	for _, t := range []struct{ file, registry, enum string }{
		{"blocks.rs", "block", "BlockKind"}, {"items.rs", "item", "ItemKind"}, {"fluids.rs", "fluid", "Fluid"}, {"entities.rs", "entity_type", "EntityKind"},
	} {
		_, variants := azaleaEnum(src, t.enum)
		rawTags, err := os.ReadFile(filepath.Join(dir, "tags", t.file))
		if err != nil {
			fatal(err)
		}
		text := strings.ReplaceAll(string(rawTags), "\r\n", "\n")
		decl := regexp.MustCompile(`pub static (\w+): LazyLock<RegistryTag<\w+>>`)
		locs := decl.FindAllStringSubmatchIndex(text, -1)
		member := regexp.MustCompile(t.enum + `::(\w+)`)
		for i, l := range locs {
			end := len(text)
			if i+1 < len(locs) {
				end = locs[i+1][0]
			}
			name := azaleaTagName(text[l[2]:l[3]])
			var nums []int
			for _, m := range member.FindAllStringSubmatch(text[l[1]:end], -1) {
				n, ok := variants[m[1]]
				if !ok {
					fatal(fmt.Errorf("azalea: no %s::%s", t.enum, m[1]))
				}
				id, ok := ids[t.registry][n]
				if !ok {
					fatal(fmt.Errorf("azalea: %s %s has no minecraft-data ID", t.registry, n))
				}
				nums = append(nums, id)
			}
			sort.Ints(nums)
			nums = slices.Compact(nums) // azalea lists some members twice
			strs := make([]string, len(nums))
			for k, n := range nums {
				strs[k] = strconv.Itoa(n)
			}
			tags = append(tags, "minecraft:"+t.registry+"\tminecraft:"+name+"\t"+strings.Join(strs, " "))
		}
	}
	g.writeLines("tags", tags)
	fmt.Printf("azalea: %d static IDs, %d tags\n", len(static), len(tags))
}

// azaleaTagPrefixes are the tag folders whose slash azalea's Rust names turn
// into an underscore ("mineable/axe" is MINEABLE_AXE).
var azaleaTagPrefixes = []string{"mineable_", "enchantable_"}

func azaleaTagName(rust string) string {
	n := strings.ToLower(rust)
	for _, p := range azaleaTagPrefixes {
		if strings.HasPrefix(n, p) {
			return strings.TrimSuffix(p, "_") + "/" + n[len(p):]
		}
	}
	return n
}
