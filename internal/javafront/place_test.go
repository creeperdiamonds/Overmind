package javafront_test

import (
	"context"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// blockAt is the block the server has at pos.
func blockAt(t *testing.T, srv *testserver.Server, pos cube.Pos) world.Block {
	t.Helper()
	b, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (world.Block, error) {
		return tx.Block(pos), nil
	})
	require.NoError(t, err)
	return b
}

func setBlock(t *testing.T, srv *testserver.Server, pos cube.Pos, b world.Block) {
	t.Helper()
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(pos, b, nil)
		return struct{}{}, nil
	})
	require.NoError(t, err)
}

// holdJava puts s in the Java player's first hotbar slot, which it holds.
func holdJava(t *testing.T, srv *testserver.Server, name string, s item.Stack) {
	t.Helper()
	onJava(t, srv, name, func(p *player.Player) {
		_ = p.SetHeldSlot(0)
		_ = p.Inventory().SetItem(0, s)
	})
}

// seenByBoth waits until both bots have been sent the server's block at pos:
// Bedrock its runtime ID, Java the Java state for it.
func seenByBoth(t *testing.T, srv *testserver.Server, j *javaBot, b *bedrock.Bot, pos cube.Pos) world.Block {
	t.Helper()
	var want world.Block
	require.Eventually(t, func() bool {
		want = blockAt(t, srv, pos)
		rid := world.BlockRuntimeID(want)
		got, ok := b.Block(pos)
		js := j.block(pos).State
		return ok && got == rid && js != nil && *js == javaStateOf(want)
	}, 5*time.Second, 50*time.Millisecond, "the two editions were not both shown the block at %v", pos)
	return want
}

// javaStateOf is the Java state a Java client should get for a block: the
// tables' state, in its colour for a bed or banner, whose Bedrock state has
// none (the tables give the black one).
func javaStateOf(b world.Block) int32 {
	s := java261.JavaState(world.BlockRuntimeID(b))
	var c item.Colour
	switch v := b.(type) {
	case block.Bed:
		c = v.Colour
	case block.Banner:
		c = v.Colour
	default:
		return int32(s)
	}
	if rest, ok := strings.CutPrefix(java261.BlockName(s), "minecraft:black_"); ok {
		s, _ = java261.AsBlock(s, "minecraft:"+c.String()+"_"+rest)
	}
	return int32(s)
}

// A Java player places blocks through Dragonfly's own item use, so placement
// rules, orientation and replacing are the server's, and both editions see the
// same block.
func TestJavaPlacesBlocks(t *testing.T) {
	srv := testserver.Start(t) // survival
	watcher, err := bedrock.Join(srv.BedrockAddr, "BLooker")
	require.NoError(t, err)
	defer watcher.Close()
	j := startBot(t, srv, "Builder")
	waitOnGround(t, srv, "Builder")
	require.Eventually(t, func() bool { return j.column(-3, 4, -61, -61).Loaded && j.column(6, 0, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)

	oak := block.Planks{Wood: block.OakWood()}
	seq := 10
	// place holds s (unless it is empty: the held item stays), looks and
	// places, and waits for the acknowledgement.
	place := func(t *testing.T, s item.Stack, yaw, pitch float64, on cube.Pos, face cube.Face, cx, cy, cz float64) {
		t.Helper()
		if !s.Empty() {
			holdJava(t, srv, "Builder", s)
		}
		j.look(yaw, pitch)
		time.Sleep(150 * time.Millisecond) // the look reaches the server first
		seq++
		j.place(on, face, cx, cy, cz, seq)
		require.Eventually(t, func() bool {
			for _, a := range j.block(on).Acks {
				if a == seq {
					return true
				}
			}
			return false
		}, 5*time.Second, 50*time.Millisecond, "place %d was not acknowledged", seq)
	}
	heldCount := func(t *testing.T) int {
		n := 0
		onJava(t, srv, "Builder", func(p *player.Player) { h, _ := p.HeldItems(); n = h.Count() })
		return n
	}

	t.Run("planks on the ground", func(t *testing.T) {
		place(t, item.NewStack(oak, 5), 0, 45, cube.Pos{2, -61, 0}, cube.FaceUp, 0.5, 1, 0.5)
		require.Equal(t, oak, seenByBoth(t, srv, j, watcher, cube.Pos{2, -60, 0}))
		require.Equal(t, 4, heldCount(t), "a survival place takes one item")
		require.Eventually(t, func() bool { s := j.inventory().Slots[wHotbar]; return s != nil && s.Count == 4 }, 5*time.Second, 50*time.Millisecond, "the client was not shown the count")
	})
	t.Run("stairs face the way the player looks", func(t *testing.T) {
		place(t, item.NewStack(block.Stairs{Block: oak}, 1), 0, 45, cube.Pos{0, -61, 2}, cube.FaceUp, 0.5, 1, 0.5)
		got := seenByBoth(t, srv, j, watcher, cube.Pos{0, -60, 2})
		s, ok := got.(block.Stairs)
		require.True(t, ok, "got %#v", got)
		require.Equal(t, cube.South, s.Facing, "yaw 0 faces south (+Z)")
		require.False(t, s.UpsideDown)
	})
	t.Run("stairs on the top half of a side are upside down", func(t *testing.T) {
		place(t, item.NewStack(block.Stairs{Block: oak}, 1), 90, 0, cube.Pos{2, -60, 0}, cube.FaceWest, 0, 0.8, 0.5)
		got := seenByBoth(t, srv, j, watcher, cube.Pos{1, -60, 0})
		s, ok := got.(block.Stairs)
		require.True(t, ok, "got %#v", got)
		require.True(t, s.UpsideDown)
		require.Equal(t, cube.West, s.Facing, "yaw 90 faces west (-X)")
	})
	t.Run("a log lies along the face it was put on", func(t *testing.T) {
		place(t, item.NewStack(block.Log{Wood: block.OakWood()}, 1), -90, 0, cube.Pos{2, -60, 0}, cube.FaceEast, 1, 0.5, 0.5)
		got := seenByBoth(t, srv, j, watcher, cube.Pos{3, -60, 0})
		require.Equal(t, block.Log{Wood: block.OakWood(), Axis: cube.X}, got)
	})
	t.Run("slabs: bottom on the ground, top on the upper half of a side", func(t *testing.T) {
		place(t, item.NewStack(block.Slab{Block: oak}, 2), 0, 45, cube.Pos{-2, -61, 0}, cube.FaceUp, 0.5, 1, 0.5)
		require.Equal(t, block.Slab{Block: oak}, seenByBoth(t, srv, j, watcher, cube.Pos{-2, -60, 0}))
		place(t, item.NewStack(block.Slab{Block: oak}, 2), 180, 0, cube.Pos{2, -60, 0}, cube.FaceNorth, 0.5, 0.75, 0)
		require.Equal(t, block.Slab{Block: oak, Top: true}, seenByBoth(t, srv, j, watcher, cube.Pos{2, -60, -1}))
	})
	t.Run("a door takes two blocks", func(t *testing.T) {
		place(t, item.NewStack(block.WoodDoor{Wood: block.OakWood()}, 1), 0, 45, cube.Pos{-2, -61, -2}, cube.FaceUp, 0.5, 1, 0.5)
		low, ok := seenByBoth(t, srv, j, watcher, cube.Pos{-2, -60, -2}).(block.WoodDoor)
		require.True(t, ok)
		require.False(t, low.Top)
		high, ok := seenByBoth(t, srv, j, watcher, cube.Pos{-2, -59, -2}).(block.WoodDoor)
		require.True(t, ok)
		require.True(t, high.Top)
		require.Equal(t, low.Facing, high.Facing)
	})
	t.Run("a bed takes two blocks", func(t *testing.T) {
		place(t, item.NewStack(block.Bed{Colour: item.ColourRed()}, 1), 0, 45, cube.Pos{-3, -61, 3}, cube.FaceUp, 0.5, 1, 0.5)
		foot, ok := seenByBoth(t, srv, j, watcher, cube.Pos{-3, -60, 3}).(block.Bed)
		require.True(t, ok)
		require.False(t, foot.Head)
		head, ok := seenByBoth(t, srv, j, watcher, cube.Pos{-3, -60, 4}).(block.Bed)
		require.True(t, ok, "the head goes the way the player looks")
		require.True(t, head.Head)
	})
	t.Run("short grass is replaced in place", func(t *testing.T) {
		setBlock(t, srv, cube.Pos{0, -60, -2}, block.ShortGrass{})
		place(t, item.NewStack(oak, 1), 180, 45, cube.Pos{0, -60, -2}, cube.FaceUp, 0.5, 0.5, 0.5)
		require.Equal(t, oak, seenByBoth(t, srv, j, watcher, cube.Pos{0, -60, -2}))
		require.IsType(t, block.Air{}, blockAt(t, srv, cube.Pos{0, -59, -2}))
	})
	t.Run("a place inside the player is refused and the client corrected", func(t *testing.T) {
		holdJava(t, srv, "Builder", item.NewStack(oak, 3))
		require.Eventually(t, func() bool { s := j.inventory().Slots[wHotbar]; return s != nil && s.Count == 3 }, 5*time.Second, 50*time.Millisecond)
		time.Sleep(200 * time.Millisecond)
		before := j.inventory().State
		place(t, item.Stack{}, 0, 90, cube.Pos{0, -61, 0}, cube.FaceUp, 0.5, 1, 0.5)
		require.IsType(t, block.Air{}, blockAt(t, srv, cube.Pos{0, -60, 0}))
		require.Equal(t, 3, heldCount(t))
		require.Eventually(t, func() bool {
			inv := j.inventory()
			return inv.State != before && inv.Slots[wHotbar] != nil && inv.Slots[wHotbar].Count == 3
		}, 5*time.Second, 50*time.Millisecond, "the client was not sent its held item back")
	})
	t.Run("past Java's reach is refused, though Dragonfly's own is longer", func(t *testing.T) {
		place(t, item.NewStack(oak, 3), -90, 20, cube.Pos{6, -61, 0}, cube.FaceUp, 0.5, 1, 0.5)
		require.IsType(t, block.Air{}, blockAt(t, srv, cube.Pos{6, -60, 0}))
		require.Equal(t, 3, heldCount(t))
	})
}

// Items used in the air go through Dragonfly too: food is eaten (and finished
// by the server, as a Java client sends nothing when it is done), and buckets
// find their fluid where the player looks, as Java's do.
func TestJavaUsesItemsInTheAir(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "BLooker")
	require.NoError(t, err)
	defer watcher.Close()
	j := startBot(t, srv, "User")
	waitOnGround(t, srv, "User")
	require.Eventually(t, func() bool { return j.column(2, 0, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)

	t.Run("bread is eaten", func(t *testing.T) {
		onJava(t, srv, "User", func(p *player.Player) { p.SetFood(10) })
		holdJava(t, srv, "User", item.NewStack(item.Bread{}, 3))
		j.useItem(1)
		require.Eventually(t, func() bool {
			food, bread := 0, 0
			onJava(t, srv, "User", func(p *player.Player) { food = p.Food(); h, _ := p.HeldItems(); bread = h.Count() })
			return food > 10 && bread == 2
		}, 5*time.Second, 50*time.Millisecond, "the bread was not eaten")
		time.Sleep(2500 * time.Millisecond)
		bread := 0
		onJava(t, srv, "User", func(p *player.Player) { h, _ := p.HeldItems(); bread = h.Count() })
		require.Equal(t, 2, bread, "the server ate on after the client was done")
	})
	t.Run("a water bucket empties where the player looks", func(t *testing.T) {
		holdJava(t, srv, "User", item.NewStack(item.Bucket{Content: item.LiquidBucketContent(block.Water{Still: true, Depth: 8})}, 1))
		// From eyes at (0.5, -58.38, 0.5), at the middle of the top of the grass
		// at (2, -61, 0), which is at Y -60.
		j.look(lookAt(mgl64.Vec3{0.5, -58.38, 0.5}, mgl64.Vec3{2.5, -60, 0.5}))
		time.Sleep(150 * time.Millisecond)
		j.place(cube.Pos{2, -61, 0}, cube.FaceUp, 0.5, 1, 0.5, 2) // what a client sends first; a bucket does nothing on a block
		j.useItem(3)
		target := cube.Pos{2, -60, 0}
		require.Eventually(t, func() bool { _, ok := blockAt(t, srv, target).(block.Water); return ok }, 5*time.Second, 50*time.Millisecond, "no water")
		w := seenByBoth(t, srv, j, watcher, target).(block.Water)
		require.Equal(t, 8, w.Depth, "a source")
		var held item.Stack
		onJava(t, srv, "User", func(p *player.Player) { held, _ = p.HeldItems() })
		b, ok := held.Item().(item.Bucket)
		require.True(t, ok && b.Empty(), "the bucket was not emptied: %v", held)

		// And the empty bucket takes it back.
		j.useItem(4)
		require.Eventually(t, func() bool { _, ok := blockAt(t, srv, target).(block.Air); return ok }, 5*time.Second, 50*time.Millisecond, "the water stayed")
		onJava(t, srv, "User", func(p *player.Player) { held, _ = p.HeldItems() })
		b, ok = held.Item().(item.Bucket)
		require.True(t, ok && !b.Empty(), "the bucket was not filled: %v", held)
	})
	t.Run("an empty bucket looks past flowing water to a source", func(t *testing.T) {
		source := cube.Pos{3, -60, 0}
		setBlock(t, srv, source, block.Water{Still: true, Depth: 8})
		require.Eventually(t, func() bool {
			w, ok := blockAt(t, srv, cube.Pos{1, -60, 0}).(block.Water)
			return ok && w.Depth < 8
		}, 5*time.Second, 50*time.Millisecond, "the water did not spread")
		holdJava(t, srv, "User", item.NewStack(item.Bucket{}, 1))
		// Down into the flowing water at (1, -60, 0), on through (2, -60, 0) to
		// the source.
		j.look(lookAt(mgl64.Vec3{0.5, -58.38, 0.5}, mgl64.Vec3{3.2, -59.9, 0.5}))
		time.Sleep(150 * time.Millisecond)
		j.useItem(5)
		require.Eventually(t, func() bool {
			var held item.Stack
			onJava(t, srv, "User", func(p *player.Player) { held, _ = p.HeldItems() })
			b, ok := held.Item().(item.Bucket)
			return ok && !b.Empty()
		}, 5*time.Second, 50*time.Millisecond, "the bucket was not filled")
		_, still := blockAt(t, srv, source).(block.Water)
		require.False(t, still && blockAt(t, srv, source).(block.Water).Depth == 8, "the source was not taken")
	})
	t.Run("a bow shoots when released", func(t *testing.T) {
		onJava(t, srv, "User", func(p *player.Player) { _ = p.Inventory().SetItem(1, item.NewStack(item.Arrow{}, 16)) })
		holdJava(t, srv, "User", item.NewStack(item.Bow{}, 1))
		j.look(-90, -10)
		time.Sleep(150 * time.Millisecond)
		j.useItem(6)
		require.Eventually(t, func() bool { return state(t, srv, "User", true, (*player.Player).UsingItem) }, 5*time.Second, 50*time.Millisecond, "the bow was not drawn")
		time.Sleep(time.Second)
		j.release()
		require.Eventually(t, func() bool {
			n := 0
			onJava(t, srv, "User", func(p *player.Player) { n = count(p, item.Arrow{}) })
			return n == 15
		}, 5*time.Second, 50*time.Millisecond, "no arrow was shot")
		require.False(t, state(t, srv, "User", true, (*player.Player).UsingItem))
	})
}

// lookAt is the Java yaw and pitch from eye to a point.
func lookAt(eye, at mgl64.Vec3) (yaw, pitch float64) {
	d := at.Sub(eye)
	yaw = math.Atan2(-d[0], d[2]) * 180 / math.Pi
	pitch = -math.Atan2(d[1], math.Hypot(d[0], d[2])) * 180 / math.Pi
	return yaw, pitch
}

// A block entity with no data (Dragonfly's stand-in for a block it does not
// implement, set without NBT) reaches both editions and is saved without a
// panic. Upstream, Bedrock's chunk encoding and the world save both wrote
// coordinates into its nil data.
func TestBlockEntityWithoutDataIsSafe(t *testing.T) {
	srv := testserver.Start(t) // registers and finalises the blocks
	var bare world.Block
	for _, b := range world.Blocks() {
		if n, ok := b.(world.NBTer); ok && n.EncodeNBT() == nil {
			bare = b
			break
		}
	}
	require.NotNil(t, bare, "no block entity without data to test with")
	pos := cube.Pos{3, -60, 3}
	setBlock(t, srv, pos, bare)

	watcher, err := bedrock.Join(srv.BedrockAddr, "BLooker")
	require.NoError(t, err)
	defer watcher.Close()
	j := startBot(t, srv, "Looker")
	waitOnGround(t, srv, "Looker")
	require.Eventually(t, func() bool { return j.column(3, 3, -60, -60).Loaded }, 20*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { return watcher.ChunksReceived() > 0 }, 20*time.Second, 50*time.Millisecond)
	// As a real client does, it asks for the sub-chunk, block entities and all.
	require.NoError(t, watcher.RequestSubChunk(0, int32(pos[1]>>4), 0))
	require.Eventually(t, func() bool { return watcher.SubChunksReceived() > 0 }, 5*time.Second, 50*time.Millisecond, "no sub-chunk")
	time.Sleep(time.Second)
	require.Empty(t, watcher.Ended(), "the Bedrock player was dropped")

	require.NoError(t, srv.Close(), "the world did not save")
}
