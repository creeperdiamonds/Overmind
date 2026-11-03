package java261

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

// The tags come from azalea (ruling 17) for blocks, items, fluids and entity
// types, and are written by hand from minecraft.wiki (ruling 18) for timelines,
// damage types and enchantments. Each is checked by tags the client uses, with
// members they must hold.
func TestTags(t *testing.T) {
	regs := Tags()
	find := func(reg, tag string) []int32 {
		for _, r := range regs {
			if r.ID == reg {
				for _, tg := range r.Tags {
					if tg.Name == tag {
						return tg.IDs
					}
				}
			}
		}
		t.Fatalf("no tag %s in %s", tag, reg)
		return nil
	}
	var ids []string
	for _, r := range regs {
		ids = append(ids, r.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"minecraft:block", "minecraft:damage_type", "minecraft:dialog", "minecraft:enchantment", "minecraft:entity_type", "minecraft:fluid", "minecraft:item", "minecraft:timeline"}) {
		t.Fatalf("tag registries %v", ids)
	}
	if water := find("minecraft:fluid", "minecraft:water"); len(water) != 2 {
		t.Fatalf("fluid water tag = %v, want water and flowing water", water)
	}
	// minecraft-data lists blocks in registry order.
	raw, err := os.ReadFile("testdata/blocks.json")
	if err != nil {
		t.Fatal(err)
	}
	var bl []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &bl); err != nil {
		t.Fatal(err)
	}
	block := func(name string) int32 {
		for i, b := range bl {
			if b.Name == name {
				return int32(i)
			}
		}
		t.Fatalf("no block %s", name)
		return -1
	}
	if c := find("minecraft:block", "minecraft:climbable"); !slices.Contains(c, block("ladder")) || !slices.Contains(c, block("vine")) {
		t.Fatalf("climbable %v lacks the ladder or vines", c)
	}
	if !slices.Contains(find("minecraft:block", "minecraft:mineable/pickaxe"), block("stone")) {
		t.Fatal("mineable/pickaxe (a slash in its name) lacks stone")
	}
	if p := find("minecraft:item", "minecraft:planks"); !slices.Contains(p, JavaItemID("oak_planks")) || len(p) < 12 {
		t.Fatalf("item planks %v", p)
	}
	if !slices.Contains(find("minecraft:entity_type", "minecraft:skeletons"), EntityType("skeleton")) {
		t.Fatal("skeletons lacks the skeleton")
	}
}

// The hand-written tags (ruling 18), resolved against minecraft-data's
// registries. The client's own Overworld dimension type names the timeline
// tag #in_overworld: sent empty or not at all, no timeline runs, so the sky
// has no day cycle and no moon phases.
func TestHandTags(t *testing.T) {
	regs := Tags()
	entries := map[string][]string{}
	for _, r := range Registries() {
		entries[r.ID] = r.Entries
	}
	names := func(reg, tag string) []string {
		t.Helper()
		for _, r := range regs {
			if r.ID != reg {
				continue
			}
			for _, tg := range r.Tags {
				if tg.Name == tag {
					var out []string
					for _, id := range tg.IDs {
						if id < 0 || int(id) >= len(entries[reg]) {
							t.Fatalf("%s %s: ID %d outside the registry", reg, tag, id)
						}
						out = append(out, entries[reg][id])
					}
					return out
				}
			}
		}
		t.Fatalf("no tag %s in %s", tag, reg)
		return nil
	}
	sorted := func(s []string) []string { s = slices.Clone(s); slices.Sort(s); return s }
	if got := sorted(names("minecraft:timeline", "minecraft:in_overworld")); !slices.Equal(got, []string{"minecraft:day", "minecraft:early_game", "minecraft:moon", "minecraft:villager_schedule"}) {
		t.Fatalf("timeline in_overworld = %v", got)
	}
	for _, d := range []string{"minecraft:in_nether", "minecraft:in_end"} {
		if got := names("minecraft:timeline", d); !slices.Equal(got, []string{"minecraft:villager_schedule"}) {
			t.Fatalf("timeline %s = %v", d, got)
		}
	}
	for tag, want := range map[string][]string{
		"minecraft:is_fire":     {"minecraft:campfire", "minecraft:fireball", "minecraft:hot_floor", "minecraft:in_fire", "minecraft:lava", "minecraft:on_fire", "minecraft:unattributed_fireball"},
		"minecraft:is_freezing": {"minecraft:freeze"},
		"minecraft:is_drowning": {"minecraft:drown"},
		"minecraft:is_fall":     {"minecraft:ender_pearl", "minecraft:fall", "minecraft:stalagmite"},
		// #is_explosion is folded in.
		"minecraft:always_hurts_ender_dragons": {"minecraft:bad_respawn_point", "minecraft:explosion", "minecraft:fireworks", "minecraft:player_explosion"},
	} {
		if got := sorted(names("minecraft:damage_type", tag)); !slices.Equal(got, want) {
			t.Fatalf("damage type %s = %v, want %v", tag, got, want)
		}
	}
	if got := names("minecraft:damage_type", "minecraft:bypasses_shield"); len(got) != 19+10 {
		t.Fatalf("bypasses_shield holds %d, want bypasses_armor's 19 and 10 more", len(got))
	}
	if got := sorted(names("minecraft:enchantment", "minecraft:curse")); !slices.Equal(got, []string{"minecraft:binding_curse", "minecraft:vanishing_curse"}) {
		t.Fatalf("enchantment curse = %v", got)
	}
	// The tooltip order is the list's order, and holds every enchantment.
	order := names("minecraft:enchantment", "minecraft:tooltip_order")
	if len(order) != len(entries["minecraft:enchantment"]) || order[0] != "minecraft:binding_curse" || order[2] != "minecraft:riptide" || order[len(order)-1] != "minecraft:mending" {
		t.Fatalf("tooltip_order = %v", order)
	}
	if got := names("minecraft:enchantment", "minecraft:in_enchanting_table"); len(got) != 36 || slices.Contains(got, "minecraft:mending") {
		t.Fatalf("in_enchanting_table = %v", got)
	}
}

// A real 26.1 client refused to join with "Unbound tags in registry
// minecraft:dialog: [pause_screen_additions, quick_actions]": both must be sent,
// even though vanilla's are empty.
func TestDialogTagsAreBound(t *testing.T) {
	for _, r := range Tags() {
		if r.ID != "minecraft:dialog" {
			continue
		}
		got := map[string]bool{}
		for _, tg := range r.Tags {
			got[tg.Name] = true
		}
		for _, want := range []string{"minecraft:pause_screen_additions", "minecraft:quick_actions"} {
			if !got[want] {
				t.Fatalf("dialog tag %s not sent", want)
			}
		}
		return
	}
	t.Fatal("no dialog tag registry sent")
}
