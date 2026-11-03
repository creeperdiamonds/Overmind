// Command overmind runs the server.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/creeperdiamonds/overmind/internal/app"
	"github.com/creeperdiamonds/overmind/internal/config"
)

func main() {
	path := flag.String("config", "overmind.toml", "path to the settings file")
	flag.Parse()

	cfg, err := config.Load(*path)
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}
	servePprof()
	srv, err := app.Start(cfg)
	if err != nil {
		slog.Error("start server", "err", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()
	_ = srv.Close()
}
