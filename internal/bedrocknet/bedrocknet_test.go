package bedrocknet_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/bedrocknet"
	"github.com/creeperdiamonds/overmind/internal/config"
)

// A vanilla 26.60 client reaches only NetherNet, so a settings file made by default must turn
// it on, beside RakNet, with the game traffic on one UDP port.
func TestDefaultsListenOnBoth(t *testing.T) {
	cfg, err := config.Load(filepath.Join(t.TempDir(), "overmind.toml"))
	require.NoError(t, err)
	uc := server.DefaultConfig()
	require.NoError(t, bedrocknet.Configure(&uc, cfg))
	require.Equal(t, []string{"raknet", "nethernet"}, uc.Network.Transport)
	require.Equal(t, ":19132", uc.Network.Address)
	require.Equal(t, ":19132", uc.Network.NetherNet.Address)
	require.Equal(t, "19133", uc.Network.NetherNet.UDPPorts)
	require.NotEmpty(t, uc.Network.NetherNet.KeyFile)
}

// A settings file from before NetherNet has no transport keys; it gets the new defaults.
func TestOldSettingsFileGetsNetherNet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overmind.toml")
	require.NoError(t, os.WriteFile(path, []byte("BedrockAddr = ':19132'\nJavaAddr = ':25565'\n"), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Equal(t, []string{"raknet", "nethernet"}, cfg.BedrockTransports)
}

// A list in the file replaces the default one, not adds to it.
func TestTransportsFromFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "overmind.toml")
	require.NoError(t, os.WriteFile(path, []byte("BedrockTransports = ['raknet']\n"), 0o644))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	uc := server.DefaultConfig()
	require.NoError(t, bedrocknet.Configure(&uc, cfg))
	require.Equal(t, []string{"raknet"}, uc.Network.Transport)
}

func TestTransportChecks(t *testing.T) {
	cfg := config.Default()
	uc := server.DefaultConfig()

	cfg.BedrockTransports = nil
	require.NoError(t, bedrocknet.Configure(&uc, cfg))
	require.Equal(t, []string{"raknet"}, uc.Network.Transport, "an empty list is RakNet alone")

	cfg.BedrockTransports = []string{"nethernet", "quic"}
	require.ErrorContains(t, bedrocknet.Configure(&uc, cfg), `unknown Bedrock transport "quic"`)
}
