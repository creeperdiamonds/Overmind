// Package testserver starts a throwaway Overmind server for tests.
package testserver

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server"

	"github.com/overmind/overmind/internal/app"
	"github.com/overmind/overmind/internal/config"
)

// Server is a running test server.
type Server struct {
	*server.Server
	// BedrockAddr is the address Bedrock bots dial.
	BedrockAddr string
	// JavaAddr is a reserved spare TCP port for the Java side.
	JavaAddr string
}

// Start runs a server with Xbox login off, spare ports and a temporary
// world folder. It is closed when the test ends.
func Start(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.AuthEnabled = false
	cfg.BedrockAddr = spareUDP(t)
	cfg.JavaAddr = spareTCP(t)
	cfg.WorldFolder = filepath.Join(dir, "world")
	cfg.ResourcesFolder = filepath.Join(dir, "resources")

	srv, err := app.Start(cfg)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return &Server{Server: srv, BedrockAddr: cfg.BedrockAddr, JavaAddr: cfg.JavaAddr}
}

// PlayerByName reports whether a player with that name is online.
func (s *Server) PlayerByName(name string) (found bool) {
	_, found = s.Server.PlayerByName(name)
	return found
}

func spareUDP(t *testing.T) string {
	t.Helper()
	c, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	return c.LocalAddr().String()
}

func spareTCP(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().String()
}
