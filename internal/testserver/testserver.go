// Package testserver starts a throwaway Overmind server for tests.
package testserver

import (
	"fmt"
	"net"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"github.com/overmind/overmind/internal/app"
	"github.com/overmind/overmind/internal/config"
)

// Server is a running test server.
type Server struct {
	*app.Server
	// BedrockAddr is the address Bedrock bots dial.
	BedrockAddr string
	// JavaAddr is the address Java bots dial.
	JavaAddr string
	// Config is what the server was started with.
	Config config.Config
	// PlayersFolder holds the saved players, a folder per edition.
	PlayersFolder string
}

// Start runs a server with Xbox and Mojang login off, spare ports and a temporary
// world folder. It is closed when the test ends.
func Start(t *testing.T) *Server {
	t.Helper()
	return StartIn(t, t.TempDir(), nil)
}

// StartWith is Start with changes to the settings.
func StartWith(t *testing.T, change func(*config.Config)) *Server {
	t.Helper()
	return StartIn(t, t.TempDir(), change)
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
	cfg.JavaOffline = true
	cfg.BedrockAddr = spareUDPAndTCP(t) // RakNet on UDP, NetherNet's signalling on TCP
	// The system picks each NetherNet player's UDP port, so test servers never share one.
	cfg.NetherNetUDPPorts = ""
	cfg.NetherNetKeyFile = filepath.Join(dir, "keys", "nethernet-identity.pem")
	cfg.JavaAddr = spareTCP(t)
	cfg.WorldFolder = filepath.Join(dir, "world")
	cfg.PlayersFolder = filepath.Join(dir, "players")
	cfg.ResourcesFolder = filepath.Join(dir, "resources")

	if tweak != nil {
		tweak(&cfg)
	}
	if err := claimServer(); err != nil {
		return nil, err
	}
	srv, err := app.Start(cfg)
	if err != nil {
		made.Add(-1) // A start refused before the server was made (a bad setting) adds nothing global.
		return nil, err
	}
	t.Cleanup(func() { _ = srv.Close() })
	return &Server{Server: srv, BedrockAddr: cfg.BedrockAddr, JavaAddr: cfg.JavaAddr, Config: cfg, PlayersFolder: cfg.PlayersFolder}, nil
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

// JavaOnline reports whether a Java player with that name is online.
func (s *Server) JavaOnline(name string) bool {
	_, ok := s.Java.Player(name)
	return ok
}

// JavaPosition returns where the server thinks the named Java player is, and
// whether it is on the ground.
func (s *Server) JavaPosition(name string) (pos mgl64.Vec3, onGround, ok bool) {
	h, found := s.Java.Player(name)
	if !found {
		return pos, false, false
	}
	type result struct {
		pos      mgl64.Vec3
		onGround bool
	}
	done := make(chan result, 1)
	player.Do(h, func(_ *world.Tx, p *player.Player) {
		done <- result{p.Position(), p.OnGround()}
	})
	select {
	case r := <-done:
		return r.pos, r.onGround, true
	case <-time.After(2 * time.Second):
		return pos, false, false
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

// spareUDPAndTCP returns a local address whose port is free on both UDP and TCP.
func spareUDPAndTCP(t *testing.T) string {
	t.Helper()
	for range 50 {
		addr := spareUDP(t)
		if l, err := net.Listen("tcp", addr); err == nil {
			_ = l.Close()
			return addr
		}
	}
	t.Fatal("no port free on both UDP and TCP")
	return ""
}

// SparePort returns a free UDP port, for a test that sets a port of its own.
func SparePort(t *testing.T) string {
	t.Helper()
	_, port, _ := net.SplitHostPort(spareUDP(t))
	return port
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

// serverCap is how many servers one test process may make. Upstream Dragonfly added its creative
// items and recipes to global lists on every server it made, and after about 20 a joining bot's
// session was dropped after a second (docs/stage-2-known-gaps.md), so the cap was 19. The fork now
// registers them once per process (third_party/dragonfly/NOTES.md, vanillaOnce) and internal/javafront
// makes about 90, so the cap is only a guard against a runaway test.
var serverCap int64 = 500

// made counts the servers this process has made.
var made atomic.Int64

// Made is how many servers this test process has made so far.
func Made() int64 { return made.Load() }

// claimServer counts a server about to be made, or refuses one past the cap.
func claimServer() error {
	if n := made.Add(1); n > serverCap {
		made.Add(-1)
		return fmt.Errorf("test server %d of this process is past the cap of %d: Dragonfly adds its creative items and recipes to global lists on every server, and past about 20 a joining bot is dropped; reuse a server or move tests to another package", n, serverCap)
	}
	return nil
}
