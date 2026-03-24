package main

import (
	"log/slog"
	"os"
	"sync"

	"github.com/go-gl/mathgl/mgl64"
	"github.com/overmind/bedrock/bridge"
	"github.com/overmind/bedrock/server"
	"github.com/overmind/bedrock/server/player"
	"github.com/overmind/bedrock/server/world/mcdb"
	"github.com/pelletier/go-toml"
)

// playerMap tracks every online Bedrock player by name for command dispatch.
var (
	playerMapMu sync.RWMutex
	playerMap   = map[string]*player.Player{}
)

func addPlayer(p *player.Player)    { playerMapMu.Lock(); playerMap[p.Name()] = p; playerMapMu.Unlock() }
func removePlayer(p *player.Player) { playerMapMu.Lock(); delete(playerMap, p.Name()); playerMapMu.Unlock() }
func lookupPlayer(name string) (*player.Player, bool) {
	playerMapMu.RLock()
	defer playerMapMu.RUnlock()
	p, ok := playerMap[name]
	return p, ok
}

const (
	configPath  = "overmind.toml"
	bridgeAddr  = ":25566" // internal TCP port for the Java cross-play bridge
)

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	uc := server.DefaultConfig()
	uc.Server.Name = "Overmind"
	uc.Network.Address = ":19132"
	uc.World.SaveData = true
	uc.World.Folder = "server/bedrock/world"
	uc.Players.SaveData = true
	uc.Players.Folder = "server/bedrock/players"
	uc.Resources.Folder = "server/bedrock/resources"
	uc.Resources.AutoBuildPack = true
	// Disable Xbox Live auth for local development; set AuthEnabled = true in overmind.toml for production.
	uc.Server.AuthEnabled = false

	if data, err := os.ReadFile(configPath); err == nil {
		if err = toml.Unmarshal(data, &uc); err != nil {
			log.Error("parse overmind.toml", "err", err)
			os.Exit(1)
		}
	} else if !os.IsNotExist(err) {
		log.Error("read overmind.toml", "err", err)
		os.Exit(1)
	}

	conf, err := uc.Config(log)
	if err != nil {
		log.Error("build server config", "err", err)
		os.Exit(1)
	}

	// Open LevelDB world provider.
	if uc.World.SaveData {
		conf.WorldProvider, err = mcdb.Config{Log: log}.Open(uc.World.Folder)
		if err != nil {
			log.Error("open world", "err", err)
			os.Exit(1)
		}
	}

	// Start the Java cross-play bridge server.
	br, err := bridge.New(log, bridgeAddr)
	if err != nil {
		log.Error("start bridge", "err", err)
		os.Exit(1)
	}

	// Register handler for inbound Java → Go commands.
	br.SetCommandHandler(func(cmd bridge.Command) {
		switch cmd.Cmd {
		case bridge.CmdKick:
			if p, ok := lookupPlayer(cmd.Name); ok {
				p.Disconnect(cmd.Reason)
				log.Info("Bridge: kicked player", "name", cmd.Name, "reason", cmd.Reason)
			}
		case bridge.CmdMessage:
			if p, ok := lookupPlayer(cmd.Name); ok {
				p.Message(cmd.Text)
				log.Info("Bridge: messaged player", "name", cmd.Name)
			}
		case bridge.CmdBroadcast:
			playerMapMu.RLock()
			for _, p := range playerMap {
				p.Message(cmd.Text)
			}
			playerMapMu.RUnlock()
			log.Info("Bridge: broadcast sent", "text", cmd.Text)
		case bridge.CmdTeleport:
			if p, ok := lookupPlayer(cmd.Name); ok {
				p.Teleport(mgl64.Vec3{cmd.X, cmd.Y, cmd.Z})
				log.Info("Bridge: teleported player", "name", cmd.Name, "pos", cmd)
			}
		default:
			log.Warn("Bridge: unknown command", "cmd", cmd.Cmd)
		}
	})

	srv := conf.New()
	srv.Listen()

	// Accept blocks until all listeners are closed (server shutdown).
	// For each Bedrock player that connects:
	//   1. Track in playerMap for command dispatch.
	//   2. Emit a "join" event to the Java side.
	//   3. Assign a PlayerHandler that forwards move/quit events and removes from map.
	for p := range srv.Accept() {
		addPlayer(p)
		pos := p.Position()
		br.Emit(bridge.PlayerEvent{
			Type: bridge.EventJoin,
			Name: p.Name(),
			X:    pos[0],
			Y:    pos[1],
			Z:    pos[2],
		})
		h := bridge.NewPlayerHandler(br, p.Name())
		h.OnQuit(func() { removePlayer(p) })
		p.Handle(h)
	}
}
