package app_test

import (
	"os"
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
	"github.com/creeperdiamonds/overmind/internal/app"
	"github.com/creeperdiamonds/overmind/internal/config"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// TestEndPortalLeadsToTheEnd fills a generated stronghold's portal frame and sends a player through it
// (Task 11): the eyes the frame lacks are inserted as a player's eye of ender does, the 3 x 3 portal
// appears, and a player stepping in arrives in the End on the obsidian platform at (100, 49, 0), as on
// Bedrock, with no chunk generated on a world's tick. Strongholds need the install's structure data,
// so the test needs OVERMIND_BEDROCK_DATA.
//
// The stronghold is seed 100's ring stronghold at chunk (53, 100), whose layout structures'
// TestStrongholdLayoutMatchesSpec holds piece for piece; its portal room's box is x 832 to 842, y 32 to
// 39, z 1629 to 1644 (testdata/stronghold-seed100-53-100.json). The frames are found by a scan around it.
//
// The way back is not tested: the exit portal is inactive until the dragon is killed, and there is no
// dragon fight yet (mobs stage).
func TestEndPortalLeadsToTheEnd(t *testing.T) {
	data := os.Getenv("OVERMIND_BEDROCK_DATA")
	if data == "" {
		t.Skip("OVERMIND_BEDROCK_DATA is not set: strongholds need the Bedrock install's structure data")
	}
	onTick := app.GenerationsOnTick()
	srv := testserver.StartIn(t, t.TempDir(), func(c *config.Config) {
		seed := int64(100)
		c.Seed, c.BedrockDataDir = &seed, data
	})
	w := srv.World()
	room := cube.Pos{837, 35, 1636}
	defer watch(t, w, room, 3)()

	// The generated frames: 12, each facing the portal's centre.
	var frames []cube.Pos
	eyes := 0
	run(t, w, func(tx *world.Tx) {
		for x := 826; x <= 848; x++ {
			for z := 1622; z <= 1651; z++ {
				for y := 25; y <= 45; y++ {
					if f, ok := tx.Block(cube.Pos{x, y, z}).(block.EndPortalFrame); ok {
						frames = append(frames, cube.Pos{x, y, z})
						if f.Eye {
							eyes++
						}
					}
				}
			}
		}
	})
	require.Len(t, frames, 12, "the portal room does not hold 12 frames")
	t.Logf("frames at y %d, %d with an eye as generated", frames[0][1], eyes)
	var centre cube.Pos
	for _, p := range frames {
		centre = centre.Add(p)
	}
	centre = cube.Pos{centre[0] / 12, centre[1] / 12, centre[2] / 12}

	// Insert the missing eyes as a player's eye of ender does.
	run(t, w, func(tx *world.Tx) {
		for _, p := range frames {
			if f := tx.Block(p).(block.EndPortalFrame); !f.Eye {
				ctx := &item.UseContext{}
				require.True(t, item.EnderEye{}.UseOnBlock(p, cube.FaceUp, mgl64.Vec3{}, tx, nil, ctx), "no eye went into the frame at %v", p)
			}
		}
	})
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			p := centre.Add(cube.Pos{dx, 0, dz})
			_, ok := blockAt(t, w, p).(block.EndPortal)
			require.True(t, ok, "no End portal block at %v once the frame was filled", p)
		}
	}

	// A player who steps in arrives in the End on the obsidian platform.
	bot, err := bedrock.Join(srv.BedrockAddr, "Traveller")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("Traveller") }, 15*time.Second, 100*time.Millisecond)
	h, _ := srv.Server.PlayerByName("Traveller")
	require.NoError(t, waitTask(h.Do(func(tx *world.Tx, e world.Entity) {
		e.(*player.Player).Teleport(centre.Vec3Middle())
	})))

	var dim world.Dimension
	var pos mgl64.Vec3
	require.Eventually(t, func() bool {
		ok := waitTask(h.Do(func(tx *world.Tx, e world.Entity) {
			dim, pos = tx.World().Dimension(), e.Position()
		})) == nil
		return ok && dim == world.End
	}, 30*time.Second, 100*time.Millisecond, "the player did not reach the End (in %v at %v)", dim, pos)
	require.InDelta(t, 100.5, pos.X(), 0.01, "arrival x")
	require.InDelta(t, 0.5, pos.Z(), 0.01, "arrival z")
	require.InDelta(t, 49, pos.Y(), 0.01, "arrival y")

	end := srv.Server.End()
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			_, ok := blockAt(t, end, cube.Pos{100 + dx, 48, dz}).(block.Obsidian)
			require.True(t, ok, "the arrival platform has no obsidian at %d, 48, %d", 100+dx, dz)
			for dy := 0; dy < 3; dy++ {
				_, air := blockAt(t, end, cube.Pos{100 + dx, 49 + dy, dz}).(block.Air)
				require.True(t, air, "the arrival platform is not clear at %d, %d, %d", 100+dx, 49+dy, dz)
			}
		}
	}
	// Arriving builds the platform: its chunks are made by the chunk workers, never on the End's tick.
	require.Equal(t, onTick, app.GenerationsOnTick(), "chunks were generated on a world's tick")
}

// waitTask waits up to 10 s for a scheduled entity task.
func waitTask(task *world.Task) error {
	select {
	case <-task.Done():
		return task.Err()
	case <-time.After(10 * time.Second):
		return os.ErrDeadlineExceeded
	}
}
