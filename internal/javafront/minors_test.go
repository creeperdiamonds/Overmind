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
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// acked waits until the bot has seen the acknowledgement of seq.
func acked(t *testing.T, j *javaBot, seq int) {
	t.Helper()
	require.Eventually(t, func() bool {
		for _, a := range j.block(cube.Pos{}).Acks {
			if a == seq {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "use %d was not acknowledged", seq)
}

// resent waits until the client's window has moved on from state before and
// shows n of want in slot.
func resent(t *testing.T, j *javaBot, before, slot int, want string, n int) {
	t.Helper()
	require.Eventually(t, func() bool {
		inv := j.inventory()
		s := inv.Slots[slot]
		return inv.State != before && s != nil && s.Name == want && s.Count == n
	}, 5*time.Second, 50*time.Millisecond, "slot %d was not sent again", slot)
}

// Every refused use sends the hand's slot again, since the client took one
// for its guess: a block in a chunk it was never sent, a click point outside
// its block, and the off hand on what the main hand already opened.
func TestRefusedUsesResendTheSlot(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JRefused")
	waitOnGround(t, srv, "JRefused")
	require.Eventually(t, func() bool { return j.column(2, 0, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	oak := block.Planks{Wood: block.OakWood()}
	onJava(t, srv, "JRefused", func(p *player.Player) {
		_ = p.SetHeldSlot(0)
		p.SetHeldItems(item.NewStack(oak, 7), item.NewStack(item.Firework{}, 3))
	})
	resent(t, j, -1, wHotbar, "oak_planks", 7)

	seq := 100
	try := func(t *testing.T, pos cube.Pos, cy float64, hand, slot int, want string, n int) {
		t.Helper()
		time.Sleep(200 * time.Millisecond)
		before := j.inventory().State
		seq++
		j.placeHand(pos, cube.FaceUp, 0.5, cy, 0.5, hand, seq)
		acked(t, j, seq)
		resent(t, j, before, slot, want, n)
	}
	t.Run("a chunk the client was never sent", func(t *testing.T) {
		try(t, cube.Pos{4000, -61, 4000}, 1, 0, wHotbar, "oak_planks", 7)
	})
	t.Run("a click point outside the block", func(t *testing.T) {
		try(t, cube.Pos{2, -61, 0}, 7, 0, wHotbar, "oak_planks", 7)
	})
	t.Run("the off hand on what the main hand opened", func(t *testing.T) {
		setBlock(t, srv, cube.Pos{2, -60, 0}, block.NewChest())
		try(t, cube.Pos{2, -60, 0}, 1, 1, wOffHand, "firework_rocket", 3)
		n := 0
		onJava(t, srv, "JRefused", func(p *player.Player) { _, off := p.HeldItems(); n = off.Count() })
		require.Equal(t, 3, n, "the off hand was used")
	})
	t.Run("an empty off hand", func(t *testing.T) {
		onJava(t, srv, "JRefused", func(p *player.Player) { p.SetHeldItems(item.NewStack(oak, 7), item.Stack{}) })
		time.Sleep(200 * time.Millisecond)
		before := j.inventory().State
		seq++
		j.placeHand(cube.Pos{1, -61, 1}, cube.FaceUp, 0.5, 1, 0.5, 1, seq)
		acked(t, j, seq)
		require.Eventually(t, func() bool { inv := j.inventory(); return inv.State != before && inv.Slots[wOffHand] == nil }, 5*time.Second, 50*time.Millisecond)
		require.IsType(t, block.Air{}, blockAt(t, srv, cube.Pos{1, -60, 1}), "the main hand's planks were placed for the off hand")
	})
}

// fireworks counts the firework rockets in the world.
func fireworks(t *testing.T, srv *testserver.Server) int {
	t.Helper()
	n, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (int, error) {
		n := 0
		for e := range tx.Entities() {
			if e.H().Type() == entity.FireworkType {
				n++
			}
		}
		return n, nil
	})
	require.NoError(t, err)
	return n
}

// The off hand works from Java: a firework in it launches from a block and in
// the air, and the main hand's item is left as it was.
func TestJavaOffHandLaunchesFireworks(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "BOffSeer")
	require.NoError(t, err)
	defer watcher.Close()
	j := startBot(t, srv, "JOffHand")
	waitOnGround(t, srv, "JOffHand")
	require.Eventually(t, func() bool { return j.column(2, 0, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
	onJava(t, srv, "JOffHand", func(p *player.Player) {
		_ = p.SetHeldSlot(0)
		p.SetHeldItems(item.NewStack(item.Stick{}, 4), item.NewStack(item.Firework{}, 3))
	})
	require.Eventually(t, func() bool { s := j.inventory().Slots[wOffHand]; return s != nil && s.Count == 3 }, 5*time.Second, 50*time.Millisecond)

	j.placeHand(cube.Pos{2, -61, 0}, cube.FaceUp, 0.5, 1, 0.5, 1, 7)
	acked(t, j, 7)
	require.Eventually(t, func() bool { return fireworks(t, srv) >= 1 }, 5*time.Second, 50*time.Millisecond, "no firework was launched")
	var main, off item.Stack
	onJava(t, srv, "JOffHand", func(p *player.Player) { main, off = p.HeldItems() })
	require.Equal(t, 4, main.Count(), "the main hand changed")
	require.IsType(t, item.Stick{}, main.Item())
	require.Equal(t, 2, off.Count(), "the off hand did not give one")
	require.Eventually(t, func() bool {
		inv := j.inventory()
		return inv.Slots[wOffHand] != nil && inv.Slots[wOffHand].Count == 2 && inv.Slots[wHotbar] != nil && inv.Slots[wHotbar].Name == "stick"
	}, 5*time.Second, 50*time.Millisecond, "the client's hands are wrong")

	// In the air a rocket only boosts a glide, on both editions: standing, the
	// off hand's use_item leaves it, and the client is told.
	before := j.inventory().State
	j.useItemHand(1, 8)
	acked(t, j, 8)
	onJava(t, srv, "JOffHand", func(p *player.Player) { main, off = p.HeldItems() })
	require.Equal(t, 2, off.Count(), "a firework was used standing in the air")
	require.Equal(t, 4, main.Count())
	resent(t, j, before, wOffHand, "firework_rocket", 2)
}

// use_item's look is kept as the server keeps one: yaw wrapped, pitch held to
// 90, whatever the client sends.
func TestUseItemLookIsClamped(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JLooks")
	waitOnGround(t, srv, "JLooks")
	holdJava(t, srv, "JLooks", item.NewStack(item.Stick{}, 1))
	j.look(750, 400) // 750 is 30
	j.useItemHand(0, 3)
	acked(t, j, 3)
	var yaw, pitch float64
	onJava(t, srv, "JLooks", func(p *player.Player) { yaw, pitch = p.Rotation().Elem() })
	require.InDelta(t, 30, yaw, 1e-3)
	require.InDelta(t, 90, pitch, 1e-3)
}

// A handler set after the player joined (by a plugin, say) is wrapped again,
// so the client still gets its death screen, and the new handler still runs.
func TestReplacedHandlerStillShowsDeath(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JHandled")
	waitOnGround(t, srv, "JHandled")
	died := make(chan struct{}, 1)
	onJava(t, srv, "JHandled", func(p *player.Player) { p.Handle(deathNoter{died: died}) })
	time.Sleep(200 * time.Millisecond) // a few ticks
	onJava(t, srv, "JHandled", func(p *player.Player) { p.Hurt(100, entity.VoidDamageSource{}) })
	select {
	case <-died:
	case <-time.After(5 * time.Second):
		t.Fatal("the handler set later did not run")
	}
	require.Eventually(t, func() bool { return len(j.entities().Events.Deaths) > 0 }, 5*time.Second, 50*time.Millisecond, "no death screen")
	require.Equal(t, "death.attack.outOfWorld", plain(j.entities().Events.Deaths[0].Message).Translate)
}

type deathNoter struct {
	player.NopHandler
	died chan struct{}
}

func (d deathNoter) HandleDeath(*player.Player, world.DamageSource, *bool) {
	select {
	case d.died <- struct{}{}:
	default:
	}
}
