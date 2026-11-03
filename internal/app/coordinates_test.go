package app_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/config"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// Bedrock's "Show coordinates" follows the showcoordinates game rule, which
// vanilla BDS leaves off. With ShowCoordinates (the default) a Bedrock player
// is sent it set to true as they join; with it off, it is not sent.
func TestBedrockShowCoordinates(t *testing.T) {
	require.True(t, config.Default().ShowCoordinates, "ShowCoordinates is on by default")

	srv := testserver.Start(t)
	bot, err := bedrock.Join(srv.BedrockAddr, "Coords")
	require.NoError(t, err)
	defer bot.Close()
	require.Eventually(t, func() bool {
		v, ok := bot.GameRule("showcoordinates")
		return ok && v == true
	}, 10*time.Second, 50*time.Millisecond, "the showcoordinates game rule was never sent as true")

	off := testserver.StartWith(t, func(c *config.Config) { c.ShowCoordinates = false })
	bot2, err := bedrock.Join(off.BedrockAddr, "NoCoords")
	require.NoError(t, err)
	defer bot2.Close()
	require.Eventually(t, func() bool { return off.PlayerByName("NoCoords") }, 10*time.Second, 50*time.Millisecond)
	time.Sleep(time.Second)
	_, sent := bot2.GameRule("showcoordinates")
	require.False(t, sent, "showcoordinates was sent with ShowCoordinates off")
}
