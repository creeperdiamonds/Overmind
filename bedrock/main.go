package main

import (
	"bufio"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
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

const configPath = "overmind.toml"

// overmindRoot mirrors the top-level structure of overmind.toml.
type overmindRoot struct {
	Bedrock server.UserConfig `toml:"bedrock"`
	Bridge  struct {
		Address string `toml:"address"`
	} `toml:"bridge"`
}

// ── Server handle ─────────────────────────────────────────────────────────────

// srvHandle gives the console goroutine thread-safe access to the server
// instance before and after it is created.
type srvHandle struct {
	mu  sync.Mutex
	srv *server.Server
}

func (h *srvHandle) set(s *server.Server) { h.mu.Lock(); h.srv = s; h.mu.Unlock() }
func (h *srvHandle) get() *server.Server  { h.mu.Lock(); defer h.mu.Unlock(); return h.srv }

// ── Operator file ─────────────────────────────────────────────────────────────

const opsFile = "ops.txt"

func loadOps(log *slog.Logger) map[string]struct{} {
	ops := make(map[string]struct{})
	data, err := os.ReadFile(opsFile)
	if os.IsNotExist(err) {
		return ops
	}
	if err != nil {
		log.Warn("could not read ops.txt", "err", err)
		return ops
	}
	for _, line := range strings.Split(string(data), "\n") {
		if name := strings.TrimSpace(line); name != "" {
			ops[name] = struct{}{}
		}
	}
	return ops
}

func saveOps(ops map[string]struct{}, log *slog.Logger) {
	names := make([]string, 0, len(ops))
	for n := range ops {
		names = append(names, n)
	}
	sort.Strings(names)
	var sb strings.Builder
	for _, n := range names {
		sb.WriteString(n)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(opsFile, []byte(sb.String()), 0o644); err != nil {
		log.Warn("could not write ops.txt", "err", err)
	}
}

// ── Console ───────────────────────────────────────────────────────────────────

// runConsole reads operator commands from stdin.
// It starts immediately at server launch so commands like /op work during
// world loading and other slow initialisation.
func runConsole(log *slog.Logger, handle *srvHandle) {
	scanner := bufio.NewScanner(os.Stdin)
	log.Info("Console ready — type 'help' for commands")
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Accept "/command args" or "command args"
		if strings.HasPrefix(line, "/") {
			line = line[1:]
		}
		parts := strings.SplitN(line, " ", 2)
		cmd := strings.ToLower(parts[0])
		args := ""
		if len(parts) > 1 {
			args = strings.TrimSpace(parts[1])
		}

		switch cmd {
		case "stop":
			fmt.Println("[Console] Stopping server...")
			log.Info("console: stop requested")
			if s := handle.get(); s != nil {
				s.Close()
			} else {
				os.Exit(0)
			}
			return

		case "list":
			playerMapMu.RLock()
			if len(playerMap) == 0 {
				fmt.Println("No players online.")
			} else {
				names := make([]string, 0, len(playerMap))
				for n := range playerMap {
					names = append(names, n)
				}
				fmt.Printf("Online (%d): %s\n", len(playerMap), strings.Join(names, ", "))
			}
			playerMapMu.RUnlock()

		case "kick":
			if args == "" {
				fmt.Println("Usage: kick <player> [reason]")
				continue
			}
			kp := strings.SplitN(args, " ", 2)
			reason := "Kicked by an operator"
			if len(kp) > 1 {
				reason = kp[1]
			}
			if p, ok := lookupPlayer(kp[0]); ok {
				p.Disconnect(reason)
				fmt.Printf("Kicked %s: %s\n", kp[0], reason)
				log.Info("console: kicked player", "name", kp[0], "reason", reason)
			} else {
				fmt.Printf("Player '%s' is not online.\n", kp[0])
			}

		case "say":
			if args == "" {
				fmt.Println("Usage: say <message>")
				continue
			}
			msg := "[Server] " + args
			playerMapMu.RLock()
			for _, p := range playerMap {
				p.Message(msg)
			}
			playerMapMu.RUnlock()
			fmt.Println(msg)
			log.Info("console: broadcast", "message", args)

		case "op":
			if args == "" {
				fmt.Println("Usage: op <player>")
				continue
			}
			ops := loadOps(log)
			ops[args] = struct{}{}
			saveOps(ops, log)
			fmt.Printf("Made %s a server operator.\n", args)
			log.Info("console: opped player", "name", args)

		case "deop":
			if args == "" {
				fmt.Println("Usage: deop <player>")
				continue
			}
			ops := loadOps(log)
			delete(ops, args)
			saveOps(ops, log)
			fmt.Printf("Removed %s from operators.\n", args)
			log.Info("console: deopped player", "name", args)

		case "ops":
			ops := loadOps(log)
			if len(ops) == 0 {
				fmt.Println("No operators configured.")
			} else {
				names := make([]string, 0, len(ops))
				for n := range ops {
					names = append(names, n)
				}
				sort.Strings(names)
				fmt.Printf("Operators (%d): %s\n", len(ops), strings.Join(names, ", "))
			}

		case "help":
			fmt.Println("Commands: stop | list | kick <player> [reason] | say <message> | op <player> | deop <player> | ops | help")

		default:
			fmt.Printf("Unknown command '%s'. Type 'help' for a list.\n", cmd)
		}
	}
}

// ── Entry point ───────────────────────────────────────────────────────────────

func main() {
	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	// Start the console goroutine immediately so operators can run commands
	// (e.g. /op) while the world and bridge are initialising.
	handle := &srvHandle{}
	go runConsole(log, handle)

	// Defaults — overridden by overmind.toml if present.
	root := overmindRoot{}
	root.Bedrock = server.DefaultConfig()
	root.Bedrock.Server.Name = "Overmind"
	root.Bedrock.Network.Address = ":19132"
	root.Bedrock.World.SaveData = true
	root.Bedrock.World.Folder = "server/bedrock/world"
	root.Bedrock.Players.SaveData = true
	root.Bedrock.Players.Folder = "server/bedrock/players"
	root.Bedrock.Resources.Folder = "server/bedrock/resources"
	root.Bedrock.Resources.AutoBuildPack = true
	root.Bridge.Address = ":25566"

	if data, err := os.ReadFile(configPath); err == nil {
		if err = toml.Unmarshal(data, &root); err != nil {
			log.Error("parse overmind.toml", "err", err)
			os.Exit(1)
		}
	} else if !os.IsNotExist(err) {
		log.Error("read overmind.toml", "err", err)
		os.Exit(1)
	}

	// ── Xbox Live authentication — always enforced for EULA compliance ────────
	// The TOML value is intentionally ignored and overridden here.
	// To disable for LOCAL DEVELOPMENT ONLY, set OVERMIND_DEV_OFFLINE=true.
	// WARNING: Disabling authentication likely violates the Minecraft EULA.
	//          NEVER run with auth disabled on a public-facing server.
	authEnv := os.Getenv("OVERMIND_DEV_OFFLINE")
	if authEnv == "true" || authEnv == "1" {
		root.Bedrock.Server.AuthEnabled = false
		log.Warn("╔══════════════════════════════════════════════════════╗")
		log.Warn("║  OFFLINE MODE ENABLED — EULA WARNING                 ║")
		log.Warn("║  Xbox Live auth DISABLED (OVERMIND_DEV_OFFLINE=true) ║")
		log.Warn("║  This likely violates the Minecraft EULA.            ║")
		log.Warn("║  NEVER run in this mode on a public-facing server.   ║")
		log.Warn("╚══════════════════════════════════════════════════════╝")
	} else {
		root.Bedrock.Server.AuthEnabled = true
	}

	uc := root.Bedrock
	bridgeAddr := root.Bridge.Address
	if bridgeAddr == "" {
		bridgeAddr = ":25566"
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
	handle.set(srv) // expose server to console goroutine
	srv.Listen()

	// Accept blocks until all listeners are closed (server shutdown).
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
