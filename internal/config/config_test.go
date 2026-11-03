package config_test

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/internal/config"
)

func TestSeedIsOptional(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overmind.toml")
	cfg, err := config.Load(path) // writes the defaults
	require.NoError(t, err)
	require.Nil(t, cfg.Seed, "a new settings file must leave the seed to be chosen")
	again, err := config.Load(path)
	require.NoError(t, err)
	require.Nil(t, again.Seed)
}

func TestSeedReadsAnyInt64(t *testing.T) {
	for _, seed := range []string{"0", "-1", "2147483648", "-9223372036854775808", "9223372036854775807"} {
		path := filepath.Join(t.TempDir(), "overmind.toml")
		require.NoError(t, os.WriteFile(path, []byte("Seed = "+seed+"\n"), 0o644))
		cfg, err := config.Load(path)
		require.NoError(t, err, seed)
		require.NotNil(t, cfg.Seed, seed)
	}
	path := filepath.Join(t.TempDir(), "overmind.toml")
	require.NoError(t, os.WriteFile(path, []byte("Seed = -9223372036854775808\n"), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, int64(math.MinInt64), *cfg.Seed)
}
