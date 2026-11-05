package structures

import (
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strings"

	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Loot. A generated chest stores its table's path and a loot seed; the table is rolled when the chest is
// first opened (N2 2.1, 2.2, owner's decision). Filling the container follows LootTable::fill (N2 2.3);
// rolls, the entry pick and the item functions follow getRandomItems as read from the code (N2 phase 3,
// "loot pool rolls, entry pick, item functions", cited LR below). Not validated: no BDS chest has been
// opened. Exploration maps, potions, dyes, books and stew functions were not read and are not applied;
// the item is still made.

// LootTable is a parsed loot table.
type LootTable struct {
	Pools []lootPool
}

type lootPool struct {
	rolls   floatRange
	entries []lootEntry
	conds   []lootCond
	tiers   *lootTiers
}

type lootTiers struct {
	initial, bonusRolls int
	bonusChance         float32
}

type lootEntry struct {
	kind      string // item, loot_table, empty
	name      string
	weight    int
	functions []lootFunc
	conds     []lootCond
}

type lootFunc struct {
	fn     string
	count  floatRange // set_count
	data   floatRange // set_data
	dmg    [2]float64 // set_damage
	levels floatRange // enchant_with_levels
	values floatRange // random_aux_value, set_ominous_bottle's amplifier, random_block_state's values
	// enchants are specific_enchants' level ranges (one draw for each listed enchantment whose range is
	// wider than one level, NOTES-loot-trial-bastion T2a).
	enchants []floatRange
	stew     int  // set_stew_effect's number of effects.
	set      bool // The function's value was read.
	conds    []lootCond
}

type lootCond struct {
	kind   string
	chance float64
}

// floatRange is a number or a {min, max} range, as the tables write them.
type floatRange struct {
	min, max float64
	isRange  bool
}

func (r *floatRange) UnmarshalJSON(b []byte) error {
	var n float64
	if json.Unmarshal(b, &n) == nil {
		r.min, r.max = n, n
		return nil
	}
	var m struct{ Min, Max float64 }
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	r.min, r.max, r.isRange = m.Min, m.Max, true
	return nil
}

// draw is LR's integer range: lo = floor(min), hi = floor(max), lo + nextInt(hi - lo + 1) when lo <= hi.
// It draws even when lo == hi.
func (r floatRange) draw(rng *worldgen.BedrockRandom) int {
	lo, hi := int(math.Floor(r.min)), int(math.Floor(r.max))
	if lo > hi {
		return lo
	}
	return lo + int(rng.NextIntN(int32(hi-lo+1)))
}

// Limits on what a loot table may ask for, checked when it is read. A table is rolled on the world's tick
// when its chest is first opened, so a broken or edited one must not make that tick run away. Vanilla's
// largest rolls, tiers and counts are single digits.
const (
	maxLootRolls  = 64      // rolls, initial_range, bonus_rolls
	maxLootNumber = 1024    // set_count, set_data, enchant levels
	maxLootWeight = 1 << 20 // an entry's weight
)

// lootPicksPer is how many entry picks one fill may make, per slot of the container: with nested tables
// a table's picks multiply, and this bounds the work and the items whatever the data says.
const lootPicksPer = 4

func checkRange(what string, r floatRange, limit float64) error {
	if math.IsNaN(r.min) || math.IsNaN(r.max) || math.Abs(r.min) > limit || math.Abs(r.max) > limit {
		return fmt.Errorf("%s %v..%v is outside -%v..%v", what, r.min, r.max, limit, limit)
	}
	return nil
}

func parseLootTable(text []byte) (*LootTable, error) {
	var raw struct {
		Pools []struct {
			Rolls json.RawMessage `json:"rolls"`
			Tiers *struct {
				Initial     int     `json:"initial_range"`
				BonusRolls  int     `json:"bonus_rolls"`
				BonusChance float32 `json:"bonus_chance"`
			} `json:"tiers"`
			Entries []struct {
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
		lp := lootPool{conds: parseConds(p.Conditions), rolls: floatRange{min: 1, max: 1}}
		if len(p.Rolls) > 0 {
			if err := json.Unmarshal(p.Rolls, &lp.rolls); err != nil {
				return nil, fmt.Errorf("rolls: %w", err)
			}
		}
		if err := checkRange("rolls", lp.rolls, maxLootRolls); err != nil {
			return nil, err
		}
		if p.Tiers != nil {
			lp.tiers = &lootTiers{p.Tiers.Initial, p.Tiers.BonusRolls, p.Tiers.BonusChance}
			if p.Tiers.Initial < 0 || p.Tiers.Initial > maxLootRolls || p.Tiers.BonusRolls < 0 || p.Tiers.BonusRolls > maxLootRolls {
				return nil, fmt.Errorf("tiers %d, bonus rolls %d: outside 0..%d", p.Tiers.Initial, p.Tiers.BonusRolls, maxLootRolls)
			}
		}
		for _, e := range p.Entries {
			le := lootEntry{kind: e.Type, name: e.Name, weight: 1, conds: parseConds(e.Conditions)}
			if e.Weight != nil {
				le.weight = *e.Weight
			}
			if le.weight > maxLootWeight {
				return nil, fmt.Errorf("weight %d is above %d", le.weight, maxLootWeight)
			}
			for _, f := range e.Functions {
				lf := parseFunc(f)
				for _, r := range []floatRange{lf.count, lf.data, lf.levels} {
					if err := checkRange(lf.fn, r, maxLootNumber); err != nil {
						return nil, err
					}
				}
				le.functions = append(le.functions, lf)
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
			Condition string   `json:"condition"`
			Chance    float64  `json:"chance"`
			Default   *float64 `json:"default_chance"`
		}
		json.Unmarshal(r, &c)
		kind := strings.TrimPrefix(c.Condition, "minecraft:")
		if kind == "random_difficulty_chance" && c.Default != nil {
			// Its files give a default chance and per-difficulty ones; a chest is rolled with no
			// difficulty in its context, so the default stands.
			c.Chance = *c.Default
		}
		out = append(out, lootCond{kind: kind, chance: c.Chance})
	}
	return out
}

func parseFunc(raw json.RawMessage) lootFunc {
	var f struct {
		Function   string          `json:"function"`
		Count      json.RawMessage `json:"count"`
		Data       json.RawMessage `json:"data"`
		Levels     json.RawMessage `json:"levels"`
		Damage     struct{ Min, Max float64 }
		Values     json.RawMessage   `json:"values"`
		Amplifier  json.RawMessage   `json:"amplifier"`
		Enchants   []json.RawMessage `json:"enchants"`
		Effects    []json.RawMessage `json:"effects"`
		Conditions []json.RawMessage `json:"conditions"`
	}
	json.Unmarshal(raw, &f)
	lf := lootFunc{fn: strings.TrimPrefix(f.Function, "minecraft:"), conds: parseConds(f.Conditions)}
	switch lf.fn {
	case "set_count":
		lf.set = json.Unmarshal(f.Count, &lf.count) == nil
	case "set_data":
		lf.set = json.Unmarshal(f.Data, &lf.data) == nil
	case "set_damage":
		lf.dmg, lf.set = [2]float64{f.Damage.Min, f.Damage.Max}, true
	case "enchant_with_levels":
		lf.set = json.Unmarshal(f.Levels, &lf.levels) == nil
	case "enchant_randomly", "random_dye":
		lf.set = true
	case "random_aux_value", "random_block_state":
		lf.set = json.Unmarshal(f.Values, &lf.values) == nil
	case "set_ominous_bottle":
		lf.set = json.Unmarshal(f.Amplifier, &lf.values) == nil
	case "set_stew_effect":
		lf.stew, lf.set = len(f.Effects), true
	case "specific_enchants":
		for _, e := range f.Enchants {
			var en struct {
				Level json.RawMessage `json:"level"`
			}
			var r floatRange
			if json.Unmarshal(e, &en) == nil && en.Level != nil && json.Unmarshal(en.Level, &r) == nil {
				lf.enchants = append(lf.enchants, r)
			} else {
				lf.enchants = append(lf.enchants, floatRange{min: 1, max: 1})
			}
		}
		lf.set = true
	}
	return lf
}

// condsPass runs the conditions a chest can test: random_chance draws nextFloat() < chance (N2 1.1
// chance(p)). The kill conditions fail, as nothing kills a chest. The others need the context (the
// chest's biome: biome_has_tag, ...), draw nothing, and BDS evaluates them (NOTES-loot-trial-bastion T2b
// rule 3); the fill has no context here, so they pass (not evaluated; the camp maps' "biome_has_tag !="
// conditions all passed in BDS's chests).
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
		case "killed_by_player", "killed_by_player_or_pets", "killed_by_entity", "entity_properties", "has_mark_variant":
			return false
		}
	}
	return true
}

// items is getRandomItems (LR): every pool in order.
// picks is what is left of a fill's entry picks (lootPicksPer).
func (d *Data) items(t *LootTable, rng *worldgen.BedrockRandom, depth int, picks *int) []lootStack {
	var out []lootStack
	for i := range t.Pools {
		out = d.addRandomItems(&t.Pools[i], rng, depth, picks, out)
	}
	return out
}

// addRandomItems is LootPool::addRandomItems (LR). Luck is 0 when a chest is opened, so bonus rolls and
// qualities add nothing, but the bonus float is still drawn.
func (d *Data) addRandomItems(p *lootPool, rng *worldgen.BedrockRandom, depth int, picks *int, out []lootStack) []lootStack {
	if !condsPass(p.conds, rng) {
		return out
	}
	if p.tiers != nil {
		u := 0
		if p.tiers.initial >= 1 {
			u = int(rng.NextIntN(int32(p.tiers.initial)))
		}
		for range p.tiers.bonusRolls {
			if rng.NextFloat() < p.tiers.bonusChance {
				u++
			}
		}
		if u >= 0 && u < len(p.entries) && *picks > 0 {
			*picks--
			out = d.createItem(&p.entries[u], rng, depth, picks, out)
		}
		return out
	}
	n := p.rolls.draw(rng)
	rng.NextFloat() // The bonus rolls' float, always drawn (LR); times luck 0 it adds none.
	for range n {
		if *picks <= 0 {
			break
		}
		*picks--
		var live []*lootEntry
		total := 0
		for i := range p.entries {
			e := &p.entries[i]
			if condsPass(e.conds, rng) && e.weight > 0 { // Conditions are run every roll (LR).
				live = append(live, e)
				total += e.weight
			}
		}
		if total == 0 {
			continue
		}
		k := int(rng.NextIntN(int32(total)))
		for _, e := range live {
			if k -= e.weight; k < 0 {
				out = d.createItem(e, rng, depth, picks, out)
				break
			}
		}
	}
	return out
}

// createItem makes an entry's items (LR): an item, a referenced table with the same random, or nothing.
func (d *Data) createItem(e *lootEntry, rng *worldgen.BedrockRandom, depth int, picks *int, out []lootStack) []lootStack {
	switch e.kind {
	case "item":
		out = append(out, d.makeItem(e, rng)...)
	case "loot_table":
		if sub := d.loot[e.name]; sub != nil && depth < 8 {
			out = append(out, d.items(sub, rng, depth+1, picks)...)
		}
	}
	return out
}

// lootStack is a stack the fill places: an item, or (it == nil) an item Dragonfly has no type for, which
// still takes its place in the fill's shuffles and leaves its slot empty (T2b rule 4).
type lootStack struct {
	s item.Stack
	n int
}

func (l lootStack) grow(by int) lootStack {
	l.n += by
	if l.s.Empty() {
		return l
	}
	l.s = l.s.Grow(by)
	return l
}

// lootItemNames are loot names BDS stores as another item (T2b rule 4).
var lootItemNames = map[string]string{"minecraft:fireball": "minecraft:fire_charge"}

// makeItem is LootItem::createItem (LR): a stack of one, then each function whose conditions pass, in
// order, with the loot random. An item the server does not know still runs its functions (T2b rule
// 4). A count above the item's stack size is cut into full stacks with no draws (T2b rule 2).
func (d *Data) makeItem(e *lootEntry, rng *worldgen.BedrockRandom) []lootStack {
	name := e.name
	if !strings.Contains(name, ":") {
		name = "minecraft:" + name
	}
	if to, ok := lootItemNames[name]; ok {
		name = to
	}
	it, known := world.ItemByName(name, 0)
	count, meta, damage := 1, 0, 0
	var enchants []item.Enchantment
	for _, f := range e.functions {
		if !f.set || !condsPass(f.conds, rng) {
			continue
		}
		switch f.fn {
		case "set_count":
			count = f.count.draw(rng)
		case "set_data":
			if f.data.isRange { // Draws only with a range (LR).
				meta = f.data.draw(rng)
			} else {
				meta = int(math.Floor(f.data.min))
			}
		case "random_aux_value": // Always drawn (T2b rule 1).
			meta = f.values.draw(rng)
		case "set_ominous_bottle":
			meta = f.values.draw(rng)
		case "random_block_state":
			if _, isBlock := it.(world.Block); known && isBlock {
				f.values.draw(rng) // The state value itself is not applied.
			}
		case "set_stew_effect":
			if name == "minecraft:suspicious_stew" && f.stew > 0 {
				rng.NextIntN(int32(f.stew)) // The effect (aux value) is not applied: Dragonfly's stew types differ.
			}
		case "specific_enchants":
			for _, r := range f.enchants {
				if lo, hi := int(math.Floor(r.min)), int(math.Floor(r.max)); hi > lo {
					rng.NextIntN(int32(hi - lo + 1))
				}
			}
		case "random_dye":
			if strings.HasPrefix(name, "minecraft:leather_") || name == "minecraft:wolf_armor" { // Dyeable items.
				rng.NextIntN(16)
				f1 := rng.NextFloat()
				if f1 < 0.3 {
					rng.NextIntN(16)
				}
				if rng.NextFloat() < 0.2 {
					rng.NextIntN(16)
				} // The colour itself is not applied.
			}
		case "set_damage":
			if dr, ok := it.(item.Durable); known && ok { // Only damageable items draw (LR).
				fr := float64(rng.NextFloat())
				mx := float64(dr.DurabilityInfo().MaxDurability)
				damage = int(math.Floor(mx * (1 - (f.dmg[0] + (f.dmg[1]-f.dmg[0])*fr))))
			}
		case "enchant_randomly":
			rng.NextIntN(30) // LR: always drawn from the loot random first.
			if known {
				enchants = append(enchants, randomEnchant(it))
			}
		case "enchant_with_levels":
			f.levels.draw(rng)
			if known {
				enchants = append(enchants, randomEnchant(it))
			}
		}
	}
	if count <= 0 {
		return nil
	}
	if !known {
		return cutStacks(lootStack{n: 1}, count, 64)
	}
	if meta > 0 {
		if m, ok := world.ItemByName(name, int16(meta)); ok {
			it = m
		}
	}
	s := item.NewStack(it, 1)
	if damage > 0 {
		s = s.Damage(damage)
	}
	var keep []item.Enchantment
	for _, en := range enchants {
		if en.Type() != nil {
			keep = append(keep, en)
		}
	}
	if len(keep) > 0 {
		if _, isBook := it.(item.Book); isBook {
			s = item.NewStack(item.EnchantedBook{}, 1)
		}
		s = s.WithEnchantments(keep...)
	}
	return cutStacks(lootStack{s: s, n: 1}, count, s.MaxCount())
}

// cutStacks is a stack of count, in stacks of at most max.
func cutStacks(one lootStack, count, max int) []lootStack {
	var out []lootStack
	for count > 0 {
		n := min(count, max)
		out = append(out, one.grow(n-1))
		count -= n
	}
	return out
}

// randomEnchant picks the enchantment of enchant_randomly and enchant_with_levels. BDS chooses it with a
// thread-local random that is not the loot random (LR), so it is not a function of the loot seed, and
// its choice rule was not read. Here: one enchantment the item takes, at a level from 1 to its maximum,
// from an unseeded random.
func randomEnchant(it world.Item) item.Enchantment {
	var fit []item.EnchantmentType
	_, isBook := it.(item.Book)
	for _, t := range item.Enchantments() {
		if isBook || t.CompatibleWithItem(it) {
			fit = append(fit, t)
		}
	}
	if len(fit) == 0 {
		return item.Enchantment{}
	}
	t := fit[rand.IntN(len(fit))]
	return item.NewEnchantment(t, 1+rand.IntN(t.MaxLevel()))
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
	picks := lootPicksPer * inv.Size()
	stacks := d.items(t, rng, 0, &picks)
	var slots []int
	for i := range inv.Size() {
		if it, _ := inv.Item(i); it.Empty() {
			slots = append(slots, i)
		}
	}
	shuffleInts(rng, slots)
	var keep, split []lootStack
	for _, s := range stacks {
		switch {
		case s.n == 1:
			keep = append(keep, s)
		case s.n > 1:
			split = append(split, s)
		}
	}
	// Checked once, before the loop (N2 2.3: unlike Java, BDS does not re-test inside it). The loop ends:
	// every pass takes one stack out and puts back at most two smaller ones, never a stack of one.
	if len(slots)-len(keep) > 0 {
		for len(split) > 0 {
			i := int(rng.NextIntN(int32(len(split))))
			s := split[i]
			split = append(split[:i], split[i+1:]...)
			half := 1
			if s.n/2 != 0 {
				half = int(rng.NextIntN(int32(s.n/2))) + 1
			}
			t := s.grow(half - s.n)
			s = s.grow(-half)
			if s.n >= 2 && rng.NextBool() {
				split = append(split, s)
			} else {
				keep = append(keep, s)
			}
			if t.n >= 2 && rng.NextBool() {
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
		if !s.s.Empty() {
			_ = inv.SetItem(slot, s.s)
		}
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

func shuffleStacks(r *worldgen.BedrockRandom, a []lootStack) {
	for i := 1; i < len(a); i++ {
		if j := int(r.U32() % uint32(i+1)); j != i {
			a[i], a[j] = a[j], a[i]
		}
	}
}
