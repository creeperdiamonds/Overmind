package structures

import (
	"encoding/json"
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// codeBlockNames are the blocks the structure code names itself (ridNamed, mustState and the like
// outside the embedded tables), in Bedrock's names.
var codeBlockNames = strings.Fields(`sculk sculk_catalyst sculk_shrieker frame nether_brick_stairs nether_brick chest
	mob_spawner cobblestone stone_stairs normal_stone_stairs sandstone stone_bricks gravel suspicious_sand
	suspicious_gravel netherrack magma cracked_stone_bricks stone_brick_stairs mossy_stone_bricks
	mossy_stone_brick_stairs normal_stone_slab stone_brick_slab mossy_stone_brick_slab mossy_stone_brick_wall
	crying_obsidian polished_blackstone_brick_slab polished_blackstone chiseled_polished_blackstone
	polished_blackstone_bricks cracked_polished_blackstone_bricks polished_blackstone_brick_stairs
	polished_blackstone_brick_wall iron_chain cauldron obsidian lava water dirt`)

// Every block a structure can write breaks as a block for players (round 19): every state of every
// block named by a template of the install, by the embedded piece tables or by the code. A block with no
// Go type (Dragonfly's unknown block) cannot be broken at all.
func TestStructureBlocksBreak(t *testing.T) {
	_, d := installData(t)
	written := map[uint32]string{} // Runtime ID -> what writes it.
	for path := range d.templates {
		tp := d.Template(path)
		if tp == nil {
			continue
		}
		for _, st := range tp.palette {
			for rot := range 4 {
				if r := convert.state(st, rot); r.ok {
					written[r.rid] = path
				}
			}
		}
	}
	registry.load()
	fs.WalkDir(pieceFiles, ".", func(p string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return err
		}
		raw, _ := pieceFiles.ReadFile(p)
		var v any
		if json.Unmarshal(raw, &v) != nil {
			t.Errorf("%s does not decode", p)
			return nil
		}
		walkStrings(v, func(s string) {
			f := strings.FieldsFunc(s, func(r rune) bool { return r == '[' || r == ' ' })
			if len(f) == 0 {
				return
			}
			if _, ok := registry.byName[strings.TrimPrefix(f[0], "minecraft:")]; !ok {
				return
			}
			rid := specState(s)
			if rid == noBlock {
				rid = paletteState(s)
			}
			if rid != noBlock {
				written[rid] = p
			}
		})
		return nil
	})
	for _, n := range codeBlockNames {
		if _, ok := registry.byName[n]; !ok {
			t.Errorf("code block %s is not registered", n)
		}
		written[ridNamed(n, nil)] = "code"
	}
	// The end city's item frames, as endcity.go names them.
	for f := range int32(6) {
		written[ridNamed("frame", map[string]any{"facing_direction": f})] = "code"
	}
	rids := make([]uint32, 0, len(written))
	for rid := range written {
		rids = append(rids, rid)
	}
	sort.Slice(rids, func(i, j int) bool { return rids[i] < rids[j] })
	kinds := map[string]bool{}
	for _, rid := range rids {
		b, _ := world.BlockByRuntimeID(rid)
		n, props := b.EncodeBlock()
		kinds[n] = true
		if why := worldgen.CheckBlockBreaks(b); why != "" {
			t.Errorf("%s (written by %s): %s %v", n, written[rid], why, props)
		}
	}
	t.Logf("%d blocks, %d states checked", len(kinds), len(rids))
}

func walkStrings(v any, f func(string)) {
	switch x := v.(type) {
	case string:
		if x != "" {
			f(x)
		}
	case []any:
		for _, e := range x {
			walkStrings(e, f)
		}
	case map[string]any:
		for _, e := range x {
			walkStrings(e, f)
		}
	}
}
