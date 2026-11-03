package app_test

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/app"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/testserver"
	"github.com/overmind/overmind/internal/worldgen"
)

// Seeds that must all work: small, negative, past 32 bits and the int64 limits.
// Bedrock's own handling of the negative and the 2^31-and-up ones is not verified.
var testSeeds = []int64{
	0, 1, -1, 2, 7, 42, 100, -42, 12345, 987654321, 1 << 31, -(1 << 31) - 1, 1 << 40, -(1 << 40),
	math.MaxInt32, math.MinInt32, math.MaxInt64, math.MinInt64, 20260423, -7777777777,
}

func withSeed(seed int64) func(*config.Config) {
	return func(c *config.Config) { c.Seed = &seed }
}

// fastTicks sets the seed and a raised random tick speed.
func fastTicks(seed int64, speed int) func(*config.Config) {
	return func(c *config.Config) { c.Seed = &seed; c.RandomTickSpeed = speed }
}

// run executes f in a world transaction and waits for it.
func run(t *testing.T, w *world.World, f func(tx *world.Tx)) {
	t.Helper()
	task := w.Do(f)
	select {
	case <-task.Done():
		require.NoError(t, task.Err())
	case <-time.After(60 * time.Second):
		t.Fatal("world transaction did not finish in 60 s")
	}
}

func blockAt(t *testing.T, w *world.World, pos cube.Pos) (b world.Block) {
	t.Helper()
	run(t, w, func(tx *world.Tx) { b = tx.Block(pos) })
	return b
}

// viewer counts the chunks it is shown.
type viewer struct {
	world.NopViewer
	chunks atomic.Int64
}

func (v *viewer) ViewChunk(world.ChunkPos, world.Dimension, map[cube.Pos]world.Block, *chunk.Chunk) {
	v.chunks.Add(1)
}

// watch keeps the chunks within radius of pos loaded and ticking, as a player would, and waits
// until all of them have arrived. The returned func stops it.
func watch(t *testing.T, w *world.World, pos cube.Pos, radius int) (stop func()) {
	t.Helper()
	v := &viewer{}
	l := world.NewLoader(radius, w, v)
	// The loader keeps chunks whose rounded distance from the centre is within the radius: a
	// square with its corners cut when the radius is 2 or more.
	var want int64
	for x := -radius; x <= radius; x++ {
		for z := -radius; z <= radius; z++ {
			if int(math.Round(math.Hypot(float64(x), float64(z)))) <= radius {
				want++
			}
		}
	}
	run(t, w, func(tx *world.Tx) {
		l.Move(tx, pos.Vec3Centre())
		l.ChangeRadius(tx, radius) // fills the load queue even when the loader is already at pos's chunk
		l.Load(tx, int(want))
	})
	require.Eventually(t, func() bool { return v.chunks.Load() >= want }, 60*time.Second, 50*time.Millisecond, "chunks did not arrive")
	return func() { run(t, w, func(tx *world.Tx) { l.Close(tx) }) }
}

func TestBotSpawnsOnLand(t *testing.T) {
	// No server per seed: Dragonfly adds its creative items and recipes to global lists every time
	// a server is made, so a test process that makes many of them breaks later joins.
	for _, seed := range testSeeds {
		gen := worldgen.New(seed)
		spawn := gen.DefaultSpawn(world.Overworld)
		c := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
		gen.GenerateChunk(world.ChunkPos{int32(spawn.X() >> 4), int32(spawn.Z() >> 4)}, c)
		at := func(dy int) world.Block {
			b, ok := world.BlockByRuntimeID(c.Block(uint8(spawn.X()&15), int16(spawn.Y()+dy), uint8(spawn.Z()&15), 0))
			require.True(t, ok, "seed %d: unknown block at %v", seed, spawn)
			return b
		}
		name, _ := at(-1).EncodeBlock()
		require.Greater(t, spawn.Y(), 62, "seed %d: spawn %v is not above sea level", seed, spawn)
		require.False(t, strings.Contains(name, "leaves") || strings.Contains(name, "log") || strings.Contains(name, "water") ||
			strings.Contains(name, "lava") || name == "minecraft:air", "seed %d: spawn %v stands on %s", seed, spawn, name)
		require.True(t, at(-1).Model().FaceSolid(cube.Pos{}, cube.FaceUp, nil), "seed %d: spawn %v stands on %s, which has no solid top", seed, spawn, name)
		_, feet := at(0).(block.Air)
		_, head := at(1).(block.Air)
		require.True(t, feet && head, "seed %d: spawn %v has no two air blocks above the ground", seed, spawn)
		require.LessOrEqual(t, abs(spawn.X())+abs(spawn.Z()), 4096, "seed %d: spawn %v is very far from the origin", seed, spawn)
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func TestBotGetsTerrainChunks(t *testing.T) {
	srv := testserver.StartIn(t, t.TempDir(), withSeed(1234))
	bot, err := bedrock.Join(srv.BedrockAddr, "Looker")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("Looker") }, 15*time.Second, 100*time.Millisecond)
	require.Eventually(t, func() bool { return bot.ChunksReceived() >= 20 }, 30*time.Second, 100*time.Millisecond)

	// What the bot was sent is what the server holds: terrain, not the flat world's 4 layers.
	spawn := srv.World().Spawn()
	require.Equal(t, worldgen.New(1234).DefaultSpawn(world.Overworld), spawn, "the server did not use the generator's spawn")
	heights := map[int]bool{}
	for dx := -64; dx <= 64; dx += 16 {
		for dz := -64; dz <= 64; dz += 16 {
			heights[topY(t, srv.World(), spawn.X()+dx, spawn.Z()+dz)] = true
		}
	}
	require.Greater(t, len(heights), 3, "the ground around spawn is flat: %v", heights)
	require.Greater(t, bot.ChunksReceived(), int64(20))
}

// topY is the Y of the highest block that is not air.
func topY(t *testing.T, w *world.World, x, z int) (y int) {
	t.Helper()
	run(t, w, func(tx *world.Tx) {
		for y = 319; y > -64; y-- {
			if _, air := tx.Block(cube.Pos{x, y, z}).(block.Air); !air {
				return
			}
		}
	})
	return y
}

func TestSeedSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	// No seed set: one is picked at random and must then be kept.
	first := testserver.StartIn(t, dir, nil)
	spawn := first.World().Spawn()
	col := column(t, first.World(), spawn.X()+40, spawn.Z()-24)
	require.NoError(t, first.Close())

	second := testserver.StartIn(t, dir, nil)
	require.Equal(t, spawn, second.World().Spawn(), "spawn moved after a restart")
	require.Equal(t, col, column(t, second.World(), spawn.X()+40, spawn.Z()-24), "an untouched chunk changed after a restart")
	require.NoError(t, second.Close())

	// A different seed in the settings must not silently change an existing world.
	_, err := testserver.TryStartIn(t, dir, withSeed(99))
	require.Error(t, err)
	require.Contains(t, err.Error(), "seed")
}

// column lists the blocks of a whole column as encoded names.
func column(t *testing.T, w *world.World, x, z int) string {
	t.Helper()
	var sb strings.Builder
	run(t, w, func(tx *world.Tx) {
		for y := -64; y < 320; y++ {
			name, _ := tx.Block(cube.Pos{x, y, z}).EncodeBlock()
			sb.WriteString(name)
			sb.WriteByte(' ')
		}
	})
	return sb.String()
}

func TestSeedsPersistInWorldFolder(t *testing.T) {
	for _, seed := range []int64{math.MinInt64, math.MaxInt64} {
		dir := t.TempDir()
		first := testserver.StartIn(t, dir, withSeed(seed))
		spawn := first.World().Spawn()
		require.NoError(t, first.Close())
		// Restart with no seed in the settings: the stored one is used.
		second := testserver.StartIn(t, dir, nil)
		require.Equal(t, spawn, second.World().Spawn(), "seed %d", seed)
		require.NoError(t, second.Close())
	}
}

func TestPlayerChangesSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	first := testserver.StartIn(t, dir, withSeed(31337))
	spawn := first.World().Spawn()
	dug := spawn.Side(cube.FaceDown)
	built := cube.Pos{spawn.X() + 3, spawn.Y() + 30, spawn.Z() + 3}
	run(t, first.World(), func(tx *world.Tx) {
		tx.SetBlock(dug, block.Air{}, nil)
		tx.SetBlock(built, block.Diamond{}, nil)
	})
	require.NoError(t, first.Close())

	second := testserver.StartIn(t, dir, nil)
	_, air := blockAt(t, second.World(), dug).(block.Air)
	require.True(t, air, "a dug block came back")
	_, diamond := blockAt(t, second.World(), built).(block.Diamond)
	require.True(t, diamond, "a placed block was lost")
	require.NoError(t, second.Close())
}

// lab builds a clear 25 x 25 stone floor 40 blocks above the spawn and returns the floor's centre.
// It is high enough to be above any tree, and in full sky light. It is wide because water that
// finds a drop within a few blocks runs only towards it, as in the game.
func lab(t *testing.T, w *world.World) cube.Pos {
	t.Helper()
	spawn := w.Spawn()
	c := cube.Pos{spawn.X(), min(spawn.Y()+40, 300), spawn.Z()}
	run(t, w, func(tx *world.Tx) {
		for dx := -12; dx <= 12; dx++ {
			for dz := -12; dz <= 12; dz++ {
				tx.SetBlock(c.Add(cube.Pos{dx, 0, dz}), block.Stone{}, nil)
				for dy := 1; dy <= 6; dy++ {
					tx.SetBlock(c.Add(cube.Pos{dx, dy, dz}), block.Air{}, nil)
				}
			}
		}
	})
	return c
}

func isWater(b world.Block) bool { _, ok := b.(block.Water); return ok }

func TestWaterFlows(t *testing.T) {
	srv := testserver.StartIn(t, t.TempDir(), withSeed(5))
	w := srv.World()
	c := lab(t, w)
	defer watch(t, w, c, 2)()

	// A water source held back by one stone block, the way a lake bank holds it.
	src, wall := c.Add(cube.Pos{-1, 1, 0}), c.Add(cube.Pos{0, 1, 0})
	run(t, w, func(tx *world.Tx) {
		tx.SetBlock(wall, block.Stone{}, nil)
		tx.SetLiquid(src, block.Water{Still: true, Depth: 8})
	})
	time.Sleep(time.Second)
	require.False(t, isWater(blockAt(t, w, wall.Add(cube.Pos{1, 0, 0}))), "water got past the wall")

	run(t, w, func(tx *world.Tx) { tx.SetBlock(wall, block.Air{}, nil) })
	require.Eventually(t, func() bool {
		return isWater(blockAt(t, w, wall)) && isWater(blockAt(t, w, wall.Add(cube.Pos{2, 0, 0})))
	}, 10*time.Second, 100*time.Millisecond, "water did not spread through the gap")
}

func TestSandFalls(t *testing.T) {
	srv := testserver.StartIn(t, t.TempDir(), withSeed(6))
	w := srv.World()
	c := lab(t, w)
	defer watch(t, w, c, 2)()

	// Sand on a one-block pillar, three blocks up; take the pillar away.
	pillar := c.Add(cube.Pos{0, 1, 0})
	sand := pillar.Side(cube.FaceUp)
	run(t, w, func(tx *world.Tx) {
		tx.SetBlock(pillar, block.Stone{}, nil)
		tx.SetBlock(sand, block.Sand{}, nil)
		tx.SetBlock(pillar, block.Air{}, nil)
	})
	landed := c.Side(cube.FaceUp)
	require.Eventually(t, func() bool {
		_, isSand := blockAt(t, w, landed).(block.Sand)
		_, left := blockAt(t, w, sand).(block.Air)
		return isSand && left
	}, 10*time.Second, 100*time.Millisecond, "sand did not fall to the floor")
}

func TestDayCycleAdvances(t *testing.T) {
	srv := testserver.StartIn(t, t.TempDir(), withSeed(8))
	w := srv.World()
	defer watch(t, w, w.Spawn(), 1)()
	before := w.Time()
	require.Eventually(t, func() bool { return w.Time() >= before+20 }, 10*time.Second, 100*time.Millisecond, "time stuck at %d", before)
}

func TestLeavesDecay(t *testing.T) {
	srv := testserver.StartIn(t, t.TempDir(), fastTicks(1234, 200))
	w := srv.World()
	spawn := w.Spawn()
	defer watch(t, w, spawn, 6)()

	// Find the tree nearest spawn, cut every log within 8 blocks of it, and note which leaves are left
	// with no log within 4 blocks through other leaves: the game's rule for a leaf to survive. Leaves
	// decay on random ticks, which reach only the tick distance around a player, so the search goes
	// outwards ring by ring and stops at the first tree.
	var tree cube.Pos
	var orphans []cube.Pos
	run(t, w, func(tx *world.Tx) {
		found := false
		for r := 0; r <= 60 && !found; r++ {
			for dx := -r; dx <= r && !found; dx++ {
				for dz := -r; dz <= r && !found; dz++ {
					if max(abs(dx), abs(dz)) != r {
						continue
					}
					for y := 140; y > 55; y-- {
						p := cube.Pos{spawn.X() + dx, y, spawn.Z() + dz}
						if _, ok := tx.Block(p).(block.Log); ok {
							tree, found = p, true
							break
						}
					}
				}
			}
		}
		if !found {
			return
		}
		var leaves []cube.Pos
		for dx := -12; dx <= 12; dx++ {
			for dz := -12; dz <= 12; dz++ {
				for dy := -3; dy <= 20; dy++ {
					p := tree.Add(cube.Pos{dx, dy, dz})
					switch tx.Block(p).(type) {
					case block.Log:
						if abs(dx) <= 8 && abs(dz) <= 8 {
							tx.SetBlock(p, block.Air{}, nil)
						}
					case block.Leaves:
						leaves = append(leaves, p)
					}
				}
			}
		}
		for _, p := range leaves {
			if !logNear(tx, p) {
				orphans = append(orphans, p)
			}
		}
	})
	require.NotEqual(t, cube.Pos{}, tree, "no tree within 60 blocks of spawn %v", spawn)
	require.GreaterOrEqual(t, len(orphans), 5, "the tree at %v left too few leaves without a log", tree)

	// Dragonfly updates a leaf only when a neighbour changes, so the ones right by a cut log go
	// first and the rest follow as the leaves between them fall. Most must go.
	gone := func() int {
		n := 0
		for _, p := range orphans {
			if _, ok := blockAt(t, w, p).(block.Leaves); !ok {
				n++
			}
		}
		return n
	}
	require.Eventually(t, func() bool { return gone()*2 >= len(orphans) }, 60*time.Second, 500*time.Millisecond,
		"only %d of %d leaves without a log decayed", gone(), len(orphans))
	t.Logf("%d of %d leaves without a log had decayed", gone(), len(orphans))
}

// logNear says whether a log is within 4 blocks of p, counted through leaves, as the game decides
// whether a leaf stays.
func logNear(tx *world.Tx, p cube.Pos) bool {
	seen, step := map[cube.Pos]bool{p: true}, []cube.Pos{p}
	for d := 0; len(step) > 0; d++ {
		var next []cube.Pos
		for _, at := range step {
			switch tx.Block(at).(type) {
			case block.Log, block.Wood:
				return true
			case block.Leaves:
				if d < 4 {
					at.Neighbours(func(n cube.Pos) {
						if !seen[n] {
							seen[n] = true
							next = append(next, n)
						}
					}, tx.Range())
				}
			}
		}
		step = next
	}
	return false
}

func TestGrassSpreads(t *testing.T) {
	srv := testserver.StartIn(t, t.TempDir(), fastTicks(9, 1000))
	w := srv.World()
	c := lab(t, w)
	// Grass spreads on random ticks, which reach two chunks under a viewer's radius (the Dragonfly
	// fork's per-player tick reach), so the viewer looks 4 chunks out to tick the lab's chunks.
	defer watch(t, w, c, 4)()

	// A row of dirt with grass at one end, open sky above.
	run(t, w, func(tx *world.Tx) {
		for dx := -3; dx <= 3; dx++ {
			tx.SetBlock(c.Add(cube.Pos{dx, 0, 0}), block.Dirt{}, nil)
		}
		tx.SetBlock(c.Add(cube.Pos{-3, 0, 0}), block.Grass{}, nil)
	})
	far := c.Add(cube.Pos{3, 0, 0})
	require.Eventually(t, func() bool {
		_, ok := blockAt(t, w, far).(block.Grass)
		return ok
	}, 60*time.Second, 500*time.Millisecond, "grass did not spread along the dirt")
}

// Four bots walking apart into new ground make the server generate chunks on its workers as fast
// as it can; the world must keep ticking while they do, and no chunk may be generated on the tick.
// A fifth asks for a view distance of 3, under the tick distance plus 2: its ticks must still stay
// clear of chunks it has not loaded (the Dragonfly fork holds each player's tick reach two under
// its view distance).
func TestGenerationDoesNotStallTicks(t *testing.T) {
	onTick := app.GenerationsOnTick()
	srv := testserver.StartIn(t, t.TempDir(), fastTicks(2468, 50))
	var bots []*bedrock.Bot
	for i := range 5 {
		name := fmt.Sprintf("Runner%d", i)
		bot, err := bedrock.Join(srv.BedrockAddr, name)
		require.NoError(t, err)
		defer bot.Close()
		require.Eventually(t, func() bool { return srv.PlayerByName(name) }, 15*time.Second, 100*time.Millisecond)
		heading := float64(i) * math.Pi / 2
		if i == 4 {
			require.NoError(t, bot.SetViewDistance(3))
			heading = math.Pi / 4 // Its own way, so no other bot's view covers its edge.
		}
		bot.Walk(heading)
		bots = append(bots, bot)
	}

	start, worst := time.Now(), time.Duration(0)
	t0 := srv.World().Time()
	for time.Since(start) < 20*time.Second {
		a := time.Now()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err := srv.World().Do(func(tx *world.Tx) {}).Wait(ctx)
		cancel()
		require.NoError(t, err)
		worst = max(worst, time.Since(a))
		time.Sleep(20 * time.Millisecond)
	}
	ticks := srv.World().Time() - t0
	var sent int64
	for _, b := range bots {
		sent += b.ChunksReceived()
	}
	t.Logf("5 bots walking apart (one at view distance 3), %d chunk workers per world, random ticks raised to 50: worst empty-transaction wait %v; ticks in 20 s: %d (20 is normal); chunks sent: %d",
		max(1, runtime.NumCPU()/2), worst, ticks, sent)
	require.Less(t, worst, 500*time.Millisecond, "a world transaction waited %v while chunks were generated", worst)
	require.Greater(t, ticks, 20*17, "the world ran slow: %d ticks in 20 s", ticks)
	require.Equal(t, onTick, app.GenerationsOnTick(), "chunks were generated on the world's tick")
}

// A lone client at view distance 3 walking into new ground, with random ticks raised so blocks at
// the edge of its view tick often: no tick may reach past what it has loaded, so none generates a
// chunk (the Dragonfly fork's per-player tick reach, tick.go tickCentres).
func TestSmallViewDistanceNeverGeneratesOnTick(t *testing.T) {
	onTick := app.GenerationsOnTick()
	srv := testserver.StartIn(t, t.TempDir(), fastTicks(97531, 300))
	bot, err := bedrock.Join(srv.BedrockAddr, "Short")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("Short") }, 15*time.Second, 100*time.Millisecond)
	require.NoError(t, bot.SetViewDistance(3))
	bot.Walk(math.Pi / 3)
	time.Sleep(30 * time.Second)
	require.Equal(t, onTick, app.GenerationsOnTick(), "a tick generated a chunk for a client at view distance 3")
}
