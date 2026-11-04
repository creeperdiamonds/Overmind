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

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Data is what the structures read from a local Bedrock install (owner's decision, plan "Order after
// Task 5"): the structure templates, the loot tables and the data-driven jigsaw structures' JSON. The
// vanilla packs are read newest first and the newest copy of a file wins (N1 1.1, 1.2).
type Data struct {
	Dir       string
	templates map[string]string // Template path ("ancient_city/city_center/city_center_1") to its file.
	loot      map[string]*LootTable
	// jigsawFilters are the data-driven structures' biome filters (worldgen/structures/*.json).
	jigsawFilters map[string]json.RawMessage
	campOrder     []string // The abandoned_camp set's structures, in the set's order.

	parsed sync.Map // Template path to *Template or error, parsed on first use.
	// parsedBytes is an estimate of the parsed templates' memory; past maxParsedBytes a template is
	// parsed for each use instead of kept (TestTemplateMemory measures the ancient city's).
	parsedBytes atomic.Int64
	problemsMu  sync.Mutex
	loaded      bool // LoadData has returned: later problems are logged as they happen.

	ancientOnce sync.Once
	ancient     map[string]*pool
	// Problems lists files that could not be read; each is skipped.
	Problems []string
}

// LoadData reads an install's structure data. It fails only when the folder holds no vanilla pack.
func LoadData(dir string) (*Data, error) {
	packs, err := worldgen.PacksDir(dir)
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(packs)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() && worldgen.IsVanillaPack(e.Name()) {
			names = append(names, e.Name())
		}
	}
	slices.SortFunc(names, func(a, b string) int { return slices.Compare(worldgen.PackVersion(b), worldgen.PackVersion(a)) })
	d := &Data{Dir: dir, templates: map[string]string{}, loot: map[string]*LootTable{}, jigsawFilters: map[string]json.RawMessage{}}
	for _, pack := range names {
		root := filepath.Join(packs, pack)
		d.indexTemplates(filepath.Join(root, "structures"))
		d.readLoot(root)
		d.readJigsaw(filepath.Join(root, "__brarchive", "worldgen"))
	}
	if len(d.templates) == 0 {
		return nil, fmt.Errorf("%s: the vanilla packs hold no structure templates", packs)
	}
	d.loaded = true
	return d, nil
}

// Fingerprint names the structure data read: a SHA-256 of every template's and loot table's path and
// size, so a world records when its structure data changes (the app's provenance record).
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
	return hex.EncodeToString(h.Sum(nil))
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

// maxParsedBytes bounds the memory kept by parsed templates, outside the generator's cache.
const maxParsedBytes = 32 << 20

// templateBytes estimates a parsed template's memory.
func templateBytes(t *Template) int64 {
	n := int64(len(t.blocks))*64 + int64(len(t.palette))*96 + int64(len(t.jigsaws))*128
	for _, b := range t.blocks {
		if b.nbt != nil {
			n += 256
		}
	}
	return n
}

// indexTemplates notes each .nbt file under a pack's structures folder, unless a newer pack has it.
func (d *Data) indexTemplates(dir string) {
	filepath.WalkDir(dir, func(p string, e fs.DirEntry, err error) error {
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
	if n := templateBytes(t); d.parsedBytes.Load()+n > maxParsedBytes {
		return t // Over the bound: used, not kept.
	} else if v, loaded := d.parsed.LoadOrStore(path, t); loaded {
		t, _ = v.(*Template)
	} else {
		d.parsedBytes.Add(n)
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
	for _, f := range files {
		var j struct {
			J struct {
				Description struct {
					Identifier string `json:"identifier"`
				} `json:"description"`
				BiomeFilters json.RawMessage `json:"biome_filters"`
			} `json:"minecraft:jigsaw"`
		}
		if err := json.Unmarshal(worldgen.JSONText(f[1]), &j); err != nil || j.J.Description.Identifier == "" {
			continue
		}
		if _, ok := d.jigsawFilters[j.J.Description.Identifier]; !ok {
			d.jigsawFilters[j.J.Description.Identifier] = j.J.BiomeFilters
		}
	}
	sets, err := worldgen.ReadBrarchive(filepath.Join(dir, "structure_sets.brarchive"))
	if err != nil {
		d.problem("%v", err)
	}
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
