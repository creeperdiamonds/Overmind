package javafront_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// itemCounts is the stack sizes of the item entities in the world, sorted.
func itemCounts(t *testing.T, srv *testserver.Server) []int {
	t.Helper()
	var out []int
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.ItemType {
				out = append(out, e.(*entity.Ent).Behaviour().(*entity.ItemBehaviour).Item().Count())
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)
	slices.Sort(out)
	return out
}

// javaItemCounts is the stack sizes of the item entities a Java client shows.
func javaItemCounts(t *testing.T, j *javaBot) []int {
	var out []int
	for _, e := range j.entities().Entities {
		if e.Type != "item" {
			continue
		}
		n := -1
		if it := e.item(t); it != nil {
			n = it.Count
		}
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

func bedrockItemCounts(b *bedrock.Bot) []int {
	var out []int
	for _, it := range b.Items() {
		out = append(out, it.Count)
	}
	slices.Sort(out)
	return out
}

// A Java player dies with a full inventory next to a Java and a Bedrock
// watcher and respawns where it sees its drops. Some of the drops merge, a
// Java player picks some up. Every client then shows the item entities the
// server has, no more: the owner's playtest left the dead Java player's
// screen covered in items that could not be picked up and never went.
func TestDeathDropsLeaveNoGhostItems(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BGhostView")
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	view := startBot(t, srv, "JGhostView")
	die := startBot(t, srv, "JGhostDie")
	waitOnGround(t, srv, "JGhostView")
	waitOnGround(t, srv, "JGhostDie")
	b.GoTo(-5, 0.5)
	require.Eventually(t, func() bool { p, ok := srv.PlayerPosition("BGhostView"); return ok && p.X() < -4.5 }, 10*time.Second, 50*time.Millisecond)
	var deathAt mgl64.Vec3
	onJava(t, srv, "JGhostDie", func(p *player.Player) {
		deathAt = p.Position().Add(mgl64.Vec3{8, 0, 0})
		p.Teleport(deathAt)
	})
	onJava(t, srv, "JGhostView", func(p *player.Player) { p.Teleport(p.Position().Add(mgl64.Vec3{-3, 0, 0})) })
	time.Sleep(time.Second)

	// Dirt in ten stacks of five, which merge, and a stack of each colour of
	// wool, which do not.
	onJava(t, srv, "JGhostDie", func(p *player.Player) {
		p.SetGameMode(world.GameModeSurvival)
		inv := p.Inventory()
		for i := range 10 {
			_ = inv.SetItem(i, item.NewStack(block.Dirt{}, 5))
		}
		for i, c := range item.Colours() {
			_ = inv.SetItem(10+i, item.NewStack(block.Wool{Colour: c}, 1+i))
		}
	})
	// The watcher's inventory is full but for two slots, so it takes some.
	onJava(t, srv, "JGhostView", func(p *player.Player) {
		p.SetGameMode(world.GameModeSurvival)
		for i := range 34 {
			_ = p.Inventory().SetItem(i, item.NewStack(block.Stone{}, 64))
		}
	})
	onJava(t, srv, "JGhostDie", func(p *player.Player) { p.Hurt(1000, entity.VoidDamageSource{}) })
	require.Eventually(t, func() bool { h := die.state().Seen.Health; return h != nil && *h <= 0 }, 5*time.Second, 50*time.Millisecond, "the Java client did not die")
	require.Eventually(t, func() bool { return len(itemCounts(t, srv)) > 0 }, 5*time.Second, 50*time.Millisecond, "no drops")
	time.Sleep(1500 * time.Millisecond) // past finishDying
	// While the player is between its removal and its return (Dragonfly's
	// respawn queues its return on the world), the world takes the wool stack
	// of 16 away and drops a stick: the conn drops those callbacks.
	onJava(t, srv, "JGhostDie", func(p *player.Player) {
		p.Handle(gapHandler{Handler: p.Handler(), gap: func(tx *world.Tx) {
			for e := range tx.Entities() {
				if it, ok := e.(*entity.Ent); ok && e.H().Type() == entity.ItemType && it.Behaviour().(*entity.ItemBehaviour).Item().Count() == 16 {
					_ = it.Close()
				}
			}
			tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: deathAt}, item.NewStack(item.Stick{}, 40)))
		}})
	})
	die.respawn()
	require.Eventually(t, func() bool { h := die.state().Seen.Health; return h != nil && *h == 20 }, 10*time.Second, 50*time.Millisecond, "the Java player did not come back")

	same := func(when string) {
		t.Helper()
		var server, java, watcher, bed []int
		ok := func() bool {
			server = itemCounts(t, srv)
			java, watcher, bed = javaItemCounts(t, die), javaItemCounts(t, view), bedrockItemCounts(b)
			return slices.Equal(server, java) && slices.Equal(server, watcher) && slices.Equal(server, bed)
		}
		deadline := time.Now().Add(10 * time.Second)
		for !ok() && time.Now().Before(deadline) {
			time.Sleep(100 * time.Millisecond)
		}
		require.True(t, ok(), fmt.Sprintf("%s: server %v\nrespawned Java %v\nJava watcher %v\nBedrock %v", when, server, java, watcher, bed))
	}
	same("after the respawn")
	time.Sleep(3 * time.Second) // the dirt merges
	same("after merges")

	onJava(t, srv, "JGhostView", func(p *player.Player) { p.Teleport(deathAt) })
	require.Eventually(t, func() bool {
		n := 0
		onJava(t, srv, "JGhostView", func(p *player.Player) { n = len(p.Inventory().Items()) })
		return n == 36
	}, 15*time.Second, 50*time.Millisecond, "the watcher picked nothing up")
	time.Sleep(2 * time.Second)
	same("after the pickup")
}

// gapHandler runs gap on the world in the moment a respawning player is in no
// world: queued before Dragonfly queues the player's return.
type gapHandler struct {
	player.Handler
	gap func(tx *world.Tx)
}

func (h gapHandler) HandleRespawn(p *player.Player, pos *mgl64.Vec3, w **world.World) {
	h.Handler.HandleRespawn(p, pos, w)
	p.Tx().World().Do(h.gap)
}

// A Java player taken to another world of the same dimension (a plugin's
// second Overworld) gets a Respawn into the dimension it is in, which keeps
// the client's level: what it was shown of the world it left is taken off,
// not left there as ghosts.
func TestSameDimensionWorldChangeLeavesNoGhosts(t *testing.T) {
	srv := testserver.Start(t)
	other := world.Config{Entities: entity.DefaultRegistry, Generator: generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Bedrock{}}), Dim: world.Overworld}.New()
	t.Cleanup(func() { _ = other.Close() })
	j := startBot(t, srv, "JWorldHop")
	waitOnGround(t, srv, "JWorldHop")
	var at mgl64.Vec3
	onJava(t, srv, "JWorldHop", func(p *player.Player) {
		at = p.Position()
		p.Tx().AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: at.Add(mgl64.Vec3{4, 0, 0})}, item.NewStack(block.Dirt{}, 7)))
	})
	javaItemEntity(t, j, "dirt", 7)
	before := j.state().Seen.Respawns

	moveTo(t, srv, "JWorldHop", other, mgl64.Vec3{0.5, 10, 0.5})
	require.Eventually(t, func() bool { return j.state().Seen.Respawns > before }, 10*time.Second, 50*time.Millisecond, "no respawn into the other world")
	require.Eventually(t, func() bool { return len(javaItemCounts(t, j)) == 0 }, 5*time.Second, 50*time.Millisecond, "items of the world left still shown: %v", javaItemCounts(t, j))
	require.Eventually(t, func() bool { return j.column(0, 0, -64, -64).Loaded }, 20*time.Second, 50*time.Millisecond, "the other world's chunk did not come")
}
