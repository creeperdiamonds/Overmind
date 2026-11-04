package structures

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Loot. A generated chest stores its table's path and a loot seed; the table is rolled when the chest is
// first opened (N2 2.1, 2.2, owner's decision). Filling the container follows LootTable::fill as read
// (N2 2.3). How a table makes its items (getRandomItems, N2 2.5) was decompiled but not decoded: the
// pools, rolls, weighted pick and item functions here have Java's LootPool shape, the shape the notes
// expect, and are not confirmed against Bedrock. Functions with draws we do not know (enchanting, maps,
// potions, dyes ...) are left off the item. See NOTES.md.

// LootTable is a parsed loot table.
type LootTable struct {
	Pools []lootPool
}

type lootPool struct {
	rolls      intRange
	bonusRolls float64
	entries    []lootEntry
	conds      []lootCond
}

type lootEntry struct {
	kind      string // item, loot_table, empty
	name      string
	weight    int
	functions []lootFunc
	conds     []lootCond
}

type lootFunc struct {
	fn    string
	count intRange // set_count
	data  intRange // set_data
	dmg   [2]float64
	set   bool // count/data/damage given.
}

type lootCond struct {
	kind   string
	chance float64
}

// intRange is a number or a {min, max} range.
type intRange struct{ min, max int }

func (r *intRange) UnmarshalJSON(b []byte) error {
	var n float64
	if json.Unmarshal(b, &n) == nil {
		r.min, r.max = int(n), int(n)
		return nil
	}
	var m struct{ Min, Max float64 }
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	r.min, r.max = int(m.Min), int(m.Max)
	return nil
}

// roll draws a whole number in the range, as Java's uniform generator (nextInt(max - min + 1) + min).
func (r intRange) roll(rng *worldgen.BedrockRandom) int {
	if r.max <= r.min {
		return r.min
	}
	return int(rng.NextIntN(int32(r.max-r.min+1))) + r.min
}

func parseLootTable(text []byte) (*LootTable, error) {
	var raw struct {
		Pools []struct {
			Rolls      json.RawMessage `json:"rolls"`
			BonusRolls float64         `json:"bonus_rolls"`
			Entries    []struct {
				Type       string            `json:"type"`
				Name       string            `json:"name"`
				Weight     *int              `json:"weight"`
				Functions  []json.RawMessage `json:"functions"`
				Conditions []json.RawMessage `json:"conditions"`
			} `json:"entries"`
			Conditions []json.RawMessage `json:"conditions"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(text, &raw); err != nil {
		return nil, err
	}
	t := &LootTable{}
	for _, p := range raw.Pools {
		lp := lootPool{bonusRolls: p.BonusRolls, conds: parseConds(p.Conditions)}
		if len(p.Rolls) > 0 {
			if err := json.Unmarshal(p.Rolls, &lp.rolls); err != nil {
				return nil, fmt.Errorf("rolls: %w", err)
			}
		} else {
			lp.rolls = intRange{1, 1}
		}
		for _, e := range p.Entries {
			le := lootEntry{kind: e.Type, name: e.Name, weight: 1, conds: parseConds(e.Conditions)}
			if e.Weight != nil {
				le.weight = *e.Weight
			}
			for _, f := range e.Functions {
				le.functions = append(le.functions, parseFunc(f))
			}
			lp.entries = append(lp.entries, le)
		}
		t.Pools = append(t.Pools, lp)
	}
	return t, nil
}

func parseConds(raw []json.RawMessage) []lootCond {
	var out []lootCond
	for _, r := range raw {
		var c struct {
			Condition string  `json:"condition"`
			Chance    float64 `json:"chance"`
		}
		json.Unmarshal(r, &c)
		out = append(out, lootCond{kind: c.Condition, chance: c.Chance})
	}
	return out
}

func parseFunc(raw json.RawMessage) lootFunc {
	var f struct {
		Function string          `json:"function"`
		Count    json.RawMessage `json:"count"`
		Data     json.RawMessage `json:"data"`
		Damage   struct{ Min, Max float64 }
	}
	json.Unmarshal(raw, &f)
	lf := lootFunc{fn: strings.TrimPrefix(f.Function, "minecraft:")}
	switch lf.fn {
	case "set_count":
		lf.set = json.Unmarshal(f.Count, &lf.count) == nil
	case "set_data":
		lf.set = json.Unmarshal(f.Data, &lf.data) == nil
	case "set_damage":
		lf.dmg, lf.set = [2]float64{f.Damage.Min, f.Damage.Max}, true
	}
	return lf
}

// condsPass runs the conditions a chest can test: random_chance draws nextFloat() < chance (N2 1.1
// chance(p)); any other condition (killed by player, ...) fails, as nothing kills a chest.
func condsPass(cs []lootCond, rng *worldgen.BedrockRandom) bool {
	for _, c := range cs {
		switch c.kind {
		case "random_chance", "random_chance_with_looting", "random_difficulty_chance":
			if c.chance <= 0 {
				return false
			}
			if c.chance < 1 && !(rng.NextFloat() < float32(c.chance)) {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// items makes a table's items with the random (Java's LootPool shape; unconfirmed, see the top).
func (d *Data) items(t *LootTable, rng *worldgen.BedrockRandom, depth int) []item.Stack {
	var out []item.Stack
	for _, p := range t.Pools {
		if !condsPass(p.conds, rng) {
			continue
		}
		for range p.rolls.roll(rng) {
			var live []*lootEntry
			total := 0
			for i := range p.entries {
				if e := &p.entries[i]; condsPass(e.conds, rng) && e.weight > 0 {
					live = append(live, e)
					total += e.weight
				}
			}
			if total == 0 {
				continue
			}
			k := int(rng.NextIntN(int32(total)))
			var e *lootEntry
			for _, c := range live {
				if k -= c.weight; k < 0 {
					e = c
					break
				}
			}
			switch e.kind {
			case "item":
				if s, ok := d.makeItem(e, rng); ok {
					out = append(out, s)
				}
			case "loot_table":
				if sub := d.loot[e.name]; sub != nil && depth < 8 {
					out = append(out, d.items(sub, rng, depth+1)...)
				}
			}
		}
	}
	return out
}

// makeItem makes one entry's stack and applies the functions we can.
func (d *Data) makeItem(e *lootEntry, rng *worldgen.BedrockRandom) (item.Stack, bool) {
	count, meta := 1, 0
	var dmg [2]float64
	hasDmg := false
	for _, f := range e.functions {
		switch {
		case f.fn == "set_count" && f.set:
			count = f.count.roll(rng)
		case f.fn == "set_data" && f.set:
			meta = f.data.roll(rng)
		case f.fn == "set_damage" && f.set:
			// A fraction of durability left, uniform in the range (Java's shape; one draw).
			dmg, hasDmg = f.dmg, true
			dmg[0] += float64(rng.NextFloat()) * (dmg[1] - dmg[0])
		}
	}
	name := e.name
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	it, ok := world.ItemByName(name, int16(meta))
	if !ok {
		it, ok = world.ItemByName(name, 0)
	}
	if !ok || count <= 0 {
		return item.Stack{}, count <= 0 && ok
	}
	s := item.NewStack(it, count)
	if hasDmg {
		if dr, ok := it.(item.Durable); ok {
			mx := dr.DurabilityInfo().MaxDurability
			s = s.Damage(int(float64(mx) * (1 - dmg[0])))
		}
	}
	return s, true
}

// Fill rolls a table into a container: LootTable::fill (N2 2.3) on MT(seed), a fresh seed when 0 (N2
// 2.2). It reports whether the table was found.
func (d *Data) Fill(table string, seed int32, inv *inventory.Inventory) bool {
	t := d.loot[table]
	if t == nil {
		return false
	}
	if seed == 0 {
		seed = int32(rand.Uint32())
	}
	rng := worldgen.NewBedrockRandom(uint32(seed))
	stacks := d.items(t, rng, 0)
	var slots []int
	for i := range inv.Size() {
		if it, _ := inv.Item(i); it.Empty() {
			slots = append(slots, i)
		}
	}
	shuffleInts(rng, slots)
	var keep, split []item.Stack
	for _, s := range stacks {
		switch {
		case s.Count() == 1:
			keep = append(keep, s)
		case s.Count() > 1:
			split = append(split, s)
		}
	}
	if len(slots)-len(keep) > 0 {
		for len(split) > 0 {
			i := int(rng.NextIntN(int32(len(split))))
			s := split[i]
			split = append(split[:i], split[i+1:]...)
			half := 1
			if s.Count()/2 != 0 {
				half = int(rng.NextIntN(int32(s.Count()/2))) + 1
			}
			t := s.Grow(half - s.Count())
			s = s.Grow(-half)
			if s.Count() >= 2 && rng.NextBool() {
				split = append(split, s)
			} else {
				keep = append(keep, s)
			}
			if t.Count() >= 2 && rng.NextBool() {
				split = append(split, t)
			} else {
				keep = append(keep, t)
			}
		}
	}
	shuffleStacks(rng, keep)
	for _, s := range keep {
		if len(slots) == 0 {
			break
		}
		slot := slots[len(slots)-1]
		slots = slots[:len(slots)-1]
		_ = inv.SetItem(slot, s)
	}
	return true
}

// shuffleInts is the placer's and fill's shuffle: forward Fisher-Yates on raw words (N2 1.1 SHUFFLE).
func shuffleInts(r *worldgen.BedrockRandom, a []int) {
	for i := 1; i < len(a); i++ {
		if j := int(r.U32() % uint32(i+1)); j != i {
			a[i], a[j] = a[j], a[i]
		}
	}
}

func shuffleStacks(r *worldgen.BedrockRandom, a []item.Stack) {
	for i := 1; i < len(a); i++ {
		if j := int(r.U32() % uint32(i+1)); j != i {
			a[i], a[j] = a[j], a[i]
		}
	}
}
