package javafront_test

import (
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/testserver"
)

// sticksIn counts the sticks in the chests at the positions as the server has
// them, each inventory once (a double chest's halves share one).
func sticksIn(tx *world.Tx, pos ...cube.Pos) int {
	seen := map[*inventory.Inventory]bool{}
	n := 0
	for _, at := range pos {
		ch, ok := tx.Block(at).(block.Chest)
		if !ok {
			continue
		}
		inv := ch.Inventory(tx, at)
		if seen[inv] {
			continue
		}
		seen[inv] = true
		n += sticks(inv)
	}
	return n
}

func sticks(inv *inventory.Inventory) int {
	n := 0
	for _, s := range inv.Slots() {
		if _, ok := s.Item().(item.Stick); ok {
			n += s.Count()
		}
	}
	return n
}

// A chest window stays true to the chest: when the chest pairs or a half of a
// double chest goes while a Java player has it open, Dragonfly gives the chest
// a new inventory, and the old window closes rather than act on a copy (which
// duplicated items). Found by the batch C review.
func TestChestWindowFollowsPairing(t *testing.T) {
	for _, c := range []struct {
		name string
		// change, with the opened chest at a and its neighbour's place at b.
		start  func(tx *world.Tx, p *player.Player, a, b cube.Pos)
		change func(tx *world.Tx, p *player.Player, a, b cube.Pos)
	}{
		{"pairing while open", nil, func(tx *world.Tx, p *player.Player, a, b cube.Pos) {
			block.NewChest().UseOnBlock(b.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, p, &item.UseContext{})
		}},
		{"the other half broken while open", func(tx *world.Tx, p *player.Player, a, b cube.Pos) {
			block.NewChest().UseOnBlock(b.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, p, &item.UseContext{})
		}, func(tx *world.Tx, p *player.Player, a, b cube.Pos) {
			tx.Block(b).(block.Chest).BreakInfo().BreakHandler(b, tx, p) // unpairs, as a break does
			tx.SetBlock(b, nil, nil)
		}},
		{"the opened half broken while open", func(tx *world.Tx, p *player.Player, a, b cube.Pos) {
			block.NewChest().UseOnBlock(b.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, p, &item.UseContext{})
		}, func(tx *world.Tx, p *player.Player, a, b cube.Pos) {
			tx.SetBlock(a, nil, nil)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			srv := testserver.Start(t)
			j := startBot(t, srv, "JChests")
			waitOnGround(t, srv, "JChests")
			require.Eventually(t, func() bool { return j.column(1, -2, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)
			a := cube.Pos{1, -60, -2}
			var b cube.Pos
			onJava(t, srv, "JChests", func(p *player.Player) {
				tx := p.Tx()
				f := p.Rotation().Direction().Opposite()
				ch := block.NewChest()
				ch.Facing = f
				tx.SetBlock(a, ch, nil)
				_ = tx.Block(a).(block.Chest).Inventory(tx, a).SetItem(0, item.NewStack(item.Stick{}, 10))
				b = a.Side(f.RotateRight().Face())
				if c.start != nil {
					c.start(tx, p, a, b)
				}
			})
			require.Eventually(t, func() bool { return j.block(a).Name == "chest" }, 5*time.Second, 50*time.Millisecond)
			menu := "generic_9x3"
			if c.start != nil {
				menu = "generic_9x6"
			}
			id := openBlock(t, srv, j, "JChests", a, menu, 40)
			onJava(t, srv, "JChests", func(p *player.Player) { c.change(p.Tx(), p, a, b) })
			time.Sleep(200 * time.Millisecond)
			for s := 0; s < 54; s++ {
				j.clickIn(id, s, 0, 1) // shift-click everything out
			}
			time.Sleep(500 * time.Millisecond)
			var mine, inChests, dropped int
			onJava(t, srv, "JChests", func(p *player.Player) {
				mine = count(p, item.Stick{})
				inChests = sticksIn(p.Tx(), a, b)
			})
			if _, s, ok := itemOnServer(t, srv); ok {
				if _, stick := s.Item().(item.Stick); stick {
					dropped = s.Count()
				}
			}
			t.Logf("player %d, chests %d, dropped %d", mine, inChests, dropped)
			if c.name == "the opened half broken while open" {
				// The broken half's 10 sticks drop; none may also reach the player.
				require.Equal(t, 0, mine, "DUPE: sticks taken from a chest that is gone")
				return
			}
			require.Equal(t, 10, mine+inChests+dropped, "sticks multiplied or lost")
			require.Nil(t, j.window().Open, "the stale window stayed open")
		})
	}
}

// The same for Bedrock: Dragonfly's session kept the chest's old inventory as
// the open window, so a chest that paired while open let a Bedrock player take
// its items out of a copy. The fork re-reads the chest (session/player.go).
func TestBedrockChestWindowFollowsPairing(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BChests")
	require.NoError(t, err)
	defer b.Close()
	a := cube.Pos{1, -60, -2}
	var next cube.Pos
	onBedrock(t, srv, "BChests", func(p *player.Player) {
		tx := p.Tx()
		ch := block.NewChest()
		ch.Facing = cube.North
		tx.SetBlock(a, ch, nil)
		_ = tx.Block(a).(block.Chest).Inventory(tx, a).SetItem(0, item.NewStack(item.Stick{}, 10))
		next = a.Side(cube.North.RotateRight().Face())
		p.OpenBlockContainer(a, tx)
	})
	require.Eventually(t, func() bool { id, _, slots := b.Container(); return id != 0 && len(slots) > 0 && slots[0].Count == 10 }, 10*time.Second, 50*time.Millisecond, "the chest did not open on Bedrock")
	onBedrock(t, srv, "BChests", func(p *player.Player) {
		c := block.NewChest()
		c.Facing = cube.North
		c.UseOnBlock(next.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, p.Tx(), p, &item.UseContext{})
		ch, ok := p.Tx().Block(a).(block.Chest)
		require.True(t, ok && ch.Paired(), "setup: chests did not pair")
	})
	time.Sleep(200 * time.Millisecond)
	require.NoError(t, b.TakeFromContainer(0, 10, 9))
	time.Sleep(700 * time.Millisecond)
	var mine, inChests int
	onBedrock(t, srv, "BChests", func(p *player.Player) {
		mine = count(p, item.Stick{})
		inChests = sticksIn(p.Tx(), a, next)
	})
	t.Logf("player %d, chests %d", mine, inChests)
	require.Equal(t, 10, mine+inChests, "DUPE: sticks multiplied")
}

// The opened half of a double chest broken while a Bedrock player has it open:
// the session's window is the old merged inventory, whose slot 0 is the other
// half's slot 0, which the unpaired half now keeps in a copy of its own.
// Taking from the old window gave 20 sticks from 10. A player's break runs the
// break handler and then clears the block; an explosion (block/explosion.go)
// clears it first. Both are tested.
func TestBedrockChestWindowOpenedHalfBroken(t *testing.T) {
	for _, tc := range []struct {
		name   string
		remove func(tx *world.Tx, at cube.Pos, ch block.Chest)
	}{
		{"broken", func(tx *world.Tx, at cube.Pos, ch block.Chest) {
			ch.BreakInfo().BreakHandler(at, tx, nil)
			tx.SetBlock(at, nil, nil)
		}},
		{"exploded", func(tx *world.Tx, at cube.Pos, ch block.Chest) {
			tx.SetBlock(at, nil, nil)
			ch.BreakInfo().BreakHandler(at, tx, nil)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := testserver.Start(t)
			b, err := bedrock.Join(srv.BedrockAddr, "BHalf")
			require.NoError(t, err)
			defer b.Close()
			a := cube.Pos{1, -60, -2}
			var next cube.Pos
			var pairedOK bool
			onBedrock(t, srv, "BHalf", func(p *player.Player) {
				tx := p.Tx()
				ch := block.NewChest()
				ch.Facing = cube.North
				tx.SetBlock(a, ch, nil)
				next = a.Side(cube.North.RotateRight().Face())
				c := block.NewChest()
				c.Facing = cube.North
				c.UseOnBlock(next.Side(cube.FaceDown), cube.FaceUp, mgl64.Vec3{}, tx, p, &item.UseContext{})
				paired, ok := tx.Block(a).(block.Chest)
				if pairedOK = ok && paired.Paired(); pairedOK {
					_ = paired.Inventory(tx, a).SetItem(0, item.NewStack(item.Stick{}, 10))
					p.OpenBlockContainer(a, tx)
				}
			})
			require.True(t, pairedOK, "setup: chests did not pair")
			require.Eventually(t, func() bool {
				id, _, slots := b.Container()
				return id != 0 && len(slots) == 54 && slots[0].Count == 10
			}, 10*time.Second, 50*time.Millisecond, "the double chest did not open on Bedrock")
			var left int
			onBedrock(t, srv, "BHalf", func(p *player.Player) {
				tx := p.Tx()
				tc.remove(tx, a, tx.Block(a).(block.Chest))
				left = sticksIn(tx, next)
			})
			require.Equal(t, 10, left, "setup: slot 0 was not the other half's")
			time.Sleep(200 * time.Millisecond)
			require.NoError(t, b.TakeFromContainer(0, 10, 9))
			time.Sleep(700 * time.Millisecond)
			var mine, inChests int
			onBedrock(t, srv, "BHalf", func(p *player.Player) {
				mine = count(p, item.Stick{})
				inChests = sticksIn(p.Tx(), a, next)
			})
			t.Logf("player %d, chests %d", mine, inChests)
			require.Equal(t, 10, mine+inChests, "DUPE: sticks multiplied")
		})
	}
}

// And a chest left alone still gives its items to a Bedrock player.
func TestBedrockTakesFromChest(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BTaker")
	require.NoError(t, err)
	defer b.Close()
	a := cube.Pos{1, -60, -2}
	onBedrock(t, srv, "BTaker", func(p *player.Player) {
		tx := p.Tx()
		tx.SetBlock(a, block.NewChest(), nil)
		_ = tx.Block(a).(block.Chest).Inventory(tx, a).SetItem(0, item.NewStack(item.Stick{}, 10))
		p.OpenBlockContainer(a, tx)
	})
	require.Eventually(t, func() bool { id, _, slots := b.Container(); return id != 0 && len(slots) > 0 && slots[0].Count == 10 }, 10*time.Second, 50*time.Millisecond)
	require.NoError(t, b.TakeFromContainer(0, 10, 9))
	require.Eventually(t, func() bool {
		n := 0
		onBedrock(t, srv, "BTaker", func(p *player.Player) { n = count(p, item.Stick{}) })
		return n == 10
	}, 5*time.Second, 50*time.Millisecond, "the Bedrock player could not take from the chest")
}
