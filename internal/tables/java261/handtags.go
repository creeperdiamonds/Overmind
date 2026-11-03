package java261

import (
	"fmt"
	"strings"
)

// Hand-written tags (ruling 18) for three data-driven registries no open
// project has tags for. Each list is copied from a minecraft.wiki revision of
// the registry's tag page as it stood for 26.1 (before any 26.2 snapshot
// edit), cited per registry. Entry names are resolved to IDs by their place in
// minecraft-data's registry list (Registries), so a name not in 26.1's registry
// panics in Tags and fails TestHandTags. "#name" includes another tag of the
// same registry, as the page writes it; the packet carries the IDs only.
// Experimental tags (the Villager Trade Rebalance's trades/*) are left out.
var handTags = []struct {
	registry string
	source   string
	tags     [][2]string // name, space-separated entries
}{
	{"minecraft:timeline", `minecraft.wiki "Timeline tag (Java Edition)" oldid=3839231 (same lists as oldid=3310207, 26.1)`, [][2]string{
		{"universal", "villager_schedule"},
		{"in_end", "#universal"},
		{"in_nether", "#universal"},
		{"in_overworld", "#universal day moon early_game"},
	}},
	// The client refuses to join while these two are unbound ("Unbound tags in
	// registry minecraft:dialog", seen on a real 26.1 client on 2026-11-04).
	// Both are empty in vanilla: minecraft.wiki "Dialog" (Java Edition), tags
	// section, lists them with no entries.
	{"minecraft:dialog", `minecraft.wiki "Dialog" (Java Edition), tags section; required bound by the 26.1 client`, [][2]string{
		{"pause_screen_additions", ""},
		{"quick_actions", ""},
	}},
	// The client's item table reads these tags while it finishes configuration
	// ("Missing tag TagKey[minecraft:banner_pattern / minecraft:pattern_item/flower]",
	// a real 26.1 client, 2026-11-04). Lists from minecraft.wiki "Banner
	// pattern tag (Java Edition)" and "Banner/Patterns".
	{"minecraft:banner_pattern", `minecraft.wiki "Banner pattern tag (Java Edition)"; required by the 26.1 client`, [][2]string{
		{"no_item_required", "square_bottom_left square_bottom_right square_top_left square_top_right stripe_bottom stripe_top stripe_left stripe_right stripe_center stripe_middle stripe_downright stripe_downleft small_stripes cross straight_cross triangle_bottom triangle_top triangles_bottom triangles_top diagonal_left diagonal_up_right diagonal_up_left diagonal_right circle rhombus half_vertical half_horizontal half_vertical_right half_horizontal_bottom border gradient gradient_up"},
		{"pattern_item/bordure_indented", "curly_border"},
		{"pattern_item/creeper", "creeper"},
		{"pattern_item/field_masoned", "bricks"},
		{"pattern_item/flow", "flow"},
		{"pattern_item/flower", "flower"},
		{"pattern_item/globe", "globe"},
		{"pattern_item/guster", "guster"},
		{"pattern_item/mojang", "mojang"},
		{"pattern_item/piglin", "piglin"},
		{"pattern_item/skull", "skull"},
	}},
	// Goat horns name these (same item table). minecraft.wiki "Instrument tag
	// (Java Edition)".
	{"minecraft:instrument", `minecraft.wiki "Instrument tag (Java Edition)"; required by the 26.1 client`, [][2]string{
		{"regular_goat_horns", "ponder_goat_horn sing_goat_horn seek_goat_horn feel_goat_horn"},
		{"screaming_goat_horns", "admire_goat_horn call_goat_horn yearn_goat_horn dream_goat_horn"},
		{"goat_horns", "#regular_goat_horns #screaming_goat_horns"},
	}},
	// The creative tab lists paintings by this tag. Bound but empty for now:
	// the full list is not yet copied from a cited source (creative only).
	{"minecraft:painting_variant", `bound empty; minecraft.wiki "Painting variant tag (Java Edition)" to be copied`, [][2]string{
		{"placeable", ""},
	}},
	{"minecraft:damage_type", `minecraft.wiki "Damage type tag (Java Edition)" oldid=3498996`, [][2]string{
		{"is_explosion", "fireworks explosion player_explosion bad_respawn_point"},
		{"is_player_attack", "player_attack spear mace_smash"},
		{"bypasses_armor", "on_fire in_wall cramming drown fly_into_wall generic wither dragon_breath starve fall ender_pearl freeze stalagmite magic indirect_magic out_of_world generic_kill sonic_boom outside_border"},
		{"bypasses_invulnerability", "out_of_world generic_kill"},
		{"panic_environmental_causes", "cactus freeze hot_floor in_fire lava lightning_bolt on_fire"},
		{"always_hurts_ender_dragons", "#is_explosion"},
		{"always_kills_armor_stands", "arrow trident fireball wither_skull wind_charge"},
		{"always_most_significant_fall", "out_of_world"},
		{"always_triggers_silverfish", "magic"},
		{"avoids_guardian_thorns", "magic thorns #is_explosion"},
		{"burns_armor_stands", "on_fire"},
		{"burn_from_stepping", "campfire hot_floor"},
		{"bypasses_cooldown", ""},
		{"bypasses_effects", "starve"},
		{"bypasses_enchantments", "sonic_boom"},
		{"bypasses_resistance", "out_of_world generic_kill"},
		{"bypasses_shield", "#bypasses_armor cactus campfire dry_out falling_anvil falling_stalactite hot_floor in_fire lava lightning_bolt sweet_berry_bush"},
		{"bypasses_wolf_armor", "#bypasses_invulnerability cramming drown dry_out freeze in_wall indirect_magic magic outside_border starve thorns wither"},
		{"can_break_armor_stand", "player_explosion #is_player_attack"},
		{"damages_helmet", "falling_anvil falling_block falling_stalactite"},
		{"ignites_armor_stands", "in_fire campfire"},
		{"is_drowning", "drown"},
		{"is_fall", "fall ender_pearl stalagmite"},
		{"is_fire", "in_fire campfire on_fire lava hot_floor unattributed_fireball fireball"},
		{"is_freezing", "freeze"},
		{"is_lightning", "lightning_bolt"},
		{"is_projectile", "arrow trident mob_projectile unattributed_fireball fireball wither_skull thrown wind_charge"},
		{"mace_smash", "mace_smash"},
		{"no_anger", "mob_attack_no_aggro"},
		{"no_impact", "drown"},
		{"no_knockback", "explosion player_explosion bad_respawn_point in_fire lightning_bolt on_fire lava hot_floor in_wall cramming drown starve cactus fall ender_pearl fly_into_wall out_of_world generic magic wither dragon_breath dry_out sweet_berry_bush freeze stalagmite outside_border generic_kill campfire spear"},
		{"panic_causes", "#panic_environmental_causes arrow dragon_breath explosion fireball fireworks indirect_magic magic mob_attack mob_projectile player_explosion sonic_boom sting thrown trident unattributed_fireball wind_charge wither wither_skull #is_player_attack"},
		{"witch_resistant_to", "magic indirect_magic sonic_boom thorns"},
		{"wither_immune_to", "drown"},
	}},
	{"minecraft:enchantment", `minecraft.wiki "Enchantment tag (Java Edition)" oldid=3502711`, [][2]string{
		{"non_treasure", "protection fire_protection feather_falling blast_protection projectile_protection respiration aqua_affinity thorns depth_strider sharpness smite bane_of_arthropods knockback fire_aspect looting sweeping_edge efficiency silk_touch unbreaking fortune power punch flame infinity luck_of_the_sea lure loyalty impaling riptide channeling multishot quick_charge piercing density breach lunge"},
		{"treasure", "binding_curse vanishing_curse swift_sneak soul_speed frost_walker mending wind_burst"},
		{"curse", "binding_curse vanishing_curse"},
		{"double_trade_price", "#treasure"},
		{"in_enchanting_table", "#non_treasure"},
		{"on_mob_spawn_equipment", "#non_treasure"},
		{"on_random_loot", "#non_treasure binding_curse vanishing_curse frost_walker mending"},
		{"on_traded_equipment", "#non_treasure"},
		{"prevents_bee_spawns_when_mining", "silk_touch"},
		{"prevents_decorated_pot_shattering", "silk_touch"},
		{"prevents_ice_melting", "silk_touch"},
		{"prevents_infested_spawns", "silk_touch"},
		{"smelts_loot", "fire_aspect"},
		// The page keeps this list in the game's order (sort=none): the
		// tooltip shows enchantments in it.
		{"tooltip_order", "binding_curse vanishing_curse riptide channeling wind_burst frost_walker lunge sharpness smite bane_of_arthropods impaling power density breach piercing sweeping_edge multishot fire_aspect flame knockback punch protection blast_protection fire_protection projectile_protection feather_falling fortune looting silk_touch luck_of_the_sea efficiency quick_charge lure respiration aqua_affinity soul_speed swift_sneak depth_strider thorns loyalty unbreaking infinity mending"},
		{"tradeable", "#non_treasure binding_curse vanishing_curse frost_walker mending"},
		{"exclusive_set/armor", "protection blast_protection fire_protection projectile_protection"},
		{"exclusive_set/boots", "frost_walker depth_strider"},
		{"exclusive_set/bow", "infinity mending"},
		{"exclusive_set/crossbow", "multishot piercing"},
		{"exclusive_set/damage", "sharpness smite bane_of_arthropods impaling density breach"},
		{"exclusive_set/mining", "fortune silk_touch"},
		{"exclusive_set/riptide", "loyalty channeling"},
	}},
}

// handTagRegistries resolves handTags against the registries sent in
// configuration. A tag may include only a tag listed before it.
func handTagRegistries() []TagRegistry {
	regs := map[string][]string{}
	for _, r := range Registries() {
		regs[r.ID] = r.Entries
	}
	var out []TagRegistry
	for _, h := range handTags {
		entries, ok := regs[h.registry]
		if !ok {
			panic(fmt.Sprintf("java261: hand tags for %s, which is not a sent registry", h.registry))
		}
		pos := map[string]int32{}
		for i, e := range entries {
			pos[e] = int32(i)
		}
		done := map[string][]int32{}
		r := TagRegistry{ID: h.registry}
		for _, t := range h.tags {
			var ids []int32
			seen := map[int32]bool{}
			add := func(id int32) {
				if !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
			for _, e := range strings.Fields(t[1]) {
				if inc, ok := strings.CutPrefix(e, "#"); ok {
					sub, ok := done[inc]
					if !ok {
						panic(fmt.Sprintf("java261: %s tag %s includes #%s before it is listed", h.registry, t[0], inc))
					}
					for _, id := range sub {
						add(id)
					}
					continue
				}
				id, ok := pos["minecraft:"+e]
				if !ok {
					panic(fmt.Sprintf("java261: %s tag %s names %s, not in 26.1's registry", h.registry, t[0], e))
				}
				add(id)
			}
			done[t[0]] = ids
			r.Tags = append(r.Tags, Tag{Name: "minecraft:" + t[0], IDs: ids})
		}
		out = append(out, r)
	}
	return out
}
