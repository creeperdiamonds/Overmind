package javafront_test

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/javalogin"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// Player metadata as the Java bot read it, by minecraft-data's indexes. The
// flag bits and poses are minecraft.wiki's.
var (
	mFlags = int(java261.MetadataIndex("player", "shared_flags"))
	mPose  = int(java261.MetadataIndex("player", "pose"))
	mHP    = int(java261.MetadataIndex("player", "health"))
	mSkin  = int(java261.MetadataIndex("player", "player_mode_customisation"))
)

const (
	fCrouch   = 0x02
	fSprint   = 0x08
	fSwim     = 0x10
	fGlide    = 0x80
	pStanding = 0
	pGliding  = 1
	pSwimming = 3
	pCrouch   = 5
	pDying    = 7
)

// seenPlayerMeta is what the Java bot was last told of a player's flags and
// pose (-1 when never).
func seenPlayerMeta(t *testing.T, j *javaBot, name string) (flags, pose int) {
	t.Helper()
	r := j.entities()
	e, ok := r.Entities[r.idOf(name)]
	if !ok {
		return -1, -1
	}
	flags, pose = -1, -1
	if e.meta(t, mFlags, &flags) {
		flags &= 0xFF // minecraft-data reads the byte as an i8
	}
	e.meta(t, mPose, &pose)
	return flags, pose
}

func javaSees(t *testing.T, j *javaBot, name string, wantFlags, wantPose int, msg string) {
	t.Helper()
	var f, p int
	require.Eventually(t, func() bool {
		f, p = seenPlayerMeta(t, j, name)
		return f == wantFlags && p == wantPose
	}, 5*time.Second, 50*time.Millisecond, "%s (flags %#x, pose %d)", msg, f, p)
}

func onServer(t *testing.T, srv *testserver.Server, name string, java bool, f func(p *player.Player)) {
	t.Helper()
	if java {
		onJava(t, srv, name, f)
	} else {
		onBedrock(t, srv, name, f)
	}
}

func state(t *testing.T, srv *testserver.Server, name string, java bool, get func(p *player.Player) bool) bool {
	t.Helper()
	var v bool
	onServer(t, srv, name, java, func(p *player.Player) { v = get(p) })
	return v
}

// Sneaking and sprinting go both ways: Java's inputs drive its player and
// Bedrock sees it, and Java sees Bedrock's.
func TestSneakAndSprintBothWays(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BMover")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JMover")
	waitOnGround(t, srv, "JMover")
	require.Eventually(t, func() bool { _, ok := b.Player("JMover"); return ok }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { return j.entities().idOf("BMover") >= 0 }, 10*time.Second, 50*time.Millisecond)
	javaSees(t, j, "BMover", 0, pStanding, "no metadata with the spawn")

	t.Run("java sneaks", func(t *testing.T) {
		j.input("shift")
		require.Eventually(t, func() bool { return state(t, srv, "JMover", true, (*player.Player).Sneaking) }, 5*time.Second, 50*time.Millisecond, "shift did not make the player sneak")
		require.Eventually(t, func() bool { return b.Flag("JMover", protocol.EntityDataFlagSneaking) }, 5*time.Second, 50*time.Millisecond, "Bedrock did not see the sneak")
		j.input()
		require.Eventually(t, func() bool { return !b.Flag("JMover", protocol.EntityDataFlagSneaking) }, 5*time.Second, 50*time.Millisecond, "Bedrock still sees the sneak")
		require.False(t, state(t, srv, "JMover", true, (*player.Player).Sneaking))
	})
	t.Run("java sprints", func(t *testing.T) {
		j.playerCommand("start_sprinting")
		require.Eventually(t, func() bool { return b.Flag("JMover", protocol.EntityDataFlagSprinting) }, 5*time.Second, 50*time.Millisecond, "Bedrock did not see the sprint")
		require.True(t, state(t, srv, "JMover", true, (*player.Player).Sprinting))
		j.playerCommand("stop_sprinting")
		require.Eventually(t, func() bool { return !b.Flag("JMover", protocol.EntityDataFlagSprinting) }, 5*time.Second, 50*time.Millisecond)
	})
	t.Run("bedrock sneaks", func(t *testing.T) {
		b.Sneak(true)
		javaSees(t, j, "BMover", fCrouch, pCrouch, "Java did not see the Bedrock player crouch")
		b.Sneak(false)
		javaSees(t, j, "BMover", 0, pStanding, "Java did not see the Bedrock player stand")
	})
	t.Run("bedrock sprints", func(t *testing.T) {
		b.Sprint(true)
		javaSees(t, j, "BMover", fSprint, pStanding, "Java did not see the Bedrock player sprint")
		b.Sprint(false)
		javaSees(t, j, "BMover", 0, pStanding, "Java still sees the sprint")
	})
}

// Swimming and gliding follow Java's rules for a Java player (the server
// works them out, as the client does not say), and both editions see them.
func TestJavaSwimsAndGlides(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BLook")
	require.NoError(t, err)
	defer b.Close()
	eyes := startBot(t, srv, "JEyes")
	j := startBot(t, srv, "JSwim")
	waitOnGround(t, srv, "JSwim")
	require.Eventually(t, func() bool { _, ok := b.Player("JSwim"); return ok }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { return eyes.entities().idOf("JSwim") >= 0 }, 10*time.Second, 50*time.Millisecond)

	t.Run("swimming", func(t *testing.T) {
		_, err := world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
			for _, y := range []int{-60, -59} {
				tx.SetBlock(cube.Pos{0, y, 0}, block.Water{Still: true, Depth: 8}, bulkFill)
			}
			return struct{}{}, nil
		})
		require.NoError(t, err)
		j.playerCommand("start_sprinting")
		require.Eventually(t, func() bool { return state(t, srv, "JSwim", true, (*player.Player).Swimming) }, 5*time.Second, 50*time.Millisecond, "sprinting under water did not swim")
		require.Eventually(t, func() bool { return b.Flag("JSwim", protocol.EntityDataFlagSwimming) }, 5*time.Second, 50*time.Millisecond, "Bedrock did not see the swimming")
		javaSees(t, eyes, "JSwim", fSprint|fSwim, pSwimming, "Java did not see the swimming")
		j.playerCommand("stop_sprinting")
		require.Eventually(t, func() bool { return !state(t, srv, "JSwim", true, (*player.Player).Swimming) }, 5*time.Second, 50*time.Millisecond, "stopping the sprint did not stop the swim")
		javaSees(t, eyes, "JSwim", 0, pStanding, "Java still sees the swimming")
		_, err = world.Call(context.Background(), srv.World(), func(tx *world.Tx) (struct{}, error) {
			for _, y := range []int{-60, -59} {
				tx.SetBlock(cube.Pos{0, y, 0}, nil, bulkFill)
			}
			return struct{}{}, nil
		})
		require.NoError(t, err)
	})
	t.Run("gliding", func(t *testing.T) {
		onJava(t, srv, "JSwim", func(p *player.Player) { p.Armour().SetChestplate(item.NewStack(item.Elytra{}, 1)) })
		j.climb(groundY + 16)
		require.Eventually(t, func() bool {
			pos, onGround, _ := srv.JavaPosition("JSwim")
			return !onGround && pos.Y() > groundY+12
		}, 5*time.Second, 20*time.Millisecond)
		j.playerCommand("start_fall_flying")
		require.Eventually(t, func() bool { return b.Flag("JSwim", protocol.EntityDataFlagGliding) }, 3*time.Second, 20*time.Millisecond, "Bedrock did not see the glide")
		javaSees(t, eyes, "JSwim", fGlide, pGliding, "Java did not see the glide")
		require.Eventually(t, func() bool { return !state(t, srv, "JSwim", true, (*player.Player).Gliding) }, 10*time.Second, 50*time.Millisecond, "landing did not end the glide")
		javaSees(t, eyes, "JSwim", 0, pStanding, "Java still sees the glide")
	})
}

// Health and skin layers: a Java player's own layers reach other Java players
// and itself; a Bedrock player shows them all; health follows the player.
func TestHealthAndSkinLayers(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BSkin")
	require.NoError(t, err)
	defer b.Close()
	eyes := startBot(t, srv, "JEyes")
	j := startBot(t, srv, "JSkin")
	waitOnGround(t, srv, "JSkin")
	require.Eventually(t, func() bool {
		r := eyes.entities()
		return r.idOf("JSkin") >= 0 && r.idOf("BSkin") >= 0
	}, 10*time.Second, 50*time.Millisecond)
	skin := func(name string) int {
		r := eyes.entities()
		v := -1
		r.Entities[r.idOf(name)].meta(t, mSkin, &v)
		return v
	}
	require.Equal(t, javalogin.AllSkinParts, skin("BSkin"), "a Bedrock player shows every layer")

	j.settings(0x15)
	require.Eventually(t, func() bool { return skin("JSkin") == 0x15 }, 5*time.Second, 50*time.Millisecond, "the other Java player did not see the layers")
	require.Eventually(t, func() bool {
		v := -1
		raw, ok := j.entities().Self.Metadata[itoa(mSkin)]
		return ok && jsonInt(raw, &v) && v == 0x15
	}, 5*time.Second, 50*time.Millisecond, "the client was not told its own layers")

	health := func() float64 {
		r := eyes.entities()
		v := -1.0
		r.Entities[r.idOf("BSkin")].meta(t, mHP, &v)
		return v
	}
	server := func() float64 {
		var hp float64
		onBedrock(t, srv, "BSkin", func(p *player.Player) { hp = p.Health() })
		return hp
	}
	require.Equal(t, 20.0, health())
	// Natural regeneration may add to it at any time, so what Java sees is
	// compared with the server.
	onBedrock(t, srv, "BSkin", func(p *player.Player) { p.Hurt(6, entity.VoidDamageSource{}) })
	require.Eventually(t, func() bool { hp := server(); return hp < 20 && health() == hp }, 5*time.Second, 50*time.Millisecond, "Java did not see the health drop")
	before := server()
	onBedrock(t, srv, "BSkin", func(p *player.Player) { p.Heal(2, entity.FoodHealingSource{}) })
	require.Eventually(t, func() bool { hp := server(); return hp >= before+2 && health() == hp }, 5*time.Second, 50*time.Millisecond, "Java did not see the healing")
}

func itoa(i int) string { return strconv.Itoa(i) }

func jsonInt(raw json.RawMessage, v *int) bool { return json.Unmarshal(raw, v) == nil }
