// Package testserver starts a throwaway Overmind server for tests.
package testserver

import (
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

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
	// Config is what the server was started with.
	Config config.Config
}

// Start runs a server with Xbox login off, spare ports and a temporary
// world folder. It is closed when the test ends.
func Start(t *testing.T) *Server {
	t.Helper()
	return StartIn(t, t.TempDir(), nil)
}

// StartIn is Start with the world kept under dir, so a second call with the
// same dir is a restart. tweak, if not nil, may change the settings first.
func StartIn(t *testing.T, dir string, tweak func(*config.Config)) *Server {
	t.Helper()
	srv, err := TryStartIn(t, dir, tweak)
	if err != nil {
		t.Fatalf("start server: %v", err)
	}
	return srv
}

// TryStartIn is StartIn that returns the start error instead of failing the test.
func TryStartIn(t *testing.T, dir string, tweak func(*config.Config)) (*Server, error) {
	t.Helper()
	cfg := config.Default()
	cfg.AuthEnabled = false
	cfg.BedrockAddr = spareUDP(t)
	cfg.JavaAddr = spareTCP(t)
	cfg.WorldFolder = filepath.Join(dir, "world")
	cfg.ResourcesFolder = filepath.Join(dir, "resources")

	if tweak != nil {
		tweak(&cfg)
	}
	srv, err := app.Start(cfg)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = srv.Close() })
	return &Server{Server: srv, BedrockAddr: cfg.BedrockAddr, JavaAddr: cfg.JavaAddr, Config: cfg}, nil
}

// PlayerByName reports whether a player with that name is online.
func (s *Server) PlayerByName(name string) (found bool) {
	_, found = s.Server.PlayerByName(name)
	return found
}

// PlayerPosition returns where the server thinks the named player is.
func (s *Server) PlayerPosition(name string) (pos mgl64.Vec3, ok bool) {
	h, found := s.Server.PlayerByName(name)
	if !found {
		return pos, false
	}
	done := make(chan struct{})
	h.Do(func(tx *world.Tx, e world.Entity) {
		pos = e.Position()
		close(done)
	})
	select {
	case <-done:
		return pos, true
	case <-time.After(2 * time.Second):
		return pos, false
	}
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
