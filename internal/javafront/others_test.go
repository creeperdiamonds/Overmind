package javafront_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/tables/java261"
	"github.com/overmind/overmind/internal/testserver"
)

// spawn adds an entity to the world.
func spawn(t *testing.T, srv *testserver.Server, f func(tx *world.Tx) *world.EntityHandle) {
	t.Helper()
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		tx.AddEntity(f(tx))
		return struct{}{}, nil
	})
	require.NoError(t, err)
}

// firstOf waits for the bot to be shown an entity of a Java type and returns
// its ID and what the bot knows of it.
func firstOf(t *testing.T, j *javaBot, typ string) (int32, map[string]json.RawMessage) {
	t.Helper()
	var id int32 = -1
	var raw map[string]json.RawMessage
	require.Eventually(t, func() bool {
		var r struct {
			Entities map[int32]map[string]json.RawMessage `json:"entities"`
		}
		j.call("entities", nil, &r)
		for i, e := range r.Entities {
			var name string
			_ = json.Unmarshal(e["typeName"], &name)
			if name == typ {
				id, raw = i, e
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Java was never shown a %s", typ)
	return id, raw
}

// Entities other than players and items reach Java as Java's own: a falling
// block (with its block, moving as it falls, gone when it lands), TNT (with its
// fuse), an experience orb (with its value), an arrow a Bedrock player shot
// (its owner known), a snowball. One Java has no counterpart for is not shown.
func TestJavaSeesOtherEntities(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BArcher")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JSpectator")
	waitOnGround(t, srv, "JSpectator")
	require.Eventually(t, func() bool { return j.entities().idOf("BArcher") >= 0 }, 10*time.Second, 50*time.Millisecond)

	sand := block.Sand{}
	spawn(t, srv, func(*world.Tx) *world.EntityHandle {
		return entity.NewFallingBlock(world.EntitySpawnOpts{Position: mgl64.Vec3{3.5, -52, 0.5}}, sand)
	})
	id, fb := firstOf(t, j, "falling_block")
	var data int32
	require.NoError(t, json.Unmarshal(fb["data"], &data))
	require.EqualValues(t, java261.JavaState(world.BlockRuntimeID(sand)), data, "the falling block's block")
	var y0 float64
	require.NoError(t, json.Unmarshal(fb["y"], &y0))
	require.Eventually(t, func() bool { e, ok := j.entities().Entities[id]; return !ok || e.Y < y0-1 }, 5*time.Second, 50*time.Millisecond, "it did not fall")
	require.Eventually(t, func() bool { _, ok := j.entities().Entities[id]; return !ok }, 10*time.Second, 50*time.Millisecond, "it stayed after landing")
	require.Eventually(t, func() bool { return j.block(cubePos(3, -60, 0)).Name == "sand" }, 5*time.Second, 50*time.Millisecond, "it did not land as sand")

	spawn(t, srv, func(*world.Tx) *world.EntityHandle {
		return entity.NewTNT(world.EntitySpawnOpts{Position: mgl64.Vec3{-6.5, -60, 6.5}}, 40*time.Second)
	})
	// Metadata follows the spawn: wait for it.
	metaOf := func(id int32, entity, key string) int {
		v := -1
		require.Eventually(t, func() bool {
			e, ok := j.entities().Entities[id]
			return ok && e.meta(t, int(java261.MetadataIndex(entity, key)), &v)
		}, 5*time.Second, 50*time.Millisecond, "no %s %s", entity, key)
		return v
	}
	tntID, _ := firstOf(t, j, "tnt")
	require.InDelta(t, 800, metaOf(tntID, "tnt", "fuse"), 40, "the fuse in ticks")

	spawn(t, srv, func(*world.Tx) *world.EntityHandle {
		return entity.NewExperienceOrb(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, -60, 5.5}}, 7)
	})
	orbID, _ := firstOf(t, j, "experience_orb")
	require.Equal(t, 7, metaOf(orbID, "experience_orb", "value"))

	archerID := j.entities().idOf("BArcher")
	onBedrock(t, srv, "BArcher", func(p *player.Player) {
		p.Tx().AddEntity(entity.NewArrow(world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, 1.5, 0}), Velocity: mgl64.Vec3{0, 0.5, 0}}, p))
	})
	_, arrow := firstOf(t, j, "arrow")
	require.NoError(t, json.Unmarshal(arrow["data"], &data))
	require.Equal(t, archerID, data, "the arrow's owner")

	onBedrock(t, srv, "BArcher", func(p *player.Player) {
		p.Tx().AddEntity(entity.NewSnowball(world.EntitySpawnOpts{Position: p.Position().Add(mgl64.Vec3{0, 1.5, 0}), Velocity: mgl64.Vec3{0, 0.6, 0}}, p))
	})
	firstOf(t, j, "snowball")

	// Dragonfly's floating text has no Java counterpart: it is not shown.
	before := len(j.entities().Entities)
	spawn(t, srv, func(*world.Tx) *world.EntityHandle { return entity.NewText("hello", mgl64.Vec3{0.5, -58, 2.5}) })
	time.Sleep(500 * time.Millisecond)
	for _, e := range j.entities().Entities {
		require.NotEqual(t, "unknown", e.Type)
	}
	require.LessOrEqual(t, len(j.entities().Entities), before)
}

func cubePos(x, y, z int) cube.Pos { return cube.Pos{x, y, z} }

