package javafront_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/config"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// bridgeEdge digs a pit three deep beside the block the Java player stands on
// at spawn, (0, -61, 0), to the south (+Z) and the west (-X), so the player
// stands at an edge with only air in reach past it.
func bridgeEdge(t *testing.T, srv *testserver.Server) {
	t.Helper()
	_, err := world.Call(t.Context(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		for y := -63; y <= -61; y++ {
			for x := -3; x <= 3; x++ {
				for z := -3; z <= 6; z++ {
					if (z != 0 && x >= -2 && x <= 2) || (x <= -1 && z <= 2) {
						tx.SetBlock(cube.Pos{x, y, z}, nil, nil)
					}
				}
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)
}

// bridgeBot joins a Java bot, waits until it stands at spawn on (0, -61, 0)
// with the pit dug, and returns it.
func bridgeBot(t *testing.T, srv *testserver.Server, name string) *javaBot {
	t.Helper()
	j := startBot(t, srv, name)
	waitOnGround(t, srv, name)
	pos, _, _ := srv.JavaPosition(name)
	require.Equal(t, cube.Pos{0, -60, 0}, cube.PosFromVec3(pos), "the bot is not standing at spawn")
	require.Eventually(t, func() bool { return j.column(-3, 6, -61, -61).Loaded && j.column(3, -3, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	bridgeEdge(t, srv)
	require.Eventually(t, func() bool { return j.block(cube.Pos{0, -61, 1}).Name == "air" }, 5*time.Second, 50*time.Millisecond, "the client was not shown the pit")
	return j
}

// useInAir looks, sends use_item and waits for its acknowledgement.
func useInAir(t *testing.T, j *javaBot, yaw, pitch float64, seq int) {
	t.Helper()
	j.look(yaw, pitch)
	time.Sleep(150 * time.Millisecond)
	j.useItem(seq)
	require.Eventually(t, func() bool {
		for _, a := range j.block(cube.Pos{}).Acks {
			if a == seq {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "use_item %d was not acknowledged", seq)
	time.Sleep(200 * time.Millisecond) // anything the server did has reached the client
}

// shownToJava waits until the client was sent the server's block at pos.
func shownToJava(t *testing.T, srv *testserver.Server, j *javaBot, pos cube.Pos) world.Block {
	t.Helper()
	var want world.Block
	require.Eventually(t, func() bool {
		want = blockAt(t, srv, pos)
		js := j.block(pos).State
		return js != nil && *js == javaStateOf(want)
	}, 5*time.Second, 50*time.Millisecond, "the client was not shown the block at %v (server has %#v)", pos, want)
	return want
}

func heldCountOf(t *testing.T, srv *testserver.Server, name string) int {
	t.Helper()
	n := 0
	onJava(t, srv, name, func(p *player.Player) { h, _ := p.HeldItems(); n = h.Count() })
	return n
}

// A Java player bridges as a Bedrock player does (JavaBedrockBridging, on by
// default): standing at an edge and using a block while aiming at air, looking
// down at the cell past the edge, places it against the block stood on, on the
// face the player faces.
func TestJavaBedrockBridging(t *testing.T) {
	require.True(t, config.Default().JavaBedrockBridging, "JavaBedrockBridging is on by default")
	srv := testserver.Start(t) // survival
	j := bridgeBot(t, srv, "Bridger")
	oak := block.Planks{Wood: block.OakWood()}
	south, west, north := cube.Pos{0, -61, 1}, cube.Pos{-1, -61, 0}, cube.Pos{0, -61, -1}

	t.Run("an item that is not a block places nothing", func(t *testing.T) {
		holdJava(t, srv, "Bridger", item.NewStack(item.Stick{}, 3))
		useInAir(t, j, 0, 60, 1)
		require.IsType(t, block.Air{}, blockAt(t, srv, south))
		require.Equal(t, 3, heldCountOf(t, srv, "Bridger"))
	})
	t.Run("looking up at the sky places nothing", func(t *testing.T) {
		holdJava(t, srv, "Bridger", item.NewStack(oak, 5))
		useInAir(t, j, 0, -30, 2)
		require.IsType(t, block.Air{}, blockAt(t, srv, south))
		require.Equal(t, 5, heldCountOf(t, srv, "Bridger"))
	})
	t.Run("looking straight down past the edge, at no cell beside it, places nothing", func(t *testing.T) {
		// Facing south but looking nearly straight down: the look meets the
		// block stood on, so the client would have placed on its top.
		useInAir(t, j, 0, 89, 3)
		require.IsType(t, block.Air{}, blockAt(t, srv, south))
		require.Equal(t, 5, heldCountOf(t, srv, "Bridger"))
	})
	t.Run("looking down past the edge places a block there", func(t *testing.T) {
		useInAir(t, j, 0, 60, 4)
		require.Equal(t, oak, shownToJava(t, srv, j, south), "yaw 0 faces south: the block goes at (0, -61, 1)")
		require.Equal(t, 4, heldCountOf(t, srv, "Bridger"), "a survival place takes one item")
		require.Eventually(t, func() bool { s := j.inventory().Slots[wHotbar]; return s != nil && s.Count == 4 }, 5*time.Second, 50*time.Millisecond, "the client was not shown the count")
		require.IsType(t, block.Air{}, blockAt(t, srv, cube.Pos{0, -62, 1}), "only one block")
	})
	t.Run("a held button repeats no faster than Java's 4 ticks", func(t *testing.T) {
		// West, then at once north: the second use comes inside the cooldown.
		j.look(90, 60)
		time.Sleep(300 * time.Millisecond)
		j.useItem(5)
		j.look(180, 60)
		j.useItem(6)
		require.Eventually(t, func() bool { _, ok := blockAt(t, srv, west).(block.Planks); return ok }, 5*time.Second, 50*time.Millisecond, "yaw 90 faces west: no block at (-1, -61, 0)")
		time.Sleep(300 * time.Millisecond)
		require.IsType(t, block.Air{}, blockAt(t, srv, north), "a use inside the cooldown placed")
		require.Equal(t, 3, heldCountOf(t, srv, "Bridger"))
		require.Equal(t, oak, shownToJava(t, srv, j, west))
		// Past the cooldown it places again.
		useInAir(t, j, 180, 60, 7)
		require.Equal(t, oak, shownToJava(t, srv, j, north), "yaw 180 faces north: the block goes at (0, -61, -1)")
		require.Equal(t, 2, heldCountOf(t, srv, "Bridger"))
	})
}

// With JavaBedrockBridging off, Java players place only as vanilla Java does.
func TestJavaBedrockBridgingOff(t *testing.T) {
	srv := testserver.StartWith(t, func(c *config.Config) { c.JavaBedrockBridging = false })
	j := bridgeBot(t, srv, "NoBridge")
	holdJava(t, srv, "NoBridge", item.NewStack(block.Planks{Wood: block.OakWood()}, 5))
	useInAir(t, j, 0, 60, 1)
	require.IsType(t, block.Air{}, blockAt(t, srv, cube.Pos{0, -61, 1}))
	require.Equal(t, 5, heldCountOf(t, srv, "NoBridge"))
}
