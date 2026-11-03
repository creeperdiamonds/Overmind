// Package app builds and starts the Overmind server.
package app

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"

	"github.com/overmind/overmind/internal/bedrocknet"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/javafront"
	"github.com/overmind/overmind/internal/javalogin"
	"github.com/overmind/overmind/internal/playerstore"
	"github.com/overmind/overmind/internal/skins"
	"github.com/overmind/overmind/internal/worldgen"
)

// Server is the running game and its Java front end.
type Server struct {
	*server.Server
	Java *javafront.Front

	hub         *hub
	javaPlayers io.Closer
	stopSaving  func()
	uploader    *skins.Uploader // nil without an upload account
	closeOnce   sync.Once
}

// Close disconnects Java players, saving them, then closes the game. Java
// players' saves are written before their store closes; Dragonfly does the same
// for Bedrock's.
func (s *Server) Close() error {
	var err error
	s.closeOnce.Do(func() {
		s.stopSaving()
		s.Java.Close()
		if s.uploader != nil {
			s.uploader.Close()
		}
		if cerr := s.javaPlayers.Close(); cerr != nil {
			slog.Default().Error("close java player data", "err", cerr)
		}
		err = s.Server.Close()
	})
	return err
}

// Online is how many players are online, on both editions.
func (s *Server) Online() int { return s.hub.count() }

// Start creates the server from cfg, starts listening and keeps accepting
// players until the returned server is closed.
func Start(cfg config.Config) (*Server, error) {
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
	if err := bedrocknet.Configure(&uc, cfg); err != nil {
		return nil, err
	}
	uc.Server.Name = "Overmind"
	uc.Server.AuthEnabled = cfg.AuthEnabled
	uc.World.Folder = cfg.WorldFolder
	uc.Players.MaxCount = cfg.MaxPlayers
	uc.Players.MaximumChunkRadius = cfg.ViewDistance
	// Each edition keeps its own players, by account ID.
	if err := os.MkdirAll(cfg.PlayersFolder, 0o755); err != nil {
		return nil, fmt.Errorf("create player data folder: %w", err)
	}
	// Overmind's own store replaces Dragonfly's LevelDB one: a file per
	// account, written atomically, and a bad record never takes the server down.
	uc.Players.SaveData = false
	// The hub announces joins and leaves for both editions instead.
	uc.Server.DisableJoinQuitMessages = true
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

	h := newHub(log)
	h.blockOddShapes = cfg.Skins.BlockOddShapes
	if cfg.Skins.BlockOddShapes {
		conf.JoinSkin = h.allowedSkin
	}
	conf.StatusProvider = statusProvider{name: uc.Server.Name, max: cfg.MaxPlayers, count: h.count}
	bedrockPlayers, err := playerstore.Open(filepath.Join(cfg.PlayersFolder, "bedrock"), log)
	if err != nil {
		return nil, fmt.Errorf("open bedrock player data: %w", err)
	}
	conf.PlayerProvider = bedrockPlayers
	javaPlayers, err := playerstore.Open(filepath.Join(cfg.PlayersFolder, "java"), log)
	if err != nil {
		_ = bedrockPlayers.Close()
		return nil, fmt.Errorf("open java player data: %w", err)
	}
	h.stores = map[edition]player.Provider{bedrock: bedrockPlayers, java: javaPlayers}
	// One name, one player, across both editions. Bedrock is refused here; Java
	// asks the other way round when it joins.
	names := &javaNames{maxPlayers: cfg.MaxPlayers, reserved: map[string]reservation{}}
	names.javaCount = func() int {
		if f := names.front.Load(); f != nil {
			return f.Count()
		}
		return 0
	}
	// conf.New starts the Bedrock listeners, which call Allow at once, so
	// everything Allow reads is set before it and the server is reached
	// through an atomic pointer.
	var bedrockSrv atomic.Pointer[server.Server]
	names.bedrockOnline = func() int {
		if s := bedrockSrv.Load(); s != nil {
			return s.PlayerCount()
		}
		return 0
	}
	conf.Allower = names
	mode, err := gameMode(cfg.GameMode)
	if err != nil {
		_ = javaPlayers.Close()
		_ = conf.PlayerProvider.Close()
		return nil, err
	}
	srv := conf.New()
	bedrockSrv.Store(srv)
	guard.watch(srv.World())
	// Dragonfly's default comes from level.dat and is creative.
	for _, w := range []*world.World{srv.World(), srv.Nether(), srv.End()} {
		w.SetDefaultGameMode(mode)
	}
	if err := preloadSpawn(srv.World(), int(set.TickRange)+2); err != nil {
		log.Warn("Spawn chunks did not load in time", "error", err)
	}
	srv.Listen()

	if cfg.JavaOffline {
		log.Warn("Mojang login is OFF. Anyone can join on Java under any name. Use this only for test bots.")
	}
	uploader, err := startUploader(cfg.Skins, log)
	if err != nil {
		_ = srv.Close()
		_ = javaPlayers.Close()
		return nil, err
	}
	front, err := javafront.Start(javafront.Options{
		Addr:  cfg.JavaAddr,
		Login: javalogin.Options{Offline: cfg.JavaOffline, OnlinePlayers: h.count},
		World: srv.World(),
		Worlds: func(d world.Dimension) *world.World {
			switch d {
			case world.Nether:
				return srv.Nether()
			case world.End:
				return srv.End()
			}
			return nil
		},
		ViewDistance:     cfg.ViewDistance,
		MaxPlayers:       cfg.MaxPlayers,
		OtherPlayerNames: func(name string) bool { return names.reservedName(name) || bedrockHolds(srv, name) },
		OtherPlayerCount: names.otherPlayers,
		OtherPlayerUUID:  func(id uuid.UUID) bool { return names.reservedID(id) || bedrockHoldsID(srv, id) },
		Players:          javaPlayers,
		Joined:           func(p *player.Player, chat func(sender, msg string)) { h.joined(p, java, chat) },
		Left:             func(id uuid.UUID) { h.left(java, id) },
		Log:              log,
		Uploader:         uploader,
	})
	if err != nil {
		if uploader != nil {
			uploader.Close()
		}
		_ = srv.Close()
		_ = javaPlayers.Close()
		return nil, fmt.Errorf("start java front end: %w", err)
	}
	names.front.Store(front)
	h.java.Store(front)
	// Bedrock players wait in Accept until here, so none is missed by the
	// Java player list.
	go func() {
		for p := range srv.Accept() {
			names.arrived(p.Name())
			h.joined(p, bedrock, nil)
		}
	}()
	stop := h.autosave(time.Duration(cfg.SaveSeconds) * time.Second)
	return &Server{Server: srv, Java: front, hub: h, javaPlayers: javaPlayers, stopSaving: stop, uploader: uploader}, nil
}

// startUploader starts the skin uploader if the settings name an account; nil
// if they do not.
func startUploader(cfg config.Skins, log *slog.Logger) (*skins.Uploader, error) {
	if cfg.Uploader != nil {
		return cfg.Uploader, nil
	}
	if cfg.UploadAccount == "" {
		return nil, nil
	}
	acc, err := skins.OpenAccount(cfg.UploadAccount, skins.Endpoints{}, nil)
	if err != nil {
		return nil, err
	}
	u, err := skins.NewUploader(skins.UploaderConfig{Tokens: acc, CacheDir: cfg.UploadCache,
		Gap: time.Duration(cfg.UploadGapSeconds) * time.Second, Log: log})
	if err != nil {
		return nil, err
	}
	log.Warn("Bedrock skins are uploaded to a Java account for Java viewers. Mojang may suspend an account that uploads too often.")
	return u, nil
}

// reservationLife is how long a Bedrock player may take from Allow to being
// online. Their name is held for Java for that long, even if they never arrive.
const reservationLife = 15 * time.Second

type reservation struct {
	identity string
	expires  time.Time
	arrived  bool // online now, so counted by bedrockOnline and not as joining
}

// javaNames is the Bedrock side of the rules both editions share: one name, one
// player, and one player limit. It implements server.Allower.
type javaNames struct {
	front         atomic.Pointer[javafront.Front]
	maxPlayers    int
	bedrockOnline func() int // Bedrock players online
	javaCount     func() int // Java players online or joining

	mu       sync.Mutex
	reserved map[string]reservation // by lower-case name
}

// Allow implements server.Allower. The name and a player slot are reserved
// before anything is counted: Front.claim takes its slot before it counts too,
// so of two joins for the last slot, at most one gets in.
func (n *javaNames) Allow(_ net.Addr, d login.IdentityData, _ login.ClientData) (string, bool) {
	key := strings.ToLower(d.DisplayName)
	now := time.Now()
	n.mu.Lock()
	for k, r := range n.reserved {
		if now.After(r.expires) {
			delete(n.reserved, k)
		}
	}
	if r, ok := n.reserved[key]; ok && r.identity != d.Identity {
		n.mu.Unlock()
		return "That name is already online.", false
	}
	n.reserved[key] = reservation{identity: d.Identity, expires: now.Add(reservationLife)}
	n.mu.Unlock()
	unreserve := func() {
		n.mu.Lock()
		delete(n.reserved, key)
		n.mu.Unlock()
	}
	if n.maxPlayers > 0 && n.otherPlayers()+n.javaCount() > n.maxPlayers {
		unreserve()
		return "The server is full.", false
	}
	if f := n.front.Load(); f != nil && f.NameTaken(d.DisplayName) {
		unreserve()
		return "That name is already online.", false
	}
	// The reservation holds the account too, so a Java join with the same UUID
	// sees it (reservedID); and this sees a Java player already in.
	if id, err := uuid.Parse(d.Identity); err == nil {
		if f := n.front.Load(); f != nil && f.UUIDTaken(id) {
			unreserve()
			return javafront.SameAccountOnline, false
		}
	}
	return "", true
}

// reservedID reports whether a Bedrock player is on the way, or recently
// arrived, with this account UUID.
func (n *javaNames) reservedID(id uuid.UUID) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	now := time.Now()
	for _, r := range n.reserved {
		if r.identity == id.String() && now.Before(r.expires) {
			return true
		}
	}
	return false
}

// otherPlayers counts Bedrock players online or on their way, for the Java side.
func (n *javaNames) otherPlayers() int {
	online := 0
	if n.bedrockOnline != nil {
		online = n.bedrockOnline()
	}
	now := time.Now()
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, r := range n.reserved {
		if !r.arrived && now.Before(r.expires) {
			online++
		}
	}
	return online
}

// arrived marks a Bedrock player online: it now counts through bedrockOnline.
// Its name stays reserved until the reservation runs out.
func (n *javaNames) arrived(name string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	key := strings.ToLower(name)
	if r, ok := n.reserved[key]; ok {
		r.arrived = true
		n.reserved[key] = r
	}
}

// reservedName reports whether a Bedrock player is on the way with this name.
func (n *javaNames) reservedName(name string) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	r, ok := n.reserved[strings.ToLower(name)]
	return ok && time.Now().Before(r.expires)
}

func gameMode(name string) (world.GameMode, error) {
	switch name {
	case "survival":
		return world.GameModeSurvival, nil
	case "creative":
		return world.GameModeCreative, nil
	case "adventure":
		return world.GameModeAdventure, nil
	case "spectator":
		return world.GameModeSpectator, nil
	}
	return nil, fmt.Errorf("unknown game mode %q in the settings", name)
}

// bedrockHoldsID reports whether a Bedrock player online has this account.
func bedrockHoldsID(srv *server.Server, id uuid.UUID) bool {
	_, ok := srv.Player(id)
	return ok
}

// bedrockHolds reports whether a Bedrock player online has this name. Players
// goes through each player's world, so it must not be called from a world's
// goroutine. Front.claim calls it from the connection's own goroutine.
func bedrockHolds(srv *server.Server, name string) bool {
	for p := range srv.Players(nil) {
		if strings.EqualFold(p.Name(), name) {
			return true
		}
	}
	return false
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

// unusedGenerator makes the Nether and End a flat floor. A player-built Nether portal reaches the
// Nether; nothing generates an End portal yet.
func unusedGenerator(dim world.Dimension) world.Generator {
	if dim == world.Nether {
		return generator.NewFlat(biome.NetherWastes{}, []world.Block{block.Netherrack{}, block.Bedrock{}})
	}
	return generator.NewFlat(biome.End{}, []world.Block{block.EndStone{}, block.Bedrock{}})
}
