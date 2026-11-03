package java261

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Property and WithProperty agree with the state rule worked out separately
// (javaBlock.state), for every value of every property of every Java block.
func TestPropertiesMatchStateRule(t *testing.T) {
	checked := 0
	for name, b := range loadJava(t) {
		def := uint16(b.DefaultState)
		for _, st := range b.States {
			values := st.Values
			if st.Type == "bool" {
				values = []string{"true", "false"}
			}
			for _, v := range values {
				want := b.state(t, map[string]string{st.Name: v})
				got, ok := WithProperty(def, st.Name, v)
				require.True(t, ok, "%s %s=%s", name, st.Name, v)
				require.Equal(t, want, got, "%s %s=%s", name, st.Name, v)
				back, ok := Property(got, st.Name)
				require.True(t, ok)
				require.Equal(t, v, back, "%s %s", name, st.Name)
				checked++
			}
		}
		if len(b.States) > 0 {
			require.Equal(t, name, BlockName(uint16(b.MaxStateID)), "last state of %s", name)
			require.Equal(t, name, BlockName(uint16(b.MinStateID)), "first state of %s", name)
		}
	}
	require.Greater(t, checked, 3000)
	_, ok := WithProperty(1, "facing", "north") // stone has none
	require.False(t, ok)
	_, ok = WithProperty(uint16(loadJava(t)["minecraft:oak_fence"].DefaultState), "north", "maybe")
	require.False(t, ok)
}

// Connections Dragonfly works out for Bedrock 1.26 (newer than the pinned
// mapping) reach the Java state: fences, panes, bars, fire, chorus plants and
// redstone wire, which used to collapse to one Java state each.
func TestNewerPropertiesReachJava(t *testing.T) {
	java := loadJava(t)
	conn := func(n, e, s, w bool) map[string]any {
		return map[string]any{"minecraft:connection_north": n, "minecraft:connection_east": e, "minecraft:connection_south": s, "minecraft:connection_west": w}
	}
	for _, c := range []struct {
		bedrock string
		props   map[string]any
		java    string
		want    map[string]string
	}{
		{"minecraft:oak_fence", conn(true, true, false, false), "minecraft:oak_fence",
			map[string]string{"north": "true", "east": "true", "south": "false", "west": "false", "waterlogged": "false"}},
		{"minecraft:nether_brick_fence", conn(false, false, true, true), "minecraft:nether_brick_fence",
			map[string]string{"north": "false", "east": "false", "south": "true", "west": "true"}},
		{"minecraft:glass_pane", conn(true, false, true, false), "minecraft:glass_pane",
			map[string]string{"north": "true", "east": "false", "south": "true", "west": "false"}},
		{"minecraft:red_stained_glass_pane", conn(false, true, false, true), "minecraft:red_stained_glass_pane",
			map[string]string{"north": "false", "east": "true", "south": "false", "west": "true"}},
		{"minecraft:iron_bars", conn(true, true, true, true), "minecraft:iron_bars",
			map[string]string{"north": "true", "east": "true", "south": "true", "west": "true"}},
		{"minecraft:copper_bars", conn(false, true, false, false), "minecraft:copper_bars",
			map[string]string{"north": "false", "east": "true", "south": "false", "west": "false"}},
	} {
		got := JavaState(rid(c.bedrock, c.props))
		want := java[c.java].state(t, c.want)
		require.Equal(t, want, got, "%s %v", c.bedrock, c.props)
	}

	fire := conn(false, true, false, false)
	fire["minecraft:connection_up"] = true
	fire["age"] = int32(3)
	require.Equal(t, java["minecraft:fire"].state(t, map[string]string{"age": "3", "north": "false", "east": "true", "south": "false", "west": "false", "up": "true"}),
		JavaState(rid("minecraft:fire", fire)))

	chorus := conn(true, false, false, false)
	chorus["minecraft:connection_up"] = false
	chorus["minecraft:connection_down"] = true
	require.Equal(t, java["minecraft:chorus_plant"].state(t, map[string]string{"north": "true", "east": "false", "south": "false", "west": "false", "up": "false", "down": "true"}),
		JavaState(rid("minecraft:chorus_plant", chorus)))

	wire := map[string]any{"redstone_north": "up", "redstone_east": "side", "redstone_south": "side", "redstone_west": "none", "redstone_signal": int32(9)}
	require.Equal(t, java["minecraft:redstone_wire"].state(t, map[string]string{"north": "up", "east": "side", "south": "side", "west": "none", "power": "9"}),
		JavaState(rid("minecraft:redstone_wire", wire)))
}
