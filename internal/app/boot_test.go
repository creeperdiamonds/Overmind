package app_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/testserver"
)

func TestBedrockBotJoins(t *testing.T) {
	srv := testserver.Start(t) // AuthEnabled=false, spare ports, temp world folder
	bot, err := bedrock.Join(srv.BedrockAddr, "TestBot")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool {
		return srv.PlayerByName("TestBot")
	}, 15*time.Second, 100*time.Millisecond)
}

func TestBedrockBotWalks(t *testing.T) {
	srv := testserver.Start(t)
	bot, err := bedrock.Join(srv.BedrockAddr, "Walker")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("Walker") }, 15*time.Second, 100*time.Millisecond)

	var start mgl64.Vec3
	require.Eventually(t, func() bool {
		var ok bool
		start, ok = srv.PlayerPosition("Walker")
		return ok
	}, 5*time.Second, 100*time.Millisecond)
	chunksBefore := bot.ChunksReceived()

	bot.Walk(0) // +X
	require.Eventually(t, func() bool {
		pos, ok := srv.PlayerPosition("Walker")
		return ok && pos.X()-start.X() > 20
	}, 20*time.Second, 200*time.Millisecond, "server-side player did not walk 20 blocks")
	pos, _ := srv.PlayerPosition("Walker")
	require.InDelta(t, start.Z(), pos.Z(), 1.0, "walking along +X should not drift in Z")
	require.Greater(t, bot.ChunksReceived(), chunksBefore, "no new chunks arrived while walking")
}

func TestAuthIsOnByDefault(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	require.NoError(t, err)
	require.True(t, cfg.AuthEnabled)
}
