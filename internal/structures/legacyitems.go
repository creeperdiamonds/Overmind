package structures

import (
	"github.com/df-mc/dragonfly/server/world"
)

// Mansion templates (the install's Java-format .nbt files) keep their decorative chests' items in the
// pre-1.13 form: a legacy id with a Damage value for the variant, and enchantments by legacy number.
// legacyItems converts them to Bedrock's item NBT, as Dragonfly's chest reads it (Name, Damage,
// Count, Slot, tag.ench). The tables below are public game knowledge (the 1.12 data values and
// enchantment numbers), not read from any program, and cover what the 1.26.60 templates hold: alliums,
// dark oak saplings, an Efficiency I iron axe and ender pearls (round 21).

// legacyVariants names a legacy id's variants by Damage value.
var legacyVariants = map[string][]string{
	"minecraft:red_flower": {"poppy", "blue_orchid", "allium", "azure_bluet", "red_tulip", "orange_tulip",
		"white_tulip", "pink_tulip", "oxeye_daisy"},
	"minecraft:sapling": {"oak_sapling", "spruce_sapling", "birch_sapling", "jungle_sapling", "acacia_sapling",
		"dark_oak_sapling"},
}

// legacyEnchantments maps a Java legacy enchantment number to Bedrock's.
var legacyEnchantments = map[int16]int16{
	32: 15, // Efficiency.
}

// legacyItems converts a template chest's Items. Items it cannot convert are left out and returned
// as skipped (their legacy id, or "enchantment" for an unknown enchantment).
func legacyItems(nbt map[string]any) (items []any, skipped []string) {
	list, _ := nbt["Items"].([]any)
	for _, e := range list {
		m, ok := e.(map[string]any)
		if !ok {
			continue
		}
		id, _ := m["id"].(string)
		damage := nbtInt(m["Damage"])
		name := id
		if v, ok := legacyVariants[id]; ok {
			if damage < 0 || damage >= len(v) {
				skipped = append(skipped, id)
				continue
			}
			name, damage = "minecraft:"+v[damage], 0
		}
		if _, ok := world.ItemByName(name, int16(damage)); !ok {
			skipped = append(skipped, id)
			continue
		}
		out := map[string]any{"Name": name, "Damage": int16(damage), "Count": byte(nbtInt(m["Count"])),
			"Slot": byte(nbtInt(m["Slot"]))}
		if tag, ok := m["tag"].(map[string]any); ok {
			var ench []any
			list, _ := tag["ench"].([]any)
			for _, x := range list {
				em, _ := x.(map[string]any)
				b, ok := legacyEnchantments[int16(nbtInt(em["id"]))]
				if !ok {
					skipped = append(skipped, "enchantment")
					continue
				}
				ench = append(ench, map[string]any{"id": b, "lvl": int16(nbtInt(em["lvl"]))})
			}
			if len(ench) > 0 {
				out["tag"] = map[string]any{"ench": ench}
			}
		}
		items = append(items, out)
	}
	return items, skipped
}

// nbtInt reads an NBT number of any integer type.
func nbtInt(v any) int {
	switch n := v.(type) {
	case byte:
		return int(n)
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	}
	return 0
}
