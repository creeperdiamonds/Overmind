package javafront_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// clientBlock is a block as the bot has it, its properties decoded by
// prismarine-block.
type clientBlock struct {
	Loaded bool           `json:"loaded"`
	State  int32          `json:"state"`
	Name   string         `json:"name"`
	Props  map[string]any `json:"props"`
}

func (b *javaBot) blockState(pos cube.Pos) clientBlock {
	var r clientBlock
	b.call("blockState", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2]}, &r)
	return r
}

// has reports whether the block is name with every property given.
func (cb clientBlock) has(name string, props map[string]any) bool {
	if cb.Name != name {
		return false
	}
	for k, v := range props {
		if fmt.Sprint(cb.Props[k]) != fmt.Sprint(v) {
			return false
		}
	}
	return true
}

// shows waits until the bot shows the block at pos as name with props.
func shows(t *testing.T, j *javaBot, pos cube.Pos, name string, props map[string]any) clientBlock {
	t.Helper()
	var got clientBlock
	require.Eventually(t, func() bool { got = j.blockState(pos); return got.has(name, props) }, 5*time.Second, 50*time.Millisecond,
		"%v: want %s %v", pos, name, props)
	return got
}

// setWith sets a block with options.
func setWith(t *testing.T, srv *testserver.Server, pos cube.Pos, b world.Block, opts *world.SetOpts) {
	t.Helper()
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(pos, b, opts)
		return struct{}{}, nil
	})
	require.NoError(t, err)
}

// Beds and banners reach Java in their own colour. Bedrock keeps the colour in
// the block entity, not the block state, which Java has as the block itself
// (red_bed, light_blue_wall_banner): the tables alone gave every one the
// black block. Seen as they are set (block updates) and by a Java player who
// joins later (chunks).
func TestJavaSeesBedAndBannerColours(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JColours")
	waitOnGround(t, srv, "JColours")
	require.Eventually(t, func() bool { return j.column(-2, -2, -61, -61).Loaded && j.column(2, 2, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	still := &world.SetOpts{DisableBlockUpdates: true}
	type check struct {
		pos   cube.Pos
		name  string
		props map[string]any
	}
	var checks []check
	for i, c := range []item.Colour{item.ColourRed(), item.ColourLightBlue(), item.ColourWhite(), item.ColourLime(), item.ColourBlack()} {
		x := i - 2
		bed := cube.Pos{x, -60, 2}
		setWith(t, srv, bed, block.Bed{Colour: c, Facing: cube.North, Head: true}, still)
		checks = append(checks, check{bed, c.String() + "_bed", map[string]any{"part": "head"}})
		standing := cube.Pos{x, -60, -2}
		setWith(t, srv, standing, block.Banner{Colour: c, Attach: block.StandingAttachment(4)}, still)
		checks = append(checks, check{standing, c.String() + "_banner", nil})
		setWith(t, srv, cube.Pos{x, -59, -3}, block.Stone{}, still)
		wall := cube.Pos{x, -59, -2}
		setWith(t, srv, wall, block.Banner{Colour: c, Attach: block.WallAttachment(cube.South)}, still)
		checks = append(checks, check{wall, c.String() + "_wall_banner", nil})
	}
	for _, c := range checks {
		shows(t, j, c.pos, c.name, c.props)
	}
	late := startBot(t, srv, "JColoursLate")
	require.Eventually(t, func() bool { return late.column(-2, -2, -61, -61).Loaded && late.column(2, 2, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	for _, c := range checks {
		shows(t, late, c.pos, c.name, c.props)
	}
}

// Blocks whose Java state depends on their neighbours, on the water in them or
// on their block entity reach Java as a Java client expects: fences, panes and
// wire connect, a double chest has its two halves, a door and tall grass have
// both halves, and a block in water is waterlogged. A Java player sees them as
// they change (block updates) and a Java player who joins later sees them in
// its chunks; Bedrock sees the same blocks.
func TestJavaSeesConnectedBlocks(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "BConnSeer")
	require.NoError(t, err)
	defer watcher.Close()
	j := startBot(t, srv, "JBuilds")
	waitOnGround(t, srv, "JBuilds")
	require.Eventually(t, func() bool { return j.column(-3, -3, -61, -61).Loaded && j.column(3, 3, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)

	seq := 0
	place := func(t *testing.T, s item.Stack, yaw float64, on cube.Pos) {
		t.Helper()
		if !s.Empty() {
			holdJava(t, srv, "JBuilds", s)
		}
		j.look(yaw, 45)
		time.Sleep(150 * time.Millisecond)
		seq++
		j.place(on, cube.FaceUp, 0.5, 1, 0.5, seq)
		acked(t, j, seq)
	}
	f := false
	tr := true
	type check struct {
		pos   cube.Pos
		name  string
		props map[string]any
	}
	var checks []check
	expect := func(t *testing.T, pos cube.Pos, name string, props map[string]any) {
		t.Helper()
		shows(t, j, pos, name, props)
		checks = append(checks, check{pos, name, props})
	}

	t.Run("fences connect to each other and to stone", func(t *testing.T) {
		fence := item.NewStack(block.WoodFence{Wood: block.OakWood()}, 4)
		place(t, fence, 0, cube.Pos{2, -61, -2})
		place(t, fence, 0, cube.Pos{3, -61, -2})
		setBlock(t, srv, cube.Pos{2, -60, -3}, block.Stone{})
		expect(t, cube.Pos{2, -60, -2}, "oak_fence", map[string]any{"east": tr, "north": tr, "south": f, "west": f, "waterlogged": f})
		expect(t, cube.Pos{3, -60, -2}, "oak_fence", map[string]any{"east": f, "north": f, "south": f, "west": tr})
	})
	t.Run("a pane connects to stone", func(t *testing.T) {
		place(t, item.NewStack(block.GlassPane{}, 1), 0, cube.Pos{-2, -61, -2})
		setBlock(t, srv, cube.Pos{-3, -60, -2}, block.Stone{})
		expect(t, cube.Pos{-2, -60, -2}, "glass_pane", map[string]any{"west": tr, "east": f, "north": f, "south": f})
	})
	t.Run("redstone wire runs as a line", func(t *testing.T) {
		wire := item.NewStack(block.RedstoneWire{}, 2)
		place(t, wire, 0, cube.Pos{-2, -61, 2})
		place(t, wire, 0, cube.Pos{-2, -61, 3})
		for _, z := range []int{2, 3} {
			expect(t, cube.Pos{-2, -60, z}, "redstone_wire", map[string]any{"north": "side", "south": "side", "east": "none", "west": "none"})
		}
	})
	t.Run("two chests make a double chest", func(t *testing.T) {
		chest := item.NewStack(block.NewChest(), 2)
		place(t, chest, 0, cube.Pos{2, -61, 2})
		place(t, chest, 0, cube.Pos{3, -61, 2})
		a := shows(t, j, cube.Pos{2, -60, 2}, "chest", map[string]any{"type": "left"})
		b := shows(t, j, cube.Pos{3, -60, 2}, "chest", map[string]any{"type": "right"})
		// Placed looking south, so they face north, the way the player looks
		// from: left has its partner clockwise of north, at +X.
		require.Equal(t, "north", a.Props["facing"])
		require.Equal(t, "north", b.Props["facing"])
		checks = append(checks, check{cube.Pos{2, -60, 2}, "chest", map[string]any{"type": "left", "facing": "north"}},
			check{cube.Pos{3, -60, 2}, "chest", map[string]any{"type": "right", "facing": "north"}})
	})
	t.Run("a block in water is waterlogged", func(t *testing.T) {
		still := &world.SetOpts{DisableBlockUpdates: true} // the water stays put
		pos := cube.Pos{0, -60, -4}
		setWith(t, srv, pos, block.Water{Still: true, Depth: 8}, still)
		setWith(t, srv, pos, block.Stairs{Block: block.Planks{Wood: block.OakWood()}}, still)
		expect(t, pos, "oak_stairs", map[string]any{"waterlogged": tr})
		// And dry again when the water goes.
		setWith(t, srv, cube.Pos{1, -60, -4}, block.Stairs{Block: block.Planks{Wood: block.OakWood()}}, still)
		expect(t, cube.Pos{1, -60, -4}, "oak_stairs", map[string]any{"waterlogged": f})
	})
	t.Run("a door has both halves, and opens as one", func(t *testing.T) {
		place(t, item.NewStack(block.WoodDoor{Wood: block.OakWood()}, 1), 0, cube.Pos{0, -61, 3})
		low := shows(t, j, cube.Pos{0, -60, 3}, "oak_door", map[string]any{"half": "lower", "open": f})
		high := shows(t, j, cube.Pos{0, -59, 3}, "oak_door", map[string]any{"half": "upper", "open": f})
		require.Equal(t, low.Props["facing"], high.Props["facing"])
		require.Equal(t, low.Props["hinge"], high.Props["hinge"])
		holdJava(t, srv, "JBuilds", item.Stack{})
		seq++
		j.place(cube.Pos{0, -60, 3}, cube.FaceNorth, 0.5, 0.5, 0, seq)
		acked(t, j, seq)
		expect(t, cube.Pos{0, -60, 3}, "oak_door", map[string]any{"half": "lower", "open": tr, "facing": low.Props["facing"]})
		expect(t, cube.Pos{0, -59, 3}, "oak_door", map[string]any{"half": "upper", "open": tr, "facing": low.Props["facing"], "hinge": low.Props["hinge"]})
	})
	t.Run("tall grass has both halves", func(t *testing.T) {
		still := &world.SetOpts{DisableBlockUpdates: true}
		setWith(t, srv, cube.Pos{-3, -60, 0}, block.DoubleTallGrass{}, still)
		setWith(t, srv, cube.Pos{-3, -59, 0}, block.DoubleTallGrass{UpperPart: true}, still)
		expect(t, cube.Pos{-3, -60, 0}, "tall_grass", map[string]any{"half": "lower"})
		expect(t, cube.Pos{-3, -59, 0}, "tall_grass", map[string]any{"half": "upper"})
	})

	// Bedrock is shown the same blocks, by its own runtime IDs.
	for _, c := range checks {
		want := world.BlockRuntimeID(blockAt(t, srv, c.pos))
		require.Eventually(t, func() bool { got, ok := watcher.Block(c.pos); return ok && got == want }, 5*time.Second, 50*time.Millisecond, "Bedrock at %v", c.pos)
	}
	// A Java player who comes later gets all of it in its chunks.
	late := startBot(t, srv, "JLater")
	require.Eventually(t, func() bool { return late.column(-3, -4, -61, -61).Loaded && late.column(3, 3, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	for _, c := range checks {
		shows(t, late, c.pos, c.name, c.props)
	}
}
