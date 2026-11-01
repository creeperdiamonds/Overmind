package app_test

import (
	"path/filepath"
	"testing"
	"time"

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

func TestAuthIsOnByDefault(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "missing.toml"))
	require.NoError(t, err)
	require.True(t, cfg.AuthEnabled)
}
