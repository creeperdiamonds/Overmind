package javafront_test

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/testserver"
)

// itemOnServer is where the one item entity in the world is, and what it holds.
func itemOnServer(t *testing.T, srv *testserver.Server) (pos mgl64.Vec3, s item.Stack, ok bool) {
	t.Helper()
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.ItemType {
				pos, s, ok = e.Position(), e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item(), true
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)
	return pos, s, ok
}

// count is how many of an item a player has in its inventory.
func count(p *player.Player, it world.Item) int {
	n := 0
	for _, s := range p.Inventory().Items() {
		if s.Comparable(item.NewStack(it, 1)) {
			n += s.Count()
		}
	}
	return n
}

// javaItemEntity waits until the Java bot is shown an item entity holding
// name:count, and returns its ID.
func javaItemEntity(t *testing.T, j *javaBot, name string, n int) int32 {
	t.Helper()
	var id int32 = -1
	require.Eventually(t, func() bool {
		for i, e := range j.entities().Entities {
			if e.Type != "item" {
				continue
			}
			if it := e.item(t); it != nil && it.Name == name && it.Count == n {
				id = i
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Java was never shown the item %s x%d", name, n)
	return id
}

func walkToItem(t *testing.T, srv *testserver.Server, goTo func(x, z float64)) {
	t.Helper()
	var pos mgl64.Vec3
	require.Eventually(t, func() bool {
		p, _, ok := itemOnServer(t, srv)
		pos = p
		return ok
	}, 5*time.Second, 50*time.Millisecond, "no item in the world")
	time.Sleep(time.Second) // let it land
	if p, _, ok := itemOnServer(t, srv); ok {
		pos = p
	}
	goTo(pos[0], pos[2])
}

// A Bedrock player drops an item. Java is shown it (entity, item, velocity),
// walks over it and picks it up by Dragonfly's rules, and both editions see the
// pickup.
func TestBedrockDropsJavaPicksUp(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BDropper")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JPicker")
	waitOnGround(t, srv, "JPicker")
	b.GoTo(6.5, 0.5)
	require.Eventually(t, func() bool { p, ok := srv.PlayerPosition("BDropper"); return ok && p.X() > 6 }, 10*time.Second, 50*time.Millisecond)
	javaID, ok := b.RuntimeIDOf("JPicker")
	require.Eventually(t, func() bool { javaID, ok = b.RuntimeIDOf("JPicker"); return ok }, 10*time.Second, 50*time.Millisecond)

	onBedrock(t, srv, "BDropper", func(p *player.Player) { p.Drop(item.NewStack(block.Dirt{}, 3)) })

	id := javaItemEntity(t, j, "dirt", 3)
	e := j.entities().Entities[id]
	// Dropped along the dropper's look (yaw 0: +Z), 0.4 blocks a tick.
	require.InDelta(t, 0.4, mgl64.Vec3{e.SpawnVelocity.X, e.SpawnVelocity.Y, e.SpawnVelocity.Z}.Len(), 0.01, "spawned without its velocity")
	require.NotNil(t, e.Velocity, "no velocity after the item")
	require.InDelta(t, 6.5, e.X, 3, "shown far from where it was dropped")
	require.Eventually(t, func() bool { return len(b.Items()) == 1 }, 5*time.Second, 50*time.Millisecond, "Bedrock was not shown the item")

	b.GoTo(-6.5, 0.5) // out of the way: Bedrock could pick it up too
	walkToItem(t, srv, j.goTo)

	require.Eventually(t, func() bool {
		n := 0
		onJava(t, srv, "JPicker", func(p *player.Player) { n = count(p, block.Dirt{}) })
		return n == 3
	}, 10*time.Second, 50*time.Millisecond, "the Java player did not pick the item up")
	require.Eventually(t, func() bool {
		for _, c := range j.entities().Collects {
			if c.Collected == id && c.Collector == 1 && c.Count == 3 {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "the Java client was not shown its pickup")
	require.Eventually(t, func() bool { _, gone := j.entities().Entities[id]; return !gone }, 5*time.Second, 50*time.Millisecond, "the item stayed on Java")
	require.Eventually(t, func() bool {
		for _, tk := range b.Taken() {
			if tk.Taker == javaID {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Bedrock did not see the Java player take it")
	inv := j.inventory()
	require.Equal(t, 3, totalOf(inv, "dirt"), "the Java client's inventory does not show it")
}

// The reverse: a Java player drops an item with Q, and a Bedrock player picks
// it up; Java sees the Bedrock player take it.
func TestJavaDropsBedrockPicksUp(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BPicker")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JDropper")
	waitOnGround(t, srv, "JDropper")
	b.GoTo(-6.5, 0.5)
	require.Eventually(t, func() bool { p, ok := srv.PlayerPosition("BPicker"); return ok && p.X() < -6 }, 10*time.Second, 50*time.Millisecond)
	var bedrockID int32 = -1
	require.Eventually(t, func() bool { bedrockID = j.entities().idOf("BPicker"); return bedrockID >= 0 }, 10*time.Second, 50*time.Millisecond)

	holdJava(t, srv, "JDropper", item.NewStack(item.Stick{}, 5))
	j.look(-90, 0)
	time.Sleep(150 * time.Millisecond)
	j.action(4) // Q: one
	id := javaItemEntity(t, j, "stick", 1)
	j.goTo(8.5, 0.5) // out of the way
	require.Eventually(t, func() bool { return len(b.Items()) == 1 }, 5*time.Second, 50*time.Millisecond, "Bedrock was not shown the item")
	walkToItem(t, srv, func(x, z float64) { b.GoTo(float32(x), float32(z)) })

	require.Eventually(t, func() bool {
		n := 0
		onBedrock(t, srv, "BPicker", func(p *player.Player) { n = count(p, item.Stick{}) })
		return n == 1
	}, 10*time.Second, 50*time.Millisecond, "the Bedrock player did not pick the item up")
	require.Eventually(t, func() bool {
		for _, c := range j.entities().Collects {
			if c.Collected == id && c.Collector == bedrockID {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Java was not shown the Bedrock player's pickup")
	require.Eventually(t, func() bool {
		for _, tk := range b.Taken() {
			if tk.Taker == b.RuntimeID() {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "the Bedrock client was not shown its pickup")
}

// A block broken in survival drops its item, which both editions see and the
// player picks up.
func TestSurvivalBreakDropsItem(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BSees")
	require.NoError(t, err)
	defer b.Close()
	b.GoTo(-6.5, 0.5)
	j := startBot(t, srv, "JMiner")
	waitOnGround(t, srv, "JMiner")
	target := cube.Pos{1, -60, 1}
	oak := block.Planks{Wood: block.OakWood()}
	setBlock(t, srv, target, oak)
	require.Eventually(t, func() bool { return j.block(target).Name == "oak_planks" }, 5*time.Second, 50*time.Millisecond)

	j.dig(0, target, 1, 20)
	j.dig(2, target, 1, 21)

	javaItemEntity(t, j, "oak_planks", 1)
	require.Eventually(t, func() bool {
		for _, it := range b.Items() {
			if it.Name == "minecraft:oak_planks" {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Bedrock was not shown the drop")
	// Dragonfly throws a drop a little way at random, so the player goes to it.
	walkToItem(t, srv, j.goTo)
	require.Eventually(t, func() bool {
		n := 0
		onJava(t, srv, "JMiner", func(p *player.Player) { n = count(p, oak) })
		return n == 1
	}, 10*time.Second, 50*time.Millisecond, "the drop was not picked up")
}

// A player with room for only part of a stack takes that part, and Java is
// shown the count taken, not the stack's; the rest stays as an item.
func TestPartialPickupShowsTheCountTaken(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JFull")
	waitOnGround(t, srv, "JFull")
	onJava(t, srv, "JFull", func(p *player.Player) {
		for i := 1; i < 36; i++ {
			_ = p.Inventory().SetItem(i, item.NewStack(block.Stone{}, 64))
		}
		_ = p.Inventory().SetItem(0, item.NewStack(block.Dirt{}, 62))
	})
	pos, _, ok := srv.JavaPosition("JFull")
	require.True(t, ok)
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos.Add(mgl64.Vec3{1.5, 0.5, 0})}, item.NewStack(block.Dirt{}, 5)))
		return struct{}{}, nil
	})
	require.NoError(t, err)
	id := javaItemEntity(t, j, "dirt", 5)
	walkToItem(t, srv, j.goTo)
	require.Eventually(t, func() bool {
		n := 0
		onJava(t, srv, "JFull", func(p *player.Player) { n = count(p, block.Dirt{}) })
		return n == 64
	}, 10*time.Second, 50*time.Millisecond, "the Java player did not take the two it had room for")
	require.Eventually(t, func() bool {
		for _, c := range j.entities().Collects {
			if c.Collected == id && c.Collector == 1 {
				return c.Count == 2
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Java was not shown a pickup of 2: %+v", j.entities().Collects)
	javaItemEntity(t, j, "dirt", 3) // the rest
}

// totalOf is how many of an item the bot's window shows.
func totalOf(inv botInventory, name string) int {
	n := 0
	for _, s := range inv.Slots {
		if s != nil && s.Name == name {
			n += s.Count
		}
	}
	return n
}
