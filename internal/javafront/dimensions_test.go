package javafront_test

import (
	"context"
	"fmt"
	"slices"
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

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/config"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// moveTo takes a Java player to another world through Dragonfly's own API, as
// a portal or a plugin would: out of its world's transaction and into the
// other's, at pos.
func moveTo(t *testing.T, srv *testserver.Server, name string, w *world.World, pos mgl64.Vec3) {
	t.Helper()
	onJava(t, srv, name, func(p *player.Player) {
		h := p.Tx().RemoveEntity(p)
		w.Do(func(tx *world.Tx) {
			np := tx.AddEntity(h).(*player.Player)
			np.Teleport(pos)
		})
	})
}

func dimensionOfBot(j *javaBot) string {
	var r struct {
		Seen struct {
			Dimension string `json:"dimension"`
		} `json:"seen"`
	}
	j.call("state", nil, &r)
	return r.Seen.Dimension
}

// A Java player taken to the Nether and back gets a new level each time, with
// the dimension's type from the registries it was sent, its chunks (as tall as
// the dimension) and its blocks; a Bedrock player there sees it, and its
// changes reach it.
func TestJavaChangesDimension(t *testing.T) {
	srv := testserver.Start(t)
	nether := srv.Nether()
	floor := &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}
	_, err := world.Call(context.Background(), nether, func(tx *world.Tx) (struct{}, error) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Netherrack{}, floor)
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)

	j := startBot(t, srv, "JTraveler")
	waitOnGround(t, srv, "JTraveler")
	require.Equal(t, "minecraft:overworld", dimensionOfBot(j))
	before := j.state().Seen.Respawns

	moveTo(t, srv, "JTraveler", nether, mgl64.Vec3{0.5, 65, 0.5})
	require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:the_nether" }, 10*time.Second, 50*time.Millisecond, "no respawn into the Nether")
	require.Equal(t, before+1, j.state().Seen.Respawns)
	got, want := dimensionTypeOfBot(t, j, "minecraft:the_nether")
	require.Equal(t, want, got, "the Nether's dimension type")
	require.Eventually(t, func() bool {
		c := j.column(0, 0, 63, 65)
		return c.Loaded && c.Blocks[64].Block == "netherrack" && c.Blocks[65].Block == "air"
	}, 20*time.Second, 50*time.Millisecond, "the Nether's chunk did not arrive right: %+v %+v", j.column(0, 0, 63, 65), j.state())
	require.Eventually(t, func() bool {
		pos, onGround, ok := srv.JavaPosition("JTraveler")
		return ok && onGround && pos.Y() > 64.9 && pos.Y() < 65.1
	}, 10*time.Second, 50*time.Millisecond, "the player did not stand on the Nether floor: %v", fmt.Sprint(srv.JavaPosition("JTraveler")))

	// A change in the Nether reaches it.
	_, err = world.Call(context.Background(), nether, func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(cube.Pos{2, 65, 2}, block.Glowstone{}, nil)
		return struct{}{}, nil
	})
	require.NoError(t, err)
	require.Eventually(t, func() bool { return j.block(cube.Pos{2, 65, 2}).Name == "glowstone" }, 5*time.Second, 50*time.Millisecond)

	// A Bedrock player in the Nether sees the Java one there.
	b, err := bedrock.Join(srv.BedrockAddr, "BNetherSeer")
	require.NoError(t, err)
	defer b.Close()
	onBedrock(t, srv, "BNetherSeer", func(p *player.Player) {
		h := p.Tx().RemoveEntity(p)
		nether.Do(func(tx *world.Tx) { tx.AddEntity(h).(*player.Player).Teleport(mgl64.Vec3{-1.5, 65, -1.5}) })
	})
	require.Eventually(t, func() bool { _, ok := b.Player("JTraveler"); return ok }, 15*time.Second, 50*time.Millisecond, "Bedrock did not see the Java player in the Nether")
	require.Eventually(t, func() bool { _, ok := j.players()["BNetherSeer"]; return ok }, 15*time.Second, 50*time.Millisecond, "the Java player did not see the Bedrock one in the Nether")

	// And back.
	moveTo(t, srv, "JTraveler", srv.World(), mgl64.Vec3{0.5, -60, 0.5})
	require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:overworld" }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { c := j.column(0, 0, -61, -61); return c.Loaded && c.Blocks[-61].Block == "grass_block" }, 20*time.Second, 50*time.Millisecond)
	waitOnGround(t, srv, "JTraveler")
	require.Empty(t, j.players(), "players of the Nether still shown")
}

// The world a Java player leaves keeps calling its conn until that world's
// goroutine has run the loader's removal; none of those calls may touch the
// conn while the new world uses it (run with -race). Items fall in the
// Overworld, moving every tick, while the player goes to the Nether and back,
// and when it is back it sees the Overworld's items and nothing of the Nether.
func TestDimensionChangeWithBusyOldWorld(t *testing.T) {
	srv := testserver.Start(t)
	nether := srv.Nether()
	floor := &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}
	_, err := world.Call(context.Background(), nether, func(tx *world.Tx) (struct{}, error) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Netherrack{}, floor)
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)

	j := startBot(t, srv, "JBusy")
	waitOnGround(t, srv, "JBusy")
	home := mgl64.Vec3{0.5, -60, 0.5}
	drop := func(n int) {
		_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
			for k := range n {
				pos := mgl64.Vec3{float64(k%4) + 3.5, -50, float64(k/4) + 3.5}
				tx.AddEntity(entity.NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(block.Dirt{}, 1)))
			}
			return struct{}{}, nil
		})
		require.NoError(t, err)
	}
	for range 3 {
		drop(8)
		moveTo(t, srv, "JBusy", nether, mgl64.Vec3{0.5, 65, 0.5})
		require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:the_nether" }, 10*time.Second, 50*time.Millisecond)
		drop(8)
		moveTo(t, srv, "JBusy", srv.World(), home)
		require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:overworld" }, 10*time.Second, 50*time.Millisecond)
	}
	javaItemEntity(t, j, "dirt", 1)
	require.Empty(t, j.players(), "players of the Nether still shown")
}

// dimensionTypeOfBot is the dimension type the bot was last told, by its place
// in the registry, and that of name in the registry sent.
func dimensionTypeOfBot(t *testing.T, j *javaBot, name string) (got, want int) {
	t.Helper()
	var r struct {
		Seen struct {
			Type int `json:"dimensionType"`
		} `json:"seen"`
	}
	j.call("state", nil, &r)
	for _, reg := range java261.Registries() {
		if reg.ID == "minecraft:dimension_type" {
			want = slices.Index(reg.Entries, name)
		}
	}
	require.GreaterOrEqual(t, want, 0)
	return r.Seen.Type, want
}

// A Java player who leaves in the Nether comes back there, where it left.
func TestJavaRejoinsInTheNether(t *testing.T) {
	srv := testserver.Start(t)
	nether := srv.Nether()
	_, err := world.Call(context.Background(), nether, func(tx *world.Tx) (struct{}, error) {
		for x := -3; x <= 3; x++ {
			for z := -3; z <= 3; z++ {
				tx.SetBlock(cube.Pos{x, 64, z}, block.Netherrack{}, &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true})
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)
	j := startBot(t, srv, "JNetherSaver")
	waitOnGround(t, srv, "JNetherSaver")
	moveTo(t, srv, "JNetherSaver", nether, mgl64.Vec3{1.5, 65, 1.5})
	require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:the_nether" }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { _, on, ok := srv.JavaPosition("JNetherSaver"); return ok && on }, 10*time.Second, 50*time.Millisecond)
	j.quit()
	require.Eventually(t, func() bool { return !srv.JavaOnline("JNetherSaver") }, 10*time.Second, 50*time.Millisecond)

	again := startBot(t, srv, "JNetherSaver")
	require.Equal(t, "minecraft:the_nether", dimensionOfBot(again), "the player came back in another dimension")
	got, want := dimensionTypeOfBot(t, again, "minecraft:the_nether")
	require.Equal(t, want, got)
	require.Eventually(t, func() bool {
		pos, _, ok := srv.JavaPosition("JNetherSaver")
		return ok && pos.Sub(mgl64.Vec3{1.5, 65, 1.5}).Len() < 1
	}, 10*time.Second, 50*time.Millisecond, "not where it left")
	var in world.Dimension
	onJava(t, srv, "JNetherSaver", func(p *player.Player) { in = p.Tx().World().Dimension() })
	require.Equal(t, world.Nether, in)
	require.Eventually(t, func() bool { c := again.column(1, 1, 64, 64); return c.Loaded && c.Blocks[64].Block == "netherrack" }, 20*time.Second, 50*time.Millisecond)
}

// A Java player taken into the generated Nether (Bedrock's 128 rows, sent as Java's 256) and the
// generated End, and back, stays connected and gets their chunks: the Nether's bedrock floor and the
// End's arrival platform under its spawn.
func TestJavaEntersGeneratedNetherAndEnd(t *testing.T) {
	srv := testserver.StartWith(t, func(c *config.Config) { c.FlatWorld = false })
	j := startBot(t, srv, "JWanderer")
	kicked := func() bool {
		select {
		case r := <-j.kicked:
			t.Fatalf("the Java player was disconnected: %s", r)
		default:
		}
		return false
	}

	moveTo(t, srv, "JWanderer", srv.Nether(), mgl64.Vec3{0.5, 140, 0.5})
	require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:the_nether" }, 20*time.Second, 50*time.Millisecond, "no respawn into the Nether")
	require.Eventually(t, func() bool {
		c := j.column(0, 0, 0, 127)
		return !kicked() && c.Loaded && c.Blocks[0].Block == "bedrock"
	}, 30*time.Second, 100*time.Millisecond, "the generated Nether's chunk did not arrive: %+v", j.column(0, 0, 0, 3))

	moveTo(t, srv, "JWanderer", srv.End(), mgl64.Vec3{100.5, 49, 0.5})
	require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:the_end" }, 20*time.Second, 50*time.Millisecond, "no respawn into the End")
	require.Eventually(t, func() bool {
		c := j.column(100, 0, 48, 48)
		return !kicked() && c.Loaded && c.Blocks[48].Block == "obsidian"
	}, 30*time.Second, 100*time.Millisecond, "the End's platform did not arrive: %+v", j.column(100, 0, 48, 48))
	require.Eventually(t, func() bool {
		pos, onGround, ok := srv.JavaPosition("JWanderer")
		return ok && onGround && pos.Y() > 48.9 && pos.Y() < 49.1
	}, 15*time.Second, 50*time.Millisecond, "the player did not stand on the End's platform: %v", fmt.Sprint(srv.JavaPosition("JWanderer")))

	moveTo(t, srv, "JWanderer", srv.World(), srv.World().Spawn().Vec3Centre())
	require.Eventually(t, func() bool { return dimensionOfBot(j) == "minecraft:overworld" }, 20*time.Second, 50*time.Millisecond)
	kicked()
}
