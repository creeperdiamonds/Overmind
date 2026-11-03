// Package app builds and starts the Overmind server.
package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"

	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/worldgen"
)

// Start creates the server from cfg, starts listening and keeps accepting
// players until the returned server is closed.
func Start(cfg config.Config) (*server.Server, error) {
	log := slog.Default()
	if !cfg.AuthEnabled {
		log.Warn("Xbox login is OFF. Anyone can join under any name. Use this only for test bots.")
	}

	// level.dat is written when the world first saves. Until then the spawn is ours to choose.
	_, statErr := os.Stat(filepath.Join(cfg.WorldFolder, "level.dat"))
	newWorld := errors.Is(statErr, os.ErrNotExist)
	if seedFileMissing(cfg.WorldFolder) {
		log.Warn("The world folder has a saved world but no seed file. The seed in the settings, or a random one, is used and kept from now on; if it is not the seed the world was made with, saved chunks will not fit the terrain made around them.", "folder", cfg.WorldFolder, "file", seedFile)
	}
	seed, err := worldSeed(cfg.WorldFolder, cfg.Seed)
	if err != nil {
		return nil, err
	}
	log.Info("World seed", "seed", seed)
	profile := worldgen.ProfileBedrock
	if cfg.Profile != "" {
		if profile = worldgen.ProfileByName(cfg.Profile); profile == nil {
			return nil, fmt.Errorf("unknown world profile %q: want bedrock or overjava", cfg.Profile)
		}
	}

	uc := server.DefaultConfig()
	uc.Network.Address = cfg.BedrockAddr
	uc.Server.Name = "Overmind"
	uc.Server.AuthEnabled = cfg.AuthEnabled
	uc.World.Folder = cfg.WorldFolder
	uc.Players.MaxCount = cfg.MaxPlayers
	uc.Players.MaximumChunkRadius = cfg.ViewDistance
	uc.Players.SaveData = false
	uc.Resources.AutoBuildPack = false
	uc.Resources.Folder = cfg.ResourcesFolder

	// worldgen's own block types are registered in its init, which has run because this package
	// imports it. uc.Config opens the world database, and that finalizes the block registry, so
	// nothing that adds block types may run after this line.
	conf, err := uc.Config(log)
	if err != nil {
		return nil, fmt.Errorf("build server config: %w", err)
	}
	gen := worldgen.NewWithProfile(seed, profile)
	guard := &offTick{g: gen}
	conf.Generator = func(dim world.Dimension) world.Generator {
		if dim == world.Overworld {
			return guard
		}
		return unusedGenerator(dim)
	}
	conf.RandomTickSpeed = cfg.RandomTickSpeed
	conf.ChunkLoadWorkers = chunkWorkers(cfg.ChunkWorkers)

	// The settings are read when the worlds open. Dragonfly asks the generator for a spawn only when it
	// has no world database at all, which is never the case here, so a new world gets ours before it
	// opens: left unset, the first tick would look for the top block at 0, 0 and generate that chunk on
	// the tick. The search is deterministic, so a world that stopped before its first save finds the
	// same spawn again.
	set := conf.WorldProvider.Settings()
	if newWorld {
		set.Spawn = gen.DefaultSpawn(world.Overworld)
	}
	set.TickRange = int32(tickDistance(cfg.TickDistance, cfg.ViewDistance))

	srv := conf.New()
	guard.watch(srv.World())
	if err := preloadSpawn(srv.World(), int(set.TickRange)+2); err != nil {
		log.Warn("Spawn chunks did not load in time", "error", err)
	}
	srv.Listen()
	go func() {
		for range srv.Accept() {
		}
	}()
	return srv, nil
}

// chunkWorkers is how many chunk workers to run: the setting, or min(4, processors). Each busy worker
// holds its own generation scratch on top of the shared cache, so more workers cost memory (ruling 32).
func chunkWorkers(set int) int {
	if set > 0 {
		return set
	}
	return min(4, runtime.NumCPU())
}

// tickDistance is the tick range: the setting or Bedrock's default 4, held at least two under the view
// distance. Dragonfly ticks every loaded chunk within it, and a tick that reads past a loaded chunk
// makes the tick load that chunk itself; two chunks of margin keep a ticked block's neighbours loaded.
func tickDistance(set, view int) int {
	if set <= 0 {
		set = 4
	}
	return max(1, min(set, view-2))
}

// unusedGenerator makes the Nether and End a flat floor. Nothing sends players there yet.
func unusedGenerator(dim world.Dimension) world.Generator {
	if dim == world.Nether {
		return generator.NewFlat(biome.NetherWastes{}, []world.Block{block.Netherrack{}, block.Bedrock{}})
	}
	return generator.NewFlat(biome.End{}, []world.Block{block.EndStone{}, block.Bedrock{}})
}
