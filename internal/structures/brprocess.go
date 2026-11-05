package structures

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/df-mc/dragonfly/server/block/model"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// br::worldgen's processors of the data-driven jigsaws (the trail ruins' archaeology lists, JSON in the
// packs' worldgen/processors), which are Java's, bit for bit (NOTES-structures-phase2 P9.3, cited BRP;
// our own Go of Java 26.1's RuleProcessor and CappedProcessor, from the study's sources):
//   - rule: per block, a fresh Java LCG seeded with Mth.getSeed of its world position; the rules in
//     order, the first whose input predicate passes wins (random_block_match draws nextFloat() < p when
//     the block matches; block_match draws nothing);
//   - capped: after every block has been through the list, the delegate runs on the piece's blocks in a
//     shuffled order (LCG(levelSeed).forkPositional().at(piece position)), until limit of them changed.
// Location and position predicates: the packs' lists have none (always true).

type brRule struct {
	match  string  // Input block, Bedrock name.
	p      float32 // random_block_match probability; < 0 for block_match.
	output string  // Bedrock name.
}

type brProcessor struct {
	rules  []brRule // A rule processor, or a capped one's delegate.
	capped int      // The capped limit; 0 for a rule processor.
}

// getSeed is Java's Mth.getSeed(x, y, z).
func getSeed(x, y, z int) int64 {
	s := int64(int32(x)*3129871) ^ int64(z)*116129781 ^ int64(y)
	s = s*s*42317861 + s*11
	return s >> 16
}

// runRules is RuleProcessor.processBlock for one block at a world position.
func runRules(rules []brRule, name string, x, y, z int) string {
	r := worldgen.NewJavaRandom(getSeed(x, y, z))
	for _, ru := range rules {
		if name != ru.match {
			continue
		}
		if ru.p >= 0 && !(r.NextFloat() < ru.p) {
			continue
		}
		return ru.output
	}
	return name
}

// processPiece runs an element's processors over a piece's blocks (Bedrock names, in Java's template
// order, which for these rules is the order given), turning names in place.
func (s *Structures) processPiece(p *piece, names []string, world [][3]int) {
	for _, pr := range p.elem.procs {
		if pr.capped == 0 {
			for i := range names {
				names[i] = runRules(pr.rules, names[i], world[i][0], world[i][1], world[i][2])
			}
		}
	}
	for _, pr := range p.elem.procs {
		if pr.capped == 0 || len(names) == 0 {
			continue
		}
		f := worldgen.NewJavaRandom(s.seed)
		r := worldgen.NewJavaRandom(getSeed(p.pos[0], p.pos[1], p.pos[2]) ^ f.NextLong())
		idx := make([]int, len(names))
		for i := range idx {
			idx[i] = i
		}
		javaShuffle(r, idx)
		changed := 0
		for _, i := range idx {
			if changed >= min(pr.capped, len(names)) {
				break
			}
			if n := runRules(pr.rules, names[i], world[i][0], world[i][1], world[i][2]); n != names[i] {
				names[i] = n
				changed++
			}
		}
	}
}

// parseProcessors reads a processor list's JSON.
func parseProcessors(body []byte) ([]brProcessor, bool) {
	var pl struct {
		L struct {
			Processors []json.RawMessage `json:"processors"`
		} `json:"minecraft:processor_list"`
	}
	if json.Unmarshal(body, &pl) != nil {
		return nil, false
	}
	var out []brProcessor
	for _, raw := range pl.L.Processors {
		var p struct {
			Type     string          `json:"processor_type"`
			Limit    int             `json:"limit"`
			Delegate json.RawMessage `json:"delegate"`
			Rules    json.RawMessage `json:"rules"`
		}
		if json.Unmarshal(raw, &p) != nil {
			return nil, false
		}
		switch strings.TrimPrefix(p.Type, "minecraft:") {
		case "rule":
			rs, ok := parseBrRules(p.Rules)
			if !ok {
				return nil, false
			}
			out = append(out, brProcessor{rules: rs})
		case "capped":
			var d struct {
				Rules json.RawMessage `json:"rules"`
			}
			json.Unmarshal(p.Delegate, &d)
			rs, ok := parseBrRules(d.Rules)
			if !ok || p.Limit < 1 {
				return nil, false
			}
			out = append(out, brProcessor{rules: rs, capped: min(p.Limit, 4096)})
		default:
			return nil, false
		}
	}
	return out, true
}

func parseBrRules(raw json.RawMessage) ([]brRule, bool) {
	var rs []struct {
		In struct {
			Type  string  `json:"predicate_type"`
			Block string  `json:"block"`
			P     float32 `json:"probability"`
		} `json:"input_predicate"`
		Loc *json.RawMessage `json:"location_predicate"`
		Pos *json.RawMessage `json:"position_predicate"`
		Out struct {
			Name   string            `json:"name"`
			States map[string]string `json:"states"`
		} `json:"output_state"`
	}
	if json.Unmarshal(raw, &rs) != nil {
		return nil, false
	}
	var out []brRule
	for _, r := range rs {
		if r.Loc != nil || r.Pos != nil {
			return nil, false // Not in the packs' lists; not written.
		}
		ru := brRule{match: strings.TrimPrefix(r.In.Block, "minecraft:"), p: -1, output: strings.TrimPrefix(r.Out.Name, "minecraft:")}
		switch strings.TrimPrefix(r.In.Type, "minecraft:") {
		case "random_block_match":
			ru.p = r.In.P
		case "block_match":
		default:
			return nil, false
		}
		if ru.output == "dirt" && r.Out.States["dirt_type"] == "coarse" {
			ru.output = "coarse_dirt" // The legacy dirt_type state, Bedrock 1.26's own block.
		}
		out = append(out, ru)
	}
	return out, true
}

// processedNames runs a data-driven piece's processors over all its template blocks, as Java does before
// placing any (StructureTemplate.processBlockInfos): Java's block order (blocks without NBT whose shape
// is a full cube, then the other blocks without NBT, then those with NBT, each sorted by y, x, z), with
// structure blocks left out. It gives each template block's Bedrock name (by file index), nil without
// processors.
func (s *Structures) processedNames(p *piece) []string {
	if len(p.elem.procs) == 0 {
		return nil
	}
	t := p.elem.template
	type ent struct {
		i     int
		group int
		pos   [3]int
	}
	var ents []ent
	names := make([]string, len(t.blocks))
	for i, b := range t.blocks {
		st := t.palette[b.state]
		if st.name == "structure_block" {
			continue
		}
		if st.name == "jigsaw" {
			fs, _ := b.nbt["final_state"].(string)
			st = parseJavaState(fs)
		}
		res := convert.state(st, p.rot)
		names[i] = res.name
		g := 1
		switch {
		case b.nbt != nil:
			g = 2
		case res.ok && fullCube(res.rid):
			g = 0
		}
		ents = append(ents, ent{i, g, b.pos})
	}
	slices.SortStableFunc(ents, func(a, b ent) int {
		if a.group != b.group {
			return a.group - b.group
		}
		for _, k := range [3]int{1, 0, 2} {
			if a.pos[k] != b.pos[k] {
				return a.pos[k] - b.pos[k]
			}
		}
		return 0
	})
	list := make([]string, len(ents))
	world := make([][3]int, len(ents))
	for k, e := range ents {
		q := rotate(e.pos, p.rot)
		list[k] = names[e.i]
		world[k] = [3]int{p.pos[0] + q[0], p.pos[1] + q[1], p.pos[2] + q[2]}
	}
	s.processPiece(p, list, world)
	for k, e := range ents {
		names[e.i] = list[k]
	}
	return names
}

// fullCube approximates Java's "collision shape is a full block" with Dragonfly's solid model.
func fullCube(rid uint32) bool {
	b, ok := world.BlockByRuntimeID(rid)
	if !ok {
		return false
	}
	_, solid := b.Model().(model.Solid)
	return solid
}
