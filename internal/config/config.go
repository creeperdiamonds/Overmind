// Package config loads Overmind's settings from a TOML file.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/overmind/overmind/internal/skins"
)

// Config holds every setting Overmind reads at startup.
type Config struct {
	// BedrockAddr is the address Bedrock players connect to: UDP for RakNet,
	// TCP for NetherNet's signalling, both on the same port.
	BedrockAddr string
	// BedrockTransports is how Bedrock players reach the server: "raknet"
	// (UDP, older clients and the test bots) and "nethernet" (Bedrock 26.50
	// and later, which join over WebRTC after an HTTP handshake on TCP).
	// Both by default. internal/bedrocknet/NOTES.md explains NetherNet.
	BedrockTransports []string
	// NetherNetUDPPorts is the UDP port, or "min-max" range, carrying the game
	// traffic of NetherNet players. A single port is shared by every player.
	// Empty lets the system pick a port per player. Open it in the firewall
	// alongside BedrockAddr's TCP port.
	NetherNetUDPPorts string
	// NetherNetKeyFile holds the key that identifies the server to NetherNet
	// players, made on the first start. Keep it: a player joining by address
	// confirms the server's key once, and a new key asks them again.
	NetherNetKeyFile string
	// JavaAddr is the TCP address Java players connect to.
	JavaAddr string
	// JavaOffline skips the Mojang account check, so anyone can join on Java
	// under any name. Login stays on unless this is set; it is for test bots.
	JavaOffline bool
	// AuthEnabled requires an Xbox login. Turn it off only for test bots.
	AuthEnabled bool
	// GameMode is what new players get: survival, creative, adventure or
	// spectator.
	GameMode string
	// ViewDistance is the largest view distance in chunks.
	ViewDistance int
	// WorldFolder is where the world is saved.
	WorldFolder string
	// PlayersFolder is where player data is saved: one database per edition,
	// bedrock and java, each keyed by account ID.
	PlayersFolder string
	// SaveSeconds is how often online players are saved, besides when they leave
	// and when the server stops. A crash loses at most this much play. 0 turns
	// the periodic save off.
	SaveSeconds int
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
	// Skins is how skins cross between the editions ([skins] in the file).
	Skins Skins `toml:"skins"`
	// FlatWorld, set by tests (never read from the file), makes the Overworld
	// Dragonfly's flat world instead of worldgen's. The front ends' tests build
	// on it, at fixed heights.
	FlatWorld bool `toml:"-"`
}

// Skins is the [skins] table.
type Skins struct {
	// BlockOddShapes replaces, for every viewer of both editions, a Bedrock skin
	// whose shape goes beyond the standard player model (bigger, much smaller,
	// hidden or floating parts) with a default skin. Off by default.
	BlockOddShapes bool `toml:"blockOddShapes"`
	// UploadAccount is the account file (made by "overmind skin-account") of a
	// Java account whose skin is set to Bedrock players' skins so that Mojang
	// signs them for Java viewers. Empty, the default, uploads nothing. See
	// internal/javafront/NOTES.md before using an account: it can be suspended.
	UploadAccount string `toml:"uploadAccount"`
	// UploadCache keeps what Mojang signed, a file per skin, for good.
	UploadCache string `toml:"uploadCache"`
	// UploadGapSeconds is the least time between two uploads.
	UploadGapSeconds int `toml:"uploadGapSeconds"`
	// Uploader, set by tests (never read from the file), is used in place of an
	// upload account.
	Uploader *skins.Uploader `toml:"-"`
}

// Default returns the settings used when a file leaves something out.
func Default() Config {
	return Config{
		BedrockAddr:       ":19132",
		BedrockTransports: []string{"raknet", "nethernet"},
		NetherNetUDPPorts: "19133",
		NetherNetKeyFile:  "keys/nethernet-identity.pem",
		JavaAddr:          ":25565",
		AuthEnabled:       true,
		GameMode:          "survival",
		ViewDistance:      6,
		WorldFolder:       "world",
		PlayersFolder:     "players",
		SaveSeconds:       5,
		MaxPlayers:        50,
		ResourcesFolder:   "resources",
		Profile:           "bedrock",
		Skins:             Skins{UploadCache: "skins", UploadGapSeconds: 30},
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
