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
	seed, err := worldSeed(cfg.WorldFolder, cfg.Seed)
	if err != nil {
		return nil, err
	}
	log.Info("World seed", "seed", seed)

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
	gen := worldgen.New(seed)
	conf.Generator = func(dim world.Dimension) world.Generator {
		if dim == world.Overworld {
			return gen
		}
		return unusedGenerator(dim)
	}
	conf.RandomTickSpeed = cfg.RandomTickSpeed
	conf.ChunkLoadWorkers = chunkWorkers(cfg.ChunkWorkers)

	srv := conf.New()
	// Dragonfly asks the generator for a spawn only when it has no world database at all, which
	// is never the case here, so a new world gets ours. The search is deterministic, so a world
	// that stopped before its first save finds the same spawn again.
	if newWorld {
		srv.World().SetSpawn(gen.DefaultSpawn(world.Overworld))
	}
	srv.Listen()
	go func() {
		for range srv.Accept() {
		}
	}()
	return srv, nil
}

// chunkWorkers is how many chunk workers to run: the setting, or half the processors.
func chunkWorkers(set int) int {
	if set > 0 {
		return set
	}
	return max(1, runtime.NumCPU()/2)
}

// unusedGenerator makes the Nether and End a flat floor. Nothing sends players there yet.
func unusedGenerator(dim world.Dimension) world.Generator {
	if dim == world.Nether {
		return generator.NewFlat(biome.NetherWastes{}, []world.Block{block.Netherrack{}, block.Bedrock{}})
	}
	return generator.NewFlat(biome.End{}, []world.Block{block.EndStone{}, block.Bedrock{}})
}
