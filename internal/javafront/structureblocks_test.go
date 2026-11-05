package javafront

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// The blocks structures place that are our own types (worldgen) or Dragonfly
// states with no Go type reach Java as their Java block: suspicious sand and
// gravel, the flower pot (with its plant, from the block entity), the
// dispenser, the mob spawner, the jungle temple's redstone pieces and the stronghold's and mineshaft's
// blocks Dragonfly has no type for.
func TestJavaSeesStructureBlocks(t *testing.T) {
	world.DefaultBlockRegistry.Finalize() // as the server does at start; repeating it is safe
	javaOf := func(b world.Block) string {
		s, _ := stateOf(cube.Pos{}, world.BlockRuntimeID(b), airRID(), b)
		return java261.BlockName(uint16(s))
	}
	byName := func(name string, props map[string]any) world.Block {
		b, ok := world.BlockByName(name, props)
		require.True(t, ok, "%s %v not registered", name, props)
		return b
	}
	for _, c := range []struct {
		b    world.Block
		java string
	}{
		{worldgen.Brushable{LootTable: "loot_tables/x.json"}, "minecraft:suspicious_sand"},
		{worldgen.Brushable{Gravel: true, Progress: 2, Hanging: true}, "minecraft:suspicious_gravel"},
		{worldgen.FlowerPot{}, "minecraft:flower_pot"},
		{worldgen.FlowerPot{Plant: "minecraft:red_mushroom"}, "minecraft:potted_red_mushroom"},
		{worldgen.FlowerPot{Plant: "minecraft:cactus", PlantStates: map[string]any{"age": int32(0)}}, "minecraft:potted_cactus"},
		{worldgen.FlowerPot{Plant: "minecraft:azalea"}, "minecraft:potted_azalea_bush"},
		{worldgen.FlowerPot{Plant: "minecraft:no_such_plant"}, "minecraft:flower_pot"},
		{worldgen.Dispenser{Facing: 2, LootTable: "loot_tables/chests/jungle_temple_dispenser.json"}, "minecraft:dispenser"},
		{worldgen.MobSpawner{Entity: "minecraft:cave_spider"}, "minecraft:spawner"},
		{byName("minecraft:end_portal_frame", map[string]any{"minecraft:cardinal_direction": "north", "end_portal_eye_bit": uint8(1)}), "minecraft:end_portal_frame"},
		{byName("minecraft:infested_stone_bricks", nil), "minecraft:infested_stone_bricks"},
		{byName("minecraft:iron_chain", map[string]any{"pillar_axis": "y"}), "minecraft:iron_chain"},
		{byName("minecraft:tripwire_hook", map[string]any{"direction": int32(0), "attached_bit": uint8(1), "powered_bit": uint8(0)}), "minecraft:tripwire_hook"},
		{byName("minecraft:trip_wire", map[string]any{"attached_bit": uint8(1), "disarmed_bit": uint8(0), "powered_bit": uint8(0), "suspended_bit": uint8(0),
			"minecraft:connection_east": uint8(0), "minecraft:connection_north": uint8(0), "minecraft:connection_south": uint8(0), "minecraft:connection_west": uint8(0)}), "minecraft:tripwire"},
		{byName("minecraft:sticky_piston", map[string]any{"facing_direction": int32(1)}), "minecraft:sticky_piston"},
		{byName("minecraft:unpowered_repeater", map[string]any{"minecraft:cardinal_direction": "north", "repeater_delay": int32(0)}), "minecraft:repeater"},
	} {
		require.Equal(t, c.java, javaOf(c.b), "%#v", c.b)
	}
	require.True(t, needsEntity(worldgen.FlowerPot{Plant: "minecraft:red_mushroom"}))
	require.False(t, needsEntity(worldgen.FlowerPot{}))

	// Every own worldgen block type maps to a Java block, not air.
	n := 0
	for _, b := range world.Blocks() {
		switch b.(type) {
		case worldgen.OwnBlock, worldgen.Brushable, worldgen.FlowerPot, worldgen.Dispenser, worldgen.MobSpawner:
			n++
			name, _ := b.EncodeBlock()
			require.NotEqual(t, "minecraft:air", javaOf(b), "%s %#v", name, b)
		}
	}
	require.Greater(t, n, 0)
	t.Logf("%d own block states map to Java", n)
}
