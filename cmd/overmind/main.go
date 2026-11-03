// Command overmind runs the server.
//
//	overmind [-config overmind.toml]        run the server
//	overmind skin-account <account file>    sign a Java account in for skin uploads
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/overmind/overmind/internal/app"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/skins"
)

func main() {
	path := flag.String("config", "overmind.toml", "path to the settings file")
	flag.Parse()

	if flag.Arg(0) == "skin-account" {
		if flag.NArg() != 2 {
			fmt.Fprintln(os.Stderr, "usage: overmind skin-account <account file>")
			os.Exit(2)
		}
		// The device code flow: it prints a URL and a code, never a token.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		defer stop()
		if err := skins.DeviceLogin(ctx, flag.Arg(1), os.Stdout, skins.Endpoints{}, nil); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

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
