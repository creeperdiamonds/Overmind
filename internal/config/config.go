// Package config loads Overmind's settings from a TOML file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// Config holds every setting Overmind reads at startup.
type Config struct {
	// BedrockAddr is the UDP address Bedrock players connect to.
	BedrockAddr string
	// JavaAddr is the TCP address Java players connect to.
	JavaAddr string
	// AuthEnabled requires an Xbox login. Turn it off only for test bots.
	AuthEnabled bool
	// ViewDistance is the largest view distance in chunks.
	ViewDistance int
	// WorldFolder is where the world is saved.
	WorldFolder string
	// MaxPlayers is how many players may be online at once.
	MaxPlayers int
	// ResourcesFolder is where resource packs are read from.
	ResourcesFolder string
	// Seed is the world seed. Leave it out and a random one is chosen when the
	// world is first made, then kept in the world folder. Any int64 is allowed.
	Seed *int64
	// RandomTickSpeed is how many blocks per sub chunk get a random tick each
	// tick. 0 is Dragonfly's normal speed, 3.
	RandomTickSpeed int
	// ChunkWorkers is how many background workers generate and load chunks.
	// 0 picks min(4, processors): each busy worker holds its own scratch.
	ChunkWorkers int
	// TickDistance is how many chunks around a player have their blocks ticked,
	// Bedrock's tick-distance. It is held two under ViewDistance, so a ticked
	// block's neighbours are always loaded. 0 means Bedrock's default, 4.
	TickDistance int
	// Profile is the world's placement rules: "bedrock" (the default), Java's
	// algorithms with Bedrock's data, or "overjava", Java's throughout. A world
	// keeps the profile it was made with only if this is left unchanged.
	Profile string
}

// Default returns the settings used when a file leaves something out.
func Default() Config {
	return Config{
		BedrockAddr:     ":19132",
		JavaAddr:        ":25565",
		AuthEnabled:     true,
		ViewDistance:    6,
		WorldFolder:     "world",
		MaxPlayers:      50,
		ResourcesFolder: "resources",
		Profile:         "bedrock",
	}
}

// Load reads the file at path. If it does not exist, the defaults are
// written there and returned.
func Load(path string) (Config, error) {
	cfg := Default()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		out, err := toml.Marshal(cfg)
		if err != nil {
			return cfg, err
		}
		if dir := filepath.Dir(path); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return cfg, err
			}
		}
		return cfg, os.WriteFile(path, out, 0o644)
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("read %s: %w", path, err)
	}
	return cfg, nil
}
