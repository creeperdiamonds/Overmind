package app_test

import (
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/config"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// The random tick speed follows the world's profile unless the settings give one: Bedrock's
// randomTickSpeed defaults to 1, Java's random_tick_speed to 3 (minecraft.wiki, "Tick", oldid
// 3868751). Every dimension uses it.
func TestRandomTickSpeedFollowsProfile(t *testing.T) {
	for _, c := range []struct {
		profile string
		set     int
		want    int
	}{
		{"bedrock", 0, 1},
		{"overjava", 0, 3},
		{"bedrock", 50, 50},
	} {
		srv := testserver.StartIn(t, t.TempDir(), func(cfg *config.Config) {
			cfg.Profile, cfg.RandomTickSpeed = c.profile, c.set
		})
		for _, w := range []*world.World{srv.World(), srv.Nether(), srv.End()} {
			require.Equal(t, c.want, w.RandomTickSpeed(), "profile %s, setting %d, %v", c.profile, c.set, w.Dimension())
		}
		require.NoError(t, srv.Close())
	}
}
