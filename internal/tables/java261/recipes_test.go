package java261

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStaticIDs(t *testing.T) {
	// minecraft.wiki's Inventory numbers (oldid=3201379) for a few menus, and every
	// menu and block entity the front end uses.
	require.EqualValues(t, 2, MenuType("generic_9x3"))
	require.EqualValues(t, 12, MenuType("minecraft:crafting"))
	require.EqualValues(t, 14, MenuType("furnace"))
	require.EqualValues(t, 1, BlockEntityType("chest"))
	for _, m := range []string{"generic_9x3", "generic_9x6", "crafting", "furnace", "blast_furnace", "smoker", "hopper", "shulker_box", "brewing_stand", "generic_3x3"} {
		require.GreaterOrEqual(t, MenuType(m), int32(0), m)
	}
	for _, b := range []string{"chest", "ender_chest", "sign", "bed", "banner", "skull", "shulker_box", "furnace", "barrel", "mob_spawner"} {
		require.GreaterOrEqual(t, BlockEntityType(b), int32(0), b)
	}
	require.EqualValues(t, -1, MenuType("nothing"))
}

func TestCraft(t *testing.T) {
	id := func(n string) int32 { i := JavaItemID(n); require.GreaterOrEqual(t, i, int32(0), n); return i }
	e := int32(-1)
	oak, birch, stick, log, stripped := id("oak_planks"), id("birch_planks"), id("stick"), id("oak_log"), id("stripped_oak_wood")
	craft := func(grid []int32, size int) (int32, int) {
		r, ok := Craft(grid, size)
		if !ok {
			return -1, 0
		}
		return r.Result, r.Count
	}
	// Two planks one above the other make four sticks, anywhere, mixed woods.
	for _, g := range [][]int32{{oak, e, oak, e}, {e, oak, e, birch}} {
		r, n := craft(g, 2)
		require.Equal(t, stick, r)
		require.Equal(t, 4, n)
	}
	r, _ := craft([]int32{e, e, e, e, e, oak, e, e, oak}, 3)
	require.Equal(t, stick, r, "the shape moves in a 3x3 grid")
	r, _ = craft([]int32{oak, oak, e, e}, 2)
	require.NotEqual(t, stick, r, "side by side is not a stick")
	// A shapeless recipe takes its tag: any oak log.
	for _, l := range []int32{log, stripped} {
		r, n := craft([]int32{e, e, l, e}, 2)
		require.Equal(t, oak, r)
		require.Equal(t, 4, n)
	}
	// A crafting table needs all four.
	r, _ = craft([]int32{oak, oak, oak, oak}, 2)
	require.Equal(t, id("crafting_table"), r)
	r, _ = craft([]int32{oak, oak, oak, e}, 2)
	require.EqualValues(t, -1, r)
	// A furnace: a ring of cobblestone in the 3x3 only.
	c := id("cobblestone")
	r, _ = craft([]int32{c, c, c, c, e, c, c, c, c}, 3)
	require.Equal(t, id("furnace"), r)
	// Mirrored: a bow's strings may go on either side.
	s, st := id("string"), stick
	r, _ = craft([]int32{e, st, s, st, e, s, e, st, s}, 3)
	require.Equal(t, id("bow"), r)
	r, _ = craft([]int32{s, st, e, s, e, st, s, st, e}, 3)
	require.Equal(t, id("bow"), r, "mirrored")
	require.Greater(t, len(CraftingRecipes()), 900)
}

func TestJavaEntityOf(t *testing.T) {
	for bedrock, java := range map[string]string{
		"minecraft:xp_orb": "experience_orb", "minecraft:xp_bottle": "experience_bottle", "minecraft:fireworks_rocket": "firework_rocket",
		"minecraft:ender_crystal": "end_crystal", "minecraft:arrow": "arrow", "minecraft:falling_block": "falling_block",
		"minecraft:tnt": "tnt", "minecraft:snowball": "snowball", "minecraft:egg": "egg", "minecraft:ender_pearl": "ender_pearl",
		"minecraft:splash_potion": "splash_potion", "minecraft:lingering_potion": "lingering_potion", "minecraft:lightning_bolt": "lightning_bolt",
		"minecraft:area_effect_cloud": "area_effect_cloud", "minecraft:zombie": "zombie", "minecraft:item": "item",
	} {
		n, id := JavaEntityOf(bedrock)
		require.Equal(t, java, n, bedrock)
		require.Equal(t, EntityType(java), id)
		require.GreaterOrEqual(t, id, int32(0), bedrock)
	}
	_, id := JavaEntityOf("dragonfly:text")
	require.EqualValues(t, -1, id)
}
