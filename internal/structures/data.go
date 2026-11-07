package structures

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Data is what the structures read from a local Bedrock install (owner's decision, plan "Order after
// Task 5"): the structure templates, the loot tables and the data-driven jigsaw structures' JSON. The
// vanilla packs are read newest first and the newest copy of a file wins (N1 1.1, 1.2).
type Data struct {
	Dir       string
	templates map[string]string // Template path ("ancient_city/city_center/city_center_1") to its file.
	// mcstructures are the Bedrock-format templates (.mcstructure, the bastion's mobs) the packs hold, by
	// path; they are not parsed.
	mcstructures map[string]bool
	loot         map[string]*LootTable
	// jigsawFilters are the data-driven structures' biome filters (worldgen/structures/*.json).
	jigsawFilters map[string]json.RawMessage
	jigsaws       map[string]*jsonJigsaw // The data-driven jigsaw structures, by identifier.
	rawPools      map[string]json.RawMessage
	procLists     map[string][]brProcessor // br::worldgen processor lists, by identifier.
	jpoolsOnce    sync.Once
	jpools        map[string]*pool
	campOrder     []string // The abandoned_camp set's structures, in the set's order.

	parsed sync.Map // Template path to *Template or error, parsed on first use.
	// parsedBytes is templateBytes's estimate of the parsed templates' memory (see templateWarnBytes).
	parsedBytes atomic.Int64
	problemsMu  sync.Mutex
	missingSeen sync.Map // Template paths already reported missing (missingTemplate).
	loaded      bool     // LoadData has returned: later problems are logged as they happen.

	trialOnce sync.Once
	trial     map[string]*pool

	villageOnce sync.Once
	village     map[string]*pool

	ancientOnce sync.Once
	ancient     map[string]*pool

	bastionOnce sync.Once
	bastion     map[string]*pool
	// archives are the worldgen archive files read (noteArchive), for the fingerprint.
	archives []string
	// Problems lists files that could not be read; each is skipped.
	Problems []string
}

// LoadData reads an install's structure data. It fails only when the folder holds no vanilla pack.
func LoadData(dir string) (*Data, error) {
	packs, err := worldgen.VanillaPacks(dir)
	if err != nil {
		return nil, err
	}
	d := &Data{Dir: dir, templates: map[string]string{}, loot: map[string]*LootTable{}, jigsawFilters: map[string]json.RawMessage{},
		jigsaws: map[string]*jsonJigsaw{}, rawPools: map[string]json.RawMessage{}, procLists: map[string][]brProcessor{}}
	for _, root := range packs {
		d.indexTemplates(filepath.Join(root, "structures"))
		d.readLoot(root)
		d.readJigsaw(filepath.Join(root, "__brarchive", "worldgen"))
	}
	if len(d.templates) == 0 {
		return nil, fmt.Errorf("%s: the vanilla packs hold no structure templates", dir)
	}
	d.loaded = true
	return d, nil
}

// Fingerprint names the structure data read, so a world records when its structure data changes (the
// app's provenance record): a SHA-256 of every template's path and size, every loot table's path and
// pool count, every Bedrock-format template (.mcstructure) path, and every worldgen archive file read
// (jigsaw structures, template pools, processor lists, structure sets) with a hash of its text.
func (d *Data) Fingerprint() string {
	h := sha256.New()
	var keys []string
	for k := range d.templates {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		size := int64(-1)
		if st, err := os.Stat(d.templates[k]); err == nil {
			size = st.Size()
		}
		fmt.Fprintf(h, "t %s %d\n", k, size)
	}
	keys = keys[:0]
	for k := range d.loot {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "l %s %d\n", k, len(d.loot[k].Pools))
	}
	keys = keys[:0]
	for k := range d.mcstructures {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		fmt.Fprintf(h, "m %s\n", k)
	}
	for _, a := range slices.Sorted(slices.Values(d.archives)) {
		fmt.Fprintf(h, "a %s\n", a)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// noteArchive records each file of a worldgen archive read, for the fingerprint: its path relative to
// the install, its name and a hash of its text.
func (d *Data) noteArchive(path string, files [][2]string) {
	rel, err := filepath.Rel(d.Dir, path)
	if err != nil {
		rel = path
	}
	for _, f := range files {
		sum := sha256.Sum256([]byte(f[1]))
		d.archives = append(d.archives, filepath.ToSlash(rel)+" "+f[0]+" "+hex.EncodeToString(sum[:8]))
	}
}

// problem notes a file that could not be used. After loading (a template parsed on a chunk worker) it
// is also logged, once, as it happens.
func (d *Data) problem(format string, a ...any) {
	msg := fmt.Sprintf(format, a...)
	d.problemsMu.Lock()
	d.Problems = append(d.Problems, msg)
	d.problemsMu.Unlock()
	if d.loaded {
		slog.Default().Warn("A structure data file is skipped", "problem", msg)
	}
}

// missingTemplate notes a template a structure needs and the install lacks (or cannot parse), once per
// path: with a partial install, every ocean ruin or mansion piece met again adds no log line and no
// problem.
func (d *Data) missingTemplate(what, path string) {
	if _, seen := d.missingSeen.LoadOrStore(path, true); !seen {
		d.problem("%s template %s is missing", what, path)
	}
}

// Parsed templates are kept for the server's life. The structures' caches keep the templates of the
// starts they hold (mansion and end city pieces, ocean ruins, the pools' elements), so parsing a template
// again for each use would keep one copy per cached start instead: measured, 64 cached mansions took 379
// MB when their templates were parsed per use. What is kept is the set the world's structures use, at
// most the install's whole set: BDS 1.26.60's 1,285 templates take 54 MB of heap (TestTemplateMemory),
// 43 MB of them the pools' (TestStructureMemory; NOTES.md). parsedBytes counts them by templateBytes's
// estimate, and past templateWarnBytes, above vanilla's whole set, a warning is logged once.
const templateWarnBytes = 64 << 20

// templateBytes estimates a parsed template's heap: the Template, its block, palette and jigsaw slices,
// the palette's names and property maps, and each block's NBT (maps, lists, strings and boxed numbers, as
// the NBT reader makes them). TestTemplateMemory holds it near the heap the install's templates take.
func templateBytes(t *Template) int64 {
	n := allocBytes(int(unsafe.Sizeof(Template{}))) + allocBytes(cap(t.blocks)*int(unsafe.Sizeof(tBlock{}))) +
		allocBytes(cap(t.palette)*int(unsafe.Sizeof(tState{}))) + allocBytes(cap(t.jigsaws)*int(unsafe.Sizeof(jigsaw{})))
	for _, p := range t.palette {
		n += allocBytes(len(p.name)+len("minecraft:")) + mapBytes(len(p.props)) // The name is a slice of the file's.
		for k, v := range p.props {
			n += allocBytes(len(k)) + allocBytes(len(v))
		}
	}
	for _, b := range t.blocks {
		if b.nbt != nil {
			n += nbtBytes(b.nbt)
		}
	}
	return n
}

// allocBytes is the heap an allocation of n bytes takes, rounded up as Go's size classes roughly do.
func allocBytes(n int) int64 {
	switch {
	case n <= 0:
		return 0
	case n <= 16:
		return 16
	case n <= 128:
		return int64(n+15) &^ 15
	case n <= 1024:
		return int64(n+63) &^ 63
	case n <= 32<<10:
		return int64(n+511) &^ 511
	}
	return int64(n+8191) &^ 8191
}

// mapBytes is the heap of a map[string]T (T a string or an interface) holding k entries: its header and,
// once it has entries, its groups of 8 slots of 32 bytes and 8 control bytes, kept 7/8 full at most.
func mapBytes(k int) int64 {
	const header, group = 48, 8 + 8*32
	if k == 0 {
		return header
	}
	if k <= 8 {
		return header + allocBytes(group)
	}
	groups := 1
	for groups*7 < k {
		groups *= 2
	}
	return header + 64 + allocBytes(groups*group)
}

// nbtBytes is the heap of an NBT value as the reader makes it, the interface's boxing included.
func nbtBytes(v any) int64 {
	switch x := v.(type) {
	case map[string]any:
		n := mapBytes(len(x))
		for k, e := range x {
			n += allocBytes(len(k)) + nbtBytes(e)
		}
		return n
	case []any:
		n := allocBytes(24) + allocBytes(16*cap(x))
		for _, e := range x {
			n += nbtBytes(e)
		}
		return n
	case string:
		return allocBytes(16) + allocBytes(len(x))
	case byte:
		return 0 // Go keeps single bytes in an interface without allocating.
	case int16, int32, float32, int64, float64:
		return 8
	case []byte:
		return allocBytes(24) + allocBytes(len(x))
	case []int32:
		return allocBytes(24) + allocBytes(4*len(x))
	case []int64:
		return allocBytes(24) + allocBytes(8*len(x))
	}
	return 16
}

// indexTemplates notes each .nbt file under a pack's structures folder, unless a newer pack has it.
func (d *Data) indexTemplates(dir string) {
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err == nil && !e.IsDir() && strings.HasSuffix(p, ".mcstructure") {
			rel, _ := filepath.Rel(dir, p)
			if d.mcstructures == nil {
				d.mcstructures = map[string]bool{}
			}
			d.mcstructures[strings.TrimSuffix(filepath.ToSlash(rel), ".mcstructure")] = true
			return nil
		}
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".nbt") {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		key := strings.TrimSuffix(filepath.ToSlash(rel), ".nbt")
		if _, ok := d.templates[key]; !ok {
			d.templates[key] = p
		}
		return nil
	})
}

// Template is a template by path, parsed on first use; nil if the install has none or it is broken.
func (d *Data) Template(path string) *Template {
	if v, ok := d.parsed.Load(path); ok {
		t, _ := v.(*Template)
		return t
	}
	file, ok := d.templates[path]
	if !ok {
		d.parsed.Store(path, errors.New("missing"))
		return nil
	}
	t, err := readTemplate(path, file)
	if err != nil {
		if _, seen := d.parsed.LoadOrStore(path, err); !seen {
			d.problem("%v", err)
		}
		return nil
	}
	if v, loaded := d.parsed.LoadOrStore(path, t); loaded {
		return v.(*Template)
	}
	n := templateBytes(t)
	if total := d.parsedBytes.Add(n); total > templateWarnBytes && total-n <= templateWarnBytes {
		slog.Default().Warn("Structure templates take more memory than a vanilla install's", "megabytes", total>>20)
	}
	return t
}

// readLoot reads a pack's loose loot tables, keyed as chests name them ("loot_tables/chests/x.json").
func (d *Data) readLoot(root string) {
	base := filepath.Join(root, "loot_tables")
	filepath.WalkDir(base, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".json") {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		key := filepath.ToSlash(rel)
		if _, ok := d.loot[key]; ok {
			return nil
		}
		raw, err := os.ReadFile(p)
		if err != nil {
			d.problem("%s: %v", p, err)
			return nil
		}
		t, err := parseLootTable(worldgen.JSONText(string(raw)))
		if err != nil {
			d.problem("%s: %v", p, err)
			return nil
		}
		d.loot[key] = t
		return nil
	})
}

// readJigsaw reads the data-driven jigsaw structures and sets (N1 2.4) from a pack's worldgen archives.
func (d *Data) readJigsaw(dir string) {
	files, err := worldgen.ReadBrarchive(filepath.Join(dir, "structures.brarchive"))
	if err != nil {
		d.problem("%v", err)
	}
	d.noteArchive(filepath.Join(dir, "structures.brarchive"), files)
	for _, f := range files {
		var j struct {
			J struct {
				Description struct {
					Identifier string `json:"identifier"`
				} `json:"description"`
				BiomeFilters json.RawMessage `json:"biome_filters"`
				StartPool    string          `json:"start_pool"`
				StartJigsaw  string          `json:"start_jigsaw_name"`
				MaxDepth     int             `json:"max_depth"`
				MaxDistance  struct {
					Horizontal, Vertical int
				} `json:"max_distance_from_center"`
				StartHeight struct {
					Type  string `json:"type"`
					Value struct {
						Absolute *int `json:"absolute"`
					} `json:"value"`
				} `json:"start_height"`
				Projection string          `json:"heightmap_projection"`
				Adaptation string          `json:"terrain_adaptation"`
				Step       string          `json:"step"`
				Padding    json.RawMessage `json:"dimension_padding"`
			} `json:"minecraft:jigsaw"`
		}
		if err := json.Unmarshal(worldgen.JSONText(f[1]), &j); err != nil || j.J.Description.Identifier == "" {
			continue
		}
		id := j.J.Description.Identifier
		if _, ok := d.jigsawFilters[id]; ok {
			continue // Newest pack first.
		}
		d.jigsawFilters[id] = j.J.BiomeFilters
		if j.J.StartHeight.Type != "constant" || j.J.StartHeight.Value.Absolute == nil || j.J.MaxDepth < 0 || j.J.MaxDepth > 20 {
			d.problem("jigsaw structure %s: start height or depth not read (only constant absolute heights are)", id)
			continue
		}
		js := &jsonJigsaw{id: id, startPool: j.J.StartPool, startJigsaw: j.J.StartJigsaw, maxDepth: j.J.MaxDepth,
			maxH: min(max(j.J.MaxDistance.Horizontal, 1), 128), maxV: min(max(j.J.MaxDistance.Vertical, 1), 384),
			startY: *j.J.StartHeight.Value.Absolute, biomeFilters: j.J.BiomeFilters, adaptation: j.J.Adaptation,
			step: stepIndex(j.J.Step)}
		if j.J.MaxDistance.Vertical == 0 {
			js.maxV = 384
		}
		if j.J.Projection != "" && j.J.Projection != "none" {
			js.projection = j.J.Projection
		}
		var pad int
		if json.Unmarshal(j.J.Padding, &pad) == nil {
			js.padBottom, js.padTop = pad, pad
		} else {
			var pp struct{ Bottom, Top int }
			if json.Unmarshal(j.J.Padding, &pp) == nil {
				js.padBottom, js.padTop = pp.Bottom, pp.Top
			}
		}
		d.jigsaws[id] = js
	}
	d.readPools(dir)
	procs, err := worldgen.ReadBrarchive(filepath.Join(dir, "processors.brarchive"))
	if err != nil {
		d.problem("%v", err)
	}
	d.noteArchive(filepath.Join(dir, "processors.brarchive"), procs)
	for _, f := range procs {
		var pl struct {
			L struct {
				Description struct {
					Identifier string `json:"identifier"`
				} `json:"description"`
			} `json:"minecraft:processor_list"`
		}
		body := worldgen.JSONText(f[1])
		if json.Unmarshal(body, &pl) != nil || pl.L.Description.Identifier == "" {
			continue
		}
		if _, ok := d.procLists[pl.L.Description.Identifier]; ok {
			continue
		}
		if ps, ok := parseProcessors(body); ok {
			d.procLists[pl.L.Description.Identifier] = ps
		} else {
			d.problem("processor list %s: a processor or predicate type not read", pl.L.Description.Identifier)
		}
	}
	sets, err := worldgen.ReadBrarchive(filepath.Join(dir, "structure_sets.brarchive"))
	if err != nil {
		d.problem("%v", err)
	}
	d.noteArchive(filepath.Join(dir, "structure_sets.brarchive"), sets)
	for _, f := range sets {
		var s struct {
			S struct {
				Description struct {
					Identifier string `json:"identifier"`
				} `json:"description"`
				Structures []struct {
					Structure string `json:"structure"`
				} `json:"structures"`
			} `json:"minecraft:structure_set"`
		}
		if json.Unmarshal(worldgen.JSONText(f[1]), &s) != nil || s.S.Description.Identifier != "minecraft:abandoned_camp" || d.campOrder != nil {
			continue
		}
		for _, e := range s.S.Structures {
			d.campOrder = append(d.campOrder, e.Structure)
		}
	}
}

// Summary is one line on what was read, for the server log.
func (d *Data) Summary() string {
	return fmt.Sprintf("%d templates, %d loot tables, %d jigsaw structures, %d problems", len(d.templates), len(d.loot), len(d.jigsawFilters), len(d.Problems))
}

// readPools reads a pack's data-driven template pools (worldgen/template_pools*.brarchive, every level),
// keeping the newest pack's copy of each.
func (d *Data) readPools(dir string) {
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() || !strings.HasSuffix(p, ".brarchive") || !strings.Contains(filepath.ToSlash(p), "template_pools") {
			return nil
		}
		files, err := worldgen.ReadBrarchive(p)
		if err != nil {
			d.problem("%v", err)
			return nil
		}
		d.noteArchive(p, files)
		for _, f := range files {
			var tp struct {
				P struct {
					Description struct {
						Identifier string `json:"identifier"`
					} `json:"description"`
				} `json:"minecraft:template_pool"`
			}
			body := worldgen.JSONText(f[1])
			if json.Unmarshal(body, &tp) != nil || tp.P.Description.Identifier == "" {
				continue
			}
			if _, ok := d.rawPools[tp.P.Description.Identifier]; !ok {
				d.rawPools[tp.P.Description.Identifier] = body
			}
		}
		return nil
	})
}

// maxPoolElements bounds a pool's expanded element list (weights times elements) from the install.
const maxPoolElements = 4096

// dataPools builds the data-driven pools once: elements expanded by weight in order (Java's
// StructureTemplatePool), templates from the install, feature elements as one-block pieces, a pool
// without a fallback falling back to the empty pool.
func (d *Data) dataPools() map[string]*pool {
	d.jpoolsOnce.Do(func() {
		d.jpools = map[string]*pool{"minecraft:empty": {}}
		for id, body := range d.rawPools {
			var tp struct {
				P struct {
					Fallback string `json:"fallback"`
					Elements []struct {
						Element struct {
							Type       string          `json:"element_type"`
							Location   string          `json:"location"`
							Projection string          `json:"projection"`
							Feature    string          `json:"feature"`
							Processors json.RawMessage `json:"processors"`
						} `json:"element"`
						Weight int `json:"weight"`
					} `json:"elements"`
				} `json:"minecraft:template_pool"`
			}
			if err := json.Unmarshal(body, &tp); err != nil {
				d.problem("template pool %s: %v", id, err)
				continue
			}
			pl := &pool{fallback: tp.P.Fallback}
			if pl.fallback == "" {
				pl.fallback = "minecraft:empty"
			}
			for _, e := range tp.P.Elements {
				var el *element
				switch strings.TrimPrefix(e.Element.Type, "minecraft:") {
				case "empty_pool_element":
				case "feature_pool_element":
					el = &element{name: e.Element.Feature}
				case "single_pool_element", "legacy_single_pool_element":
					t := d.Template(strings.TrimPrefix(e.Element.Location, "minecraft:"))
					if t == nil {
						// Dropped with its weight slots, unlike the code pools' missing elements (village,
						// trial chambers, bastion), which keep theirs as BDS does (P16.3, P18.7): no data pool
						// element is missing in the vanilla install, and BDS's rule for one was not read.
						d.problem("template pool %s: no template %s", id, e.Element.Location)
						continue
					}
					el = &element{name: e.Element.Location, template: t, terrain: e.Element.Projection == "terrain_matching",
						procs:  d.procLists[stringOf(e.Element.Processors)],
						legacy: strings.HasPrefix(e.Element.Type, "minecraft:legacy") || strings.HasPrefix(e.Element.Type, "legacy")}
				default:
					d.problem("template pool %s: element type %s not read", id, e.Element.Type)
					continue
				}
				for range min(max(e.Weight, 0), 150) {
					if len(pl.elems) < maxPoolElements {
						pl.elems = append(pl.elems, el)
					}
				}
			}
			d.jpools[id] = pl
		}
	})
	return d.jpools
}

// stringOf is a JSON string's value, "" for anything else (an inline processor list is not read).
func stringOf(raw json.RawMessage) string {
	var v string
	json.Unmarshal(raw, &v)
	return v
}
