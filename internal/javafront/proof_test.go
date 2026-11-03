package javafront_test

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity/effect"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/javafront"
	"github.com/overmind/overmind/internal/javalogin"
	"github.com/overmind/overmind/internal/tables/java261"
	"github.com/overmind/overmind/internal/testserver"
)

// The default flat world: bedrock at Y -64, dirt at -63 and -62, grass at -61.
// Feet rest on top of the grass, at Y -60.
const groundY = -60.0

func TestJavaBotJoinsAndGetsChunks(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Chunky")

	var col columnReply
	require.Eventually(t, func() bool {
		col = bot.column(5, 7, -64, -58)
		return col.Loaded
	}, 20*time.Second, 100*time.Millisecond, "column (5,7) never arrived")

	want := map[int]string{-64: "bedrock", -63: "dirt", -62: "dirt", -61: "grass_block", -60: "air", -59: "air", -58: "air"}
	for y, name := range want {
		require.Equal(t, name, col.Blocks[y].Block, "block at Y %d", y)
		require.Equal(t, "plains", col.Blocks[y].Biome, "biome at Y %d", y)
	}
	require.Equal(t, 15, col.Blocks[-60].Sky, "the world should be full-bright")
	require.Contains(t, col.Heightmaps, "motion_blocking")
	require.Contains(t, col.Heightmaps, "world_surface")
	require.Equal(t, 4, col.Height, "heightmap should hold the first air above the ground, counted from Y -64")

	s := bot.state()
	require.NotNil(t, s.Seen.Login, "no Login (play)")
	require.Equal(t, []string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"}, s.Seen.Login.WorldNames)
	require.NotNil(t, s.Seen.Spawn, "no spawn position")
	require.Equal(t, "minecraft:overworld", s.Seen.Spawn.GlobalPos.DimensionName)
	require.Positive(t, s.Seen.Packets["game_state_change"], "client was never told chunks are coming")
	require.Positive(t, s.Seen.Packets["update_view_position"], "no chunk centre")
	require.Positive(t, s.Seen.Batches, "no chunk batch finished")
	require.True(t, s.Me.Known, "no player position")
	require.InDelta(t, 0.5, s.Me.X, 1, "spawned away from the world spawn")
}

// Fills sections with increasing variety, so every palette kind gets decoded by
// prismarine-chunk: single value, indirect at 4 bits and wider, and direct, for
// blocks and for biomes.
func TestEveryPaletteKindDecodes(t *testing.T) {
	srv := testserver.Start(t)

	var kinds []world.Block
	for _, b := range world.Blocks() {
		if settles(b) || java261.JavaState(world.BlockRuntimeID(b)) == 0 {
			continue
		}
		kinds = append(kinds, b)
	}
	require.Greater(t, len(kinds), 300, "not enough block states for a direct palette")
	biomes := world.Biomes()
	require.Greater(t, len(biomes), 16)

	const x, z = 32, 32 // chunk (2,2), inside everyone's view
	fill := func(tx *world.Tx, baseY, n int, biomeKinds int) {
		i := 0
		for y := baseY; y < baseY+16; y++ {
			for zz := z; zz < z+16; zz++ {
				for xx := x; xx < x+16; xx++ {
					tx.SetBlock(cube.Pos{xx, y, zz}, kinds[i%n], nil)
					tx.SetBiome(cube.Pos{xx, y, zz}, biomes[(xx/4+zz/4*4+y/4*16)%biomeKinds])
					i++
				}
			}
		}
	}
	type section struct{ baseY, blocks, biomes int }
	sections := []section{
		{0, 1, 1},     // single value
		{16, 10, 2},   // 4 bits, biomes 1 bit
		{32, 30, 4},   // 5 bits, biomes 2 bits
		{48, 200, 8},  // 8 bits, biomes 3 bits
		{64, 300, 16}, // direct, biomes direct
	}
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		for _, s := range sections {
			fill(tx, s.baseY, s.blocks, s.biomes)
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)

	bot := startBot(t, srv, "Palette")
	require.Eventually(t, func() bool { return bot.column(x, z, 0, 0).Loaded }, 20*time.Second, 100*time.Millisecond)

	for _, s := range sections {
		i := 0
		for y := s.baseY; y < s.baseY+16; y++ {
			for zz := z; zz < z+16; zz++ {
				for xx := x; xx < x+16; xx++ {
					if xx == x && (zz-z)%5 == 0 { // one column in 80 keeps the bot calls down
						col := bot.column(xx, zz, y, y)
						want := bot.stateName(int32(java261.JavaState(world.BlockRuntimeID(kinds[i%s.blocks]))))
						require.Equal(t, want, col.Blocks[y].Block, "block at %d,%d,%d in the %d-kind section", xx, y, zz, s.blocks)
						wantBiome := biomes[(xx/4+zz/4*4+y/4*16)%s.biomes].String()
						require.Equal(t, javaBiomeName(wantBiome), col.Blocks[y].Biome, "biome at %d,%d,%d in the %d-biome section", xx, y, zz, s.biomes)
					}
					i++
				}
			}
		}
	}
}

// settles reports whether a block might change by itself once placed, or needs
// data of its own, which would make it useless for checking what was encoded.
func settles(b world.Block) bool {
	switch b.(type) {
	case world.NBTer, world.NeighbourUpdateTicker, world.RandomTicker, world.ScheduledTicker, world.TickerBlock:
		return true
	}
	return false
}

// javaBiomeName is Dragonfly's biome name as Java spells it, or plains when
// Java does not have it, as the front end sends.
func javaBiomeName(name string) string {
	if java261.JavaBiome(name) < 0 {
		return "plains"
	}
	return name
}

func TestJavaGetsKeepAliveAndStaysOnline(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Alive")
	require.Eventually(t, func() bool { return bot.state().Seen.KeepAlives >= 2 }, 25*time.Second, 200*time.Millisecond, "the keep-alive did not repeat")
	require.True(t, srv.JavaOnline("Alive"))
}

func TestJavaBotStandsOnGround(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Stander")
	require.Eventually(t, func() bool {
		pos, onGround, ok := srv.JavaPosition("Stander")
		return ok && onGround && math.Abs(pos.Y()-groundY) < 0.01
	}, 20*time.Second, 100*time.Millisecond, "the server never saw the player on the ground")

	// It stays there: nothing on the server pushes it around.
	time.Sleep(time.Second)
	pos, onGround, ok := srv.JavaPosition("Stander")
	require.True(t, ok)
	require.True(t, onGround)
	require.InDelta(t, groundY, pos.Y(), 0.01)
	require.InDelta(t, groundY, bot.state().Me.Y, 0.01, "the bot itself should have landed on the decoded ground")
}

func TestJavaWalksBedrockSees(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "Watcher")
	require.NoError(t, err)
	defer watcher.Close()
	bot := startBot(t, srv, "JWalker")
	waitOnGround(t, srv, "JWalker")

	require.Eventually(t, func() bool { _, ok := watcher.Player("JWalker"); return ok }, 20*time.Second, 100*time.Millisecond, "Bedrock never saw the Java player")

	bot.walk(0.6)
	time.Sleep(3 * time.Second)
	bot.stop()
	time.Sleep(time.Second)

	pos, _, ok := srv.JavaPosition("JWalker")
	require.True(t, ok)
	require.Greater(t, pos.X(), 2.0, "the Java player did not move on the server")
	last := bot.state().Me
	require.InDelta(t, last.X, pos.X(), 0.01, "server position differs from the client's")
	require.InDelta(t, last.Z, pos.Z(), 0.01, "server position differs from the client's")

	seen, ok := watcher.Player("JWalker")
	require.True(t, ok)
	require.Less(t, seen.Pos.Sub(mgl64.Vec3{last.X, last.Y, last.Z}).Len(), 1.0, "Bedrock sees %v, Java is at %+v", seen.Pos, last)
	require.Less(t, angleDiff(seen.Yaw, last.Yaw), 2.0, "Bedrock sees yaw %v, Java has %v", seen.Yaw, last.Yaw)
}

func TestBedrockWalksJavaSees(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "JWatcher")
	waitOnGround(t, srv, "JWatcher")
	walker, err := bedrock.Join(srv.BedrockAddr, "BWalker")
	require.NoError(t, err)
	defer walker.Close()

	require.Eventually(t, func() bool { _, ok := bot.players()["BWalker"]; return ok }, 20*time.Second, 100*time.Millisecond, "Java never saw the Bedrock player")

	walker.Walk(1.1)
	time.Sleep(3 * time.Second)
	walker.Stop()
	time.Sleep(time.Second)

	want, ok := srv.PlayerPosition("BWalker")
	require.True(t, ok)
	require.Greater(t, want.Z(), 2.0, "the Bedrock player did not move on the server")
	seen := bot.players()["BWalker"]
	require.Less(t, mgl64.Vec3{seen.X, seen.Y, seen.Z}.Sub(want).Len(), 1.0, "Java sees %+v, server has %v", seen, want)
	require.Less(t, angleDiff(seen.Yaw, walker.Yaw()), 2.0, "Java sees yaw %v, Bedrock has %v", seen.Yaw, walker.Yaw())
	require.Less(t, angleDiff(seen.Head, walker.Yaw()), 2.0, "Java sees head yaw %v, Bedrock has %v", seen.Head, walker.Yaw())
}

func TestJavaBreaksBedrockSees(t *testing.T) {
	// The world's default game mode is creative unless told otherwise.
	t.Run("survival", func(t *testing.T) {
		srv := testserver.Start(t)
		srv.World().SetDefaultGameMode(world.GameModeSurvival)
		watcher, err := bedrock.Join(srv.BedrockAddr, "BWatch")
		require.NoError(t, err)
		defer watcher.Close()
		bot := startBot(t, srv, "JBreaker")
		waitOnGround(t, srv, "JBreaker")

		target := cube.Pos{1, -61, 1}
		bot.dig(0, target, 1, 7)
		time.Sleep(300 * time.Millisecond)
		require.False(t, watcher.BlockIsAir(target), "starting to dig broke the block at once")
		bot.dig(2, target, 1, 8)

		require.Eventually(t, func() bool { return watcher.BlockIsAir(target) }, 5*time.Second, 50*time.Millisecond, "Bedrock never saw the block become air")
		require.Eventually(t, func() bool { return bot.block(target).Name == "air" }, 5*time.Second, 50*time.Millisecond, "the breaker never got the block change")
		require.Contains(t, bot.block(target).Acks, 8, "the dig was never acknowledged")
	})
	t.Run("creative", func(t *testing.T) {
		srv := testserver.Start(t)
		srv.World().SetDefaultGameMode(world.GameModeCreative)
		watcher, err := bedrock.Join(srv.BedrockAddr, "BWatch")
		require.NoError(t, err)
		defer watcher.Close()
		bot := startBot(t, srv, "JBreaker")
		waitOnGround(t, srv, "JBreaker")

		target := cube.Pos{1, -61, 1}
		bot.dig(0, target, 1, 7) // A creative client sends nothing more.

		require.Eventually(t, func() bool { return watcher.BlockIsAir(target) }, 5*time.Second, 50*time.Millisecond, "Bedrock never saw the block become air")
		require.Eventually(t, func() bool { return bot.block(target).Name == "air" }, 5*time.Second, 50*time.Millisecond, "the breaker never got the block change")
	})
}

func TestBedrockBreaksJavaSees(t *testing.T) {
	srv := testserver.Start(t)
	srv.World().SetDefaultGameMode(world.GameModeSurvival)
	bot := startBot(t, srv, "JSees")
	waitOnGround(t, srv, "JSees")
	breaker, err := bedrock.Join(srv.BedrockAddr, "BBreaker")
	require.NoError(t, err)
	defer breaker.Close()

	require.Eventually(t, func() bool { return bot.column(2, 2, -61, -61).Loaded }, 20*time.Second, 100*time.Millisecond)
	require.Equal(t, "grass_block", bot.column(2, 2, -61, -61).Blocks[-61].Block)

	target := cube.Pos{2, -61, 2}
	require.NoError(t, breaker.Break(target))

	require.Eventually(t, func() bool { return bot.block(target).Name == "air" }, 5*time.Second, 50*time.Millisecond, "Java never saw the block change")
}

func TestKilledJavaClientDisappears(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "Mourner")
	require.NoError(t, err)
	defer watcher.Close()
	bot := startBot(t, srv, "Doomed")
	waitOnGround(t, srv, "Doomed")
	require.Eventually(t, func() bool { _, ok := watcher.Player("Doomed"); return ok }, 20*time.Second, 100*time.Millisecond)

	bot.kill()

	require.Eventually(t, func() bool { return !srv.JavaOnline("Doomed") }, 10*time.Second, 100*time.Millisecond, "the server kept the killed player")
	require.Eventually(t, func() bool { _, ok := watcher.Player("Doomed"); return !ok }, 5*time.Second, 100*time.Millisecond, "Bedrock still sees the killed player")

	// The name is free again.
	startBot(t, srv, "Doomed")
	waitOnGround(t, srv, "Doomed")
}

func TestNameClashRefused(t *testing.T) {
	srv := testserver.Start(t)
	first := startBot(t, srv, "Taken")
	waitOnGround(t, srv, "Taken")

	t.Run("java differing only in case", func(t *testing.T) {
		second := startBotRaw(t, srv, "TAKEN")
		require.Contains(t, second.waitKicked(t), "already online")
		require.True(t, srv.JavaOnline("Taken"))
		require.False(t, first.ended(), "the first player lost its connection")
	})
	t.Run("bedrock after java", func(t *testing.T) {
		_, err := bedrock.Join(srv.BedrockAddr, "Taken")
		require.ErrorContains(t, err, "already online", "Bedrock joined under a name a Java player holds")
		require.True(t, srv.JavaOnline("Taken"))
	})
	t.Run("java after bedrock", func(t *testing.T) {
		b, err := bedrock.Join(srv.BedrockAddr, "BedrockName")
		require.NoError(t, err)
		defer b.Close()
		require.Eventually(t, func() bool { return srv.PlayerByName("BedrockName") }, 15*time.Second, 100*time.Millisecond)
		second := startBotRaw(t, srv, "BedrockName")
		require.Contains(t, second.waitKicked(t), "already online")
		require.False(t, srv.JavaOnline("BedrockName"))
	})
}

// The same account joining again takes over from the old session, as on vanilla.
func TestRejoinReplacesOldSession(t *testing.T) {
	srv := testserver.Start(t)
	first := startBot(t, srv, "Twice")
	waitOnGround(t, srv, "Twice")

	second := startBot(t, srv, "Twice")
	require.Contains(t, first.waitKicked(t), "another location")
	waitOnGround(t, srv, "Twice")
	require.False(t, second.ended())
}

func TestPlayerLimitCountsBothEditions(t *testing.T) {
	srv := testserver.StartWith(t, func(c *config.Config) { c.MaxPlayers = 1 })
	startBot(t, srv, "Only")
	waitOnGround(t, srv, "Only")

	_, err := bedrock.Join(srv.BedrockAddr, "BedrockToo")
	require.ErrorContains(t, err, "full", "a Bedrock player got past a full server")
	require.Contains(t, startBotRaw(t, srv, "JavaToo").waitKicked(t), "full")
}

// A Bedrock player still connecting holds its name against Java.
func TestNameIsReservedWhileBedrockConnects(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "OnTheWay")
	require.NoError(t, err)
	_ = b.Close()
	require.Eventually(t, func() bool { return !srv.PlayerByName("OnTheWay") }, 10*time.Second, 50*time.Millisecond)
	// Gone from the server, but within the reservation.
	require.Contains(t, startBotRaw(t, srv, "OnTheWay").waitKicked(t), "already online")
}

func TestLongMoveIsSentBack(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Leaper")
	waitOnGround(t, srv, "Leaper")
	before, _, _ := srv.JavaPosition("Leaper")

	bot.jump(20) // over vanilla's 10, under the 32 the player itself refuses

	require.Eventually(t, func() bool { return math.Abs(bot.state().Me.X-before.X()) < 1 }, 5*time.Second, 50*time.Millisecond, "the client was not sent back")
	pos, _, ok := srv.JavaPosition("Leaper")
	require.True(t, ok)
	require.InDelta(t, before.X(), pos.X(), 1, "the server took the long move")
}

func TestBedrockLongMoveIsRefused(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BLeaper")
	require.NoError(t, err)
	defer b.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("BLeaper") }, 15*time.Second, 50*time.Millisecond)
	before, ok := srv.PlayerPosition("BLeaper")
	require.True(t, ok)

	b.Jump(4096)
	time.Sleep(500 * time.Millisecond)

	pos, ok := srv.PlayerPosition("BLeaper")
	require.True(t, ok, "the world stopped answering")
	require.InDelta(t, before.X(), pos.X(), 1, "the server took the long move")
}

func TestFloodDisconnectsOnlyThatPlayer(t *testing.T) {
	srv := testserver.Start(t)
	good := startBot(t, srv, "Calm")
	bad := startBot(t, srv, "Flood")
	waitOnGround(t, srv, "Flood")

	bad.junk("flood")

	require.Eventually(t, func() bool { return !srv.JavaOnline("Flood") }, 10*time.Second, 50*time.Millisecond, "the flooder stayed online")
	require.True(t, srv.JavaOnline("Calm"))
	require.False(t, good.ended())
}

// A big edit reaches an honest viewer, whatever its size, and leaves it online.
func TestLargeBlockEditReachesViewer(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Viewer")
	waitOnGround(t, srv, "Viewer")
	require.Eventually(t, func() bool { return bot.column(36, 10, -61, -61).Loaded && bot.column(20, 10, -61, -61).Loaded }, 20*time.Second, 50*time.Millisecond)

	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		for y := -60; y < 30; y++ {
			for z := -64; z < 64; z++ {
				for x := -64; x < 16; x++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, bulkFill)
				}
			}
		}
		// A few edits alone in a section, for the other two ways of sending them.
		tx.SetBlock(cube.Pos{20, 100, 10}, block.Stone{}, nil)
		tx.SetBlock(cube.Pos{36, 100, 10}, block.Dirt{}, nil)
		tx.SetBlock(cube.Pos{37, 100, 10}, block.Dirt{}, nil)
		return struct{}{}, nil
	})
	require.NoError(t, err)

	time.Sleep(2 * time.Second)
	require.False(t, bot.ended(), "the viewer was dropped")
	require.True(t, srv.JavaOnline("Viewer"))
	require.Eventually(t, func() bool {
		return bot.block(cube.Pos{36, 100, 10}).Name == "dirt" && bot.block(cube.Pos{37, 100, 10}).Name == "dirt"
	}, 5*time.Second, 50*time.Millisecond, "section update not seen")
	require.Eventually(t, func() bool { return bot.block(cube.Pos{20, 100, 10}).Name == "stone" }, 5*time.Second, 50*time.Millisecond, "single block update not seen")
	require.Eventually(t, func() bool { return bot.column(8, 8, 0, 0).Blocks[0].Block == "stone" }, 10*time.Second, 50*time.Millisecond, "whole chunk not resent")
}

// The same, on chunks with more to them than the flat world: whole-chunk
// resends must be paced, not queued all at once.
func TestLargeEditOnVariedTerrainKeepsViewer(t *testing.T) {
	srv := testserver.Start(t)
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		for cx := -7; cx <= 7; cx++ {
			for cz := -7; cz <= 7; cz++ {
				for sy := -4; sy < 20; sy++ {
					tx.SetBlock(cube.Pos{cx*16 + 1, sy*16 + 1, cz*16 + 1}, block.Andesite{}, nil)
					tx.SetBlock(cube.Pos{cx*16 + 2, sy*16 + 2, cz*16 + 2}, block.Diorite{}, nil)
				}
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)
	bot := startBot(t, srv, "Viewer")
	waitOnGround(t, srv, "Viewer")
	require.Eventually(t, func() bool { return bot.state().Seen.Packets["map_chunk"] >= 110 }, 30*time.Second, 100*time.Millisecond, "the view never filled")

	_, err = world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		for y := -60; y < -55; y++ {
			for z := -112; z < 128; z++ {
				for x := -112; x < 128; x++ {
					tx.SetBlock(cube.Pos{x, y, z}, block.Stone{}, bulkFill)
				}
			}
		}
		return struct{}{}, nil
	})
	require.NoError(t, err)

	time.Sleep(3 * time.Second)
	require.False(t, bot.ended(), "the viewer was dropped")
	require.True(t, srv.JavaOnline("Viewer"))
	for _, at := range [][2]int{{8, 8}, {85, 40}, {-60, -70}} {
		require.Eventually(t, func() bool { return bot.column(at[0], at[1], -58, -58).Blocks[-58].Block == "stone" }, 30*time.Second, 100*time.Millisecond, "chunk at %v not resent", at)
	}
}

// Haste and the like count: a block the player breaks within a tick needs no
// second packet.
func TestHasteMakesBreakInstant(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Hasty")
	waitOnGround(t, srv, "Hasty")
	h, ok := srv.Java.Player("Hasty")
	require.True(t, ok)
	done := make(chan struct{})
	player.Do(h, func(_ *world.Tx, p *player.Player) {
		p.AddEffect(effect.New(effect.Haste, 100, time.Minute))
		close(done)
	})
	<-done

	target := cube.Pos{1, -61, 1}
	bot.dig(0, target, 1, 4)

	require.Eventually(t, func() bool { return bot.block(target).Name == "air" }, 5*time.Second, 50*time.Millisecond, "haste did not make the break instant")
}

func TestNewPlayersGetSurvivalByDefault(t *testing.T) {
	require.Equal(t, "survival", config.Default().GameMode)
	srv := testserver.Start(t)
	require.Equal(t, world.GameModeSurvival, srv.World().DefaultGameMode())
}

func TestFallDamageShowsOnJava(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Faller")
	waitOnGround(t, srv, "Faller")
	require.Eventually(t, func() bool { h := bot.state().Seen.Health; return h != nil && *h == 20 }, 5*time.Second, 50*time.Millisecond, "no health at join")

	bot.climb(groundY + 10)

	require.Eventually(t, func() bool { h := bot.state().Seen.Health; return h != nil && *h < 20 && *h > 0 }, 10*time.Second, 50*time.Millisecond, "a 10 block fall showed no damage")
}

func TestJavaDiesAndRespawns(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "Witness")
	require.NoError(t, err)
	defer watcher.Close()
	bot := startBot(t, srv, "Mortal")
	waitOnGround(t, srv, "Mortal")
	require.Eventually(t, func() bool { _, ok := watcher.Player("Mortal"); return ok }, 20*time.Second, 50*time.Millisecond)

	bot.climb(groundY + 60)

	require.Eventually(t, func() bool { h := bot.state().Seen.Health; return h != nil && *h <= 0 }, 20*time.Second, 50*time.Millisecond, "the client never saw itself die")
	bot.walk(0)
	time.Sleep(200 * time.Millisecond)
	bot.stop()

	bot.respawn()

	require.Eventually(t, func() bool {
		s := bot.state()
		return s.Seen.Respawns == 1 && s.Seen.Health != nil && *s.Seen.Health == 20
	}, 10*time.Second, 50*time.Millisecond, "the client never came back")
	waitOnGround(t, srv, "Mortal")
	pos, _, ok := srv.JavaPosition("Mortal")
	require.True(t, ok)
	require.InDelta(t, 0.5, pos.X(), 1, "respawned away from the spawn")
	require.Eventually(t, func() bool { _, ok := watcher.Player("Mortal"); return ok }, 10*time.Second, 50*time.Millisecond, "Bedrock lost the player")
}

func TestDigOutsideSentChunksIsRefused(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Reacher")
	waitOnGround(t, srv, "Reacher")

	far := cube.Pos{2_000_000, 0, 2_000_000}
	bot.dig(0, far, 1, 41)

	require.Eventually(t, func() bool { return slices.Contains(bot.block(far).Acks, 41) }, 5*time.Second, 50*time.Millisecond, "not acknowledged")
}

// A block a survival client breaks at once gets no second packet.
func TestInstantBreakInSurvival(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "Quick")
	waitOnGround(t, srv, "Quick")
	target := cube.Pos{1, -60, 1}
	_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
		tx.SetBlock(target, block.ShortGrass{}, nil)
		return struct{}{}, nil
	})
	require.NoError(t, err)

	bot.dig(0, target, 1, 3)

	require.Eventually(t, func() bool { return bot.block(target).Name == "air" }, 5*time.Second, 50*time.Millisecond, "the instant block stayed")
}

func TestJunkPacketDisconnectsOnlyThatPlayer(t *testing.T) {
	for _, kind := range []string{"unknown-id", "short-position", "nan-position"} {
		t.Run(kind, func(t *testing.T) {
			srv := testserver.Start(t)
			watcher, err := bedrock.Join(srv.BedrockAddr, "Bystander")
			require.NoError(t, err)
			defer watcher.Close()
			good := startBot(t, srv, "Good")
			bad := startBot(t, srv, "Bad")
			waitOnGround(t, srv, "Good")
			waitOnGround(t, srv, "Bad")

			bad.junk(kind)

			require.Eventually(t, func() bool { return !srv.JavaOnline("Bad") }, 10*time.Second, 50*time.Millisecond, "the sender of junk stayed online")
			require.Eventually(t, bad.ended, 10*time.Second, 50*time.Millisecond, "the junk sender's connection stayed open")

			// Everyone else carries on.
			require.True(t, srv.JavaOnline("Good"))
			require.False(t, good.ended())
			require.Eventually(t, func() bool { return srv.PlayerByName("Bystander") }, 15*time.Second, 50*time.Millisecond, "the Bedrock player left: %s", watcher.Ended())
			before, _, _ := srv.JavaPosition("Good")
			good.walk(0)
			require.Eventually(t, func() bool {
				p, _, ok := srv.JavaPosition("Good")
				return ok && p.X() > before.X()+1
			}, 5*time.Second, 100*time.Millisecond, "the other Java player stopped working")
		})
	}
}

func TestJavaLoginIsOnByDefault(t *testing.T) {
	require.False(t, config.Default().JavaOffline)
	srv := testserver.Start(t)

	// A session server that has never heard of anyone, standing in for Mojang.
	mojang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer mojang.Close()
	front, err := javafront.Start(javafront.Options{
		Addr:         "127.0.0.1:0",
		Login:        javalogin.Options{SessionURL: mojang.URL},
		World:        srv.World(),
		ViewDistance: 2,
	})
	require.NoError(t, err)
	defer front.Close()

	bot := startBotAt(t, front.Addr().String(), "NoAccount")
	require.Eventually(t, bot.ended, 15*time.Second, 50*time.Millisecond, "a client with no account stayed connected")
	_, online := front.Player("NoAccount")
	require.False(t, online, "a client with no account got in")
	select {
	case <-bot.played:
		t.Fatal("a client with no account reached play")
	default:
	}
	bot.clearErrors() // the refused client's own error is expected
}

// A long move is sent as an absolute position, and walking away unloads chunks.
func TestLongMovesAndUnloading(t *testing.T) {
	srv := testserver.Start(t)
	watcher := startBot(t, srv, "Eyes")
	waitOnGround(t, srv, "Eyes")
	runner := startBot(t, srv, "Runner")
	waitOnGround(t, srv, "Runner")
	require.Eventually(t, func() bool { _, ok := watcher.players()["Runner"]; return ok }, 20*time.Second, 50*time.Millisecond)

	runner.walkFast(0, 9) // 9 blocks a tick, too far for a delta of 8 blocks
	time.Sleep(200 * time.Millisecond)
	runner.stop()
	time.Sleep(500 * time.Millisecond)
	last := runner.state().Me
	seen := watcher.players()["Runner"]
	require.Greater(t, last.X, 9.0)
	require.InDelta(t, last.X, seen.X, 1, "the watcher lost track of a long move")

	runner.walkFast(0, 4)
	require.Eventually(t, func() bool { return runner.state().Seen.Packets["unload_chunk"] > 0 }, 15*time.Second, 100*time.Millisecond, "walking away never unloaded a chunk")
	// The bot drops a chunk only when told the right coordinates. Chunk (0,0) is
	// behind it; radius 6 means gone after about 7 chunks.
	require.Eventually(t, func() bool { return runner.state().Me.X > 130 && !runner.column(0, 0, -64, -64).Loaded }, 15*time.Second, 100*time.Millisecond, "the chunk at the origin was never forgotten")
	require.Eventually(t, func() bool { _, ok := watcher.players()["Runner"]; return !ok }, 15*time.Second, 100*time.Millisecond, "the runner stayed visible out of range")
}

func TestServerCloseDisconnectsJavaPlayers(t *testing.T) {
	srv := testserver.Start(t)
	bot := startBot(t, srv, "LastOut")
	waitOnGround(t, srv, "LastOut")

	closed := make(chan error, 1)
	go func() { closed <- srv.Close() }()
	select {
	case err := <-closed:
		require.NoError(t, err)
	case <-time.After(15 * time.Second):
		t.Fatal("closing the server hung with a Java player online")
	}
	require.Contains(t, bot.waitKicked(t), "closing")
	require.False(t, srv.JavaOnline("LastOut"))
}

// bulkFill is how the big edits are made. With Dragonfly's default options,
// each of the hundreds of thousands of SetBlock calls queues redstone and
// neighbour updates, and the next world tick spends seconds working through
// them (redstoneEngine.compile, performNeighbourUpdates), stalling every
// player's tick. The tests are about sending edits, so they skip those.
var bulkFill = &world.SetOpts{DisableBlockUpdates: true, DisableRedstoneUpdates: true}

func angleDiff(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 360)
	return math.Min(d, 360-d)
}
