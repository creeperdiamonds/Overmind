// Package app builds and starts the Overmind server.
package app

import (
	"fmt"
	"log/slog"

	"github.com/df-mc/dragonfly/server"

	"github.com/overmind/overmind/internal/config"
)

// Start creates the server from cfg, starts listening and keeps accepting
// players until the returned server is closed.
func Start(cfg config.Config) (*server.Server, error) {
	log := slog.Default()
	if !cfg.AuthEnabled {
		log.Warn("Xbox login is OFF. Anyone can join under any name. Use this only for test bots.")
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

	conf, err := uc.Config(log)
	if err != nil {
		return nil, fmt.Errorf("build server config: %w", err)
	}
	srv := conf.New() // flat world generator by default
	srv.Listen()
	go func() {
		for range srv.Accept() {
		}
	}()
	return srv, nil
}
