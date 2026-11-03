package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/go-nethernet/endpoint"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/testserver"
)

// A Bedrock 26.60 client joins over NetherNet only: an HTTP handshake on the TCP port, then
// WebRTC. The default settings must take it, beside RakNet on the same port.
func TestBedrockJoinsOverNetherNet(t *testing.T) {
	srv := testserver.Start(t)
	bot, err := bedrock.JoinNetherNet(srv.BedrockAddr, "NetherBot")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("NetherBot") }, 15*time.Second, 100*time.Millisecond)

	// RakNet still works alongside, on UDP at the same address.
	old, err := bedrock.Join(srv.BedrockAddr, "RakBot")
	require.NoError(t, err)
	defer old.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("RakBot") }, 15*time.Second, 100*time.Millisecond)

	// The player plays, not just logs in: walking moves it on the server.
	var start, now [3]float64
	require.Eventually(t, func() bool {
		pos, ok := srv.PlayerPosition("NetherBot")
		start = [3]float64{pos.X(), pos.Y(), pos.Z()}
		return ok
	}, 5*time.Second, 100*time.Millisecond)
	bot.Walk(0)
	require.Eventually(t, func() bool {
		pos, ok := srv.PlayerPosition("NetherBot")
		now = [3]float64{pos.X(), pos.Y(), pos.Z()}
		return ok && now[0]-start[0] > 5
	}, 20*time.Second, 200*time.Millisecond, "the NetherNet player did not walk")
}

// The server list asks GET /v1/join; the answer carries the name and the player counts.
func TestNetherNetStatus(t *testing.T) {
	srv := testserver.Start(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var status endpoint.Status
	require.Eventually(t, func() bool {
		var err error
		status, err = endpoint.NewClient().Status(ctx, "http://"+srv.BedrockAddr)
		return err == nil && status.ServerName != ""
	}, 10*time.Second, 200*time.Millisecond, "no status from /v1/join")
	require.Equal(t, "Overmind", status.ServerName)
	require.Equal(t, srv.Config.MaxPlayers, status.MaxPlayerCount)
	require.NotZero(t, status.Protocol)
}

// The default shares one UDP port among all NetherNet players. The server keeps its key, so a
// player who confirmed it once is not asked again after a restart.
func TestNetherNetSharedPortAndKept(t *testing.T) {
	dir := t.TempDir()
	port := testserver.SparePort(t)
	srv := testserver.StartIn(t, dir, func(c *config.Config) { c.NetherNetUDPPorts = port })
	bot, err := bedrock.JoinNetherNet(srv.BedrockAddr, "Shared")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool { return srv.PlayerByName("Shared") }, 15*time.Second, 100*time.Millisecond)

	key := srv.Config.NetherNetKeyFile
	require.Equal(t, filepath.Join(dir, "keys", "nethernet-identity.pem"), key)
	first, err := os.ReadFile(key)
	require.NoError(t, err, "the identity key was not saved")
	bot.Close()
	require.NoError(t, srv.Close())

	testserver.StartIn(t, dir, func(c *config.Config) { c.NetherNetUDPPorts = port })
	again, err := os.ReadFile(key)
	require.NoError(t, err)
	require.Equal(t, first, again, "a restart made a new identity key")
}
