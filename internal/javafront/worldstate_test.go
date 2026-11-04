package javafront_test

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// worldSeen is what the Java bot was sent of the world's state.
type worldSeen struct {
	Time []struct {
		Clocks []struct {
			ID   int32   `json:"id"`
			Time int64   `json:"time"`
			Rate float32 `json:"rate"`
		} `json:"clocks"`
	} `json:"time"`
	GameEvents []struct {
		Reason string  `json:"reason"`
		Value  float32 `json:"value"`
	} `json:"gameEvents"`
	Sounds []struct {
		Name     string  `json:"name"`
		Known    bool    `json:"known"`
		Category string  `json:"category"`
		X        float64 `json:"x"`
		Volume   float32 `json:"volume"`
		Pitch    float32 `json:"pitch"`
	} `json:"sounds"`
	WorldEvents []struct {
		ID      int32 `json:"id"`
		X, Y, Z int
		Data    int32 `json:"data"`
	} `json:"worldEvents"`
}

func (b *javaBot) world() (w worldSeen) { b.call("world", nil, &w); return w }

// lastClock is the last Overworld clock the bot was sent: its time and rate.
func lastClock(w worldSeen) (time int64, rate float32, ok bool) {
	for i := len(w.Time) - 1; i >= 0; i-- {
		for _, c := range w.Time[i].Clocks {
			return c.Time, c.Rate, true
		}
	}
	return 0, 0, false
}

func doWorld(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	_, err := world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) { f(tx); return struct{}{}, nil })
	require.NoError(t, err)
}

// The time of day reaches Java as the Overworld clock, running while the day cycles and still while it
// does not, and the weather as Java's game events: rain begins (its level 1), thunder (its level 1), and
// both end.
func TestJavaSeesTimeAndWeather(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JSky")
	waitOnGround(t, srv, "JSky")
	w := srv.World()

	w.SetTime(13000)
	require.Eventually(t, func() bool {
		tm, rate, ok := lastClock(j.world())
		return ok && tm >= 13000 && tm < 14000 && rate == 1
	}, 5*time.Second, 50*time.Millisecond, "Java was not sent the night: %+v", j.world().Time)
	w.StopTime()
	require.Eventually(t, func() bool { _, rate, ok := lastClock(j.world()); return ok && rate == 0 }, 5*time.Second, 50*time.Millisecond, "the clock did not stop")
	w.StartTime()
	require.Eventually(t, func() bool { _, rate, ok := lastClock(j.world()); return ok && rate == 1 }, 5*time.Second, 50*time.Millisecond, "the clock did not start again")

	events := func() []string {
		var out []string
		for _, e := range j.world().GameEvents {
			switch e.Reason {
			case "start_raining", "stop_raining":
				out = append(out, e.Reason)
			case "rain_level_change":
				out = append(out, map[float32]string{0: "rain 0", 1: "rain 1"}[e.Value])
			case "thunder_level_change":
				out = append(out, map[float32]string{0: "thunder 0", 1: "thunder 1"}[e.Value])
			}
		}
		return out
	}
	w.StartThundering(time.Hour)
	want := []string{"start_raining", "rain 1", "thunder 1"}
	require.Eventually(t, func() bool { return len(events()) >= 3 }, 5*time.Second, 50*time.Millisecond, "no storm reached Java: %v", events())
	require.Equal(t, want, events())
	w.StopRaining()
	want = append(want, "stop_raining", "rain 0", "thunder 0")
	require.Eventually(t, func() bool { return len(events()) >= 6 }, 5*time.Second, 50*time.Millisecond, "the storm's end did not reach Java: %v", events())
	require.Equal(t, want, events())
}

// Everyone asleep at night skips to morning, and the Java client is sent the new time.
func TestJavaSeesNightSkipped(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JSleeper")
	waitOnGround(t, srv, "JSleeper")
	w := srv.World()
	setBlock(t, srv, cube.Pos{1, -60, 1}, block.Bed{Colour: item.ColourRed(), Facing: cube.South})
	setBlock(t, srv, cube.Pos{1, -60, 2}, block.Bed{Colour: item.ColourRed(), Facing: cube.South, Head: true})
	w.SetTime(14000)
	j.place(cube.Pos{1, -60, 1}, cube.FaceUp, 0.5, 0.5, 0.5, 1)
	require.Eventually(t, func() bool {
		tm, _, ok := lastClock(j.world())
		return ok && tm%24000 < 1000
	}, 15*time.Second, 100*time.Millisecond, "Java never saw the morning after sleeping: %+v", j.world().Time)
	require.Less(t, w.Time()%24000, 1000, "the night was not skipped")
}

// Sounds both editions hear reach Java by name, as the vanilla game plays them: an explosion, a door, a
// chest; lightning as thunder and impact, with no explosion. A block another player breaks shows its
// breaking (level event 2001, with the block's Java state); one the Java player breaks itself does not,
// as its client showed it already.
func TestJavaHearsTheWorld(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JEars")
	waitOnGround(t, srv, "JEars")
	w := srv.World()
	heard := func(name string) bool {
		for _, s := range j.world().Sounds {
			if s.Name == name {
				return true
			}
		}
		return false
	}
	for _, c := range []struct {
		s    world.Sound
		name string
	}{
		{sound.Explosion{}, "entity.generic.explode"},
		{sound.DoorOpen{Block: block.WoodDoor{Wood: block.OakWood()}}, "block.wooden_door.open"},
		{sound.DoorClose{Block: block.WoodDoor{Wood: block.CherryWood()}}, "block.cherry_wood_door.close"},
		{sound.ChestOpen{}, "block.chest.open"},
		{sound.Fizz{}, "block.lava.extinguish"},
	} {
		doWorld(t, w, func(tx *world.Tx) { tx.PlaySound(mgl64.Vec3{3.5, -59.5, 3.5}, c.s) })
		require.Eventually(t, func() bool { return heard(c.name) }, 5*time.Second, 50*time.Millisecond, "Java did not hear %s: %+v", c.name, j.world().Sounds)
	}
	for _, s := range j.world().Sounds {
		require.True(t, s.Known, "%s is not a Java sound", s.Name)
		require.InDelta(t, 3.5, s.X, 0.13)
	}

	// Lightning, struck as a storm strikes.
	explosions := func() (n int) {
		for _, s := range j.world().Sounds {
			if s.Name == "entity.generic.explode" {
				n++
			}
		}
		return n
	}
	before := explosions()
	doWorld(t, w, func(tx *world.Tx) {
		tx.AddEntity(entity.NewLightning(world.EntitySpawnOpts{Position: mgl64.Vec3{6, -60, 6}}))
	})
	require.Eventually(t, func() bool { return heard("entity.lightning_bolt.thunder") && heard("entity.lightning_bolt.impact") }, 5*time.Second, 50*time.Millisecond, "no thunder")
	require.Equal(t, before, explosions(), "lightning was heard as an explosion too")

	// A block broken by the world shows its breaking to Java.
	stone := block.Stone{}
	doWorld(t, w, func(tx *world.Tx) { tx.AddParticle(mgl64.Vec3{4.5, -61.5, 4.5}, particle.BlockBreak{Block: stone}) })
	require.Eventually(t, func() bool {
		for _, e := range j.world().WorldEvents {
			if e.ID == 2001 && e.X == 4 && e.Y == -62 && e.Z == 4 {
				require.EqualValues(t, java261.JavaState(world.BlockRuntimeID(stone)), e.Data)
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Java did not see the block break: %+v", j.world().WorldEvents)

	// One the Java player breaks itself, at once (short grass), does not.
	at := cube.Pos{2, -60, 0}
	setBlock(t, srv, at, block.ShortGrass{})
	j.dig(0, at, 1, 7)
	require.Eventually(t, func() bool { _, air := blockAt(t, srv, at).(block.Air); return air }, 5*time.Second, 50*time.Millisecond, "the block was not broken")
	time.Sleep(500 * time.Millisecond)
	for _, e := range j.world().WorldEvents {
		require.False(t, e.ID == 2001 && e.X == at[0] && e.Y == at[1] && e.Z == at[2], "Java was shown its own break")
	}
}
