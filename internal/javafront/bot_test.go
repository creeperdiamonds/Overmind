package javafront_test

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// javaBot drives bots/java/player.js.
type javaBot struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  *json.Encoder
	mu     sync.Mutex
	nextID int
	reply  map[int]chan json.RawMessage
	kicked chan string
	played chan struct{}
	end    atomic.Bool
	over   atomic.Bool // the test has finished

	errMu sync.Mutex
	errs  []string // packets the protocol.json parser could not read
}

type columnEntry struct {
	Block string `json:"block"`
	Biome string `json:"biome"`
	Sky   int    `json:"sky"`
}

type columnReply struct {
	Loaded     bool                `json:"loaded"`
	Blocks     map[int]columnEntry `json:"blocks"`
	Heightmaps []string            `json:"heightmaps"`
	Height     int                 `json:"height"`
}

type botState struct {
	Me struct {
		X, Y, Z, Yaw, Pitch float64
		OnGround            bool `json:"onGround"`
		Known               bool `json:"known"`
	} `json:"me"`
	Seen struct {
		Login *struct {
			WorldNames []string `json:"worldNames"`
			WorldState struct {
				GameMode string `json:"gamemode"`
			} `json:"worldState"`
		} `json:"login"`
		Spawn *struct {
			GlobalPos struct {
				DimensionName string `json:"dimensionName"`
			} `json:"globalPos"`
		} `json:"spawn"`
		Health     *float64       `json:"health"`
		Respawns   int            `json:"respawns"`
		KeepAlives int            `json:"keepAlives"`
		Batches    int            `json:"batches"`
		Packets    map[string]int `json:"packets"`
	} `json:"seen"`
}

type seenPlayer struct {
	X, Y, Z, Yaw, Pitch, Head float64
}

type blockReply struct {
	Name  string `json:"name"`
	State *int32 `json:"state"` // the last state ID sent for the block
	Acks  []int  `json:"acks"`
}

func botScript(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("..", "..", "bots", "java"))
	require.NoError(t, err)
	if _, err := os.Stat(filepath.Join(dir, "node_modules", "prismarine-chunk")); err != nil {
		t.Skip("run npm install in bots/java first")
	}
	return filepath.Join(dir, "player.js")
}

// startBotRaw launches the bot and returns without waiting for it to play.
func startBotRaw(t *testing.T, srv *testserver.Server, name string) *javaBot {
	t.Helper()
	return startBotAt(t, srv.JavaAddr, name)
}

func startBotAt(t *testing.T, addr, name string) *javaBot {
	t.Helper()
	host, port, _ := strings.Cut(addr, ":")
	b := &javaBot{t: t, reply: map[int]chan json.RawMessage{}, kicked: make(chan string, 1), played: make(chan struct{})}
	b.cmd = exec.Command("node", botScript(t), host, port, name)
	in, err := b.cmd.StdinPipe()
	require.NoError(t, err)
	out, err := b.cmd.StdoutPipe()
	require.NoError(t, err)
	b.cmd.Stderr = os.Stderr
	b.stdin = json.NewEncoder(in)
	require.NoError(t, b.cmd.Start())
	t.Cleanup(func() {
		b.over.Store(true)
		b.kill()
		b.errMu.Lock()
		defer b.errMu.Unlock()
		for _, e := range b.errs {
			t.Errorf("bot %s could not parse a packet: %s", name, e)
		}
	})
	go b.read(out)
	return b
}

// startBot launches the bot and waits until the server has put it in play.
func startBot(t *testing.T, srv *testserver.Server, name string) *javaBot {
	t.Helper()
	b := startBotRaw(t, srv, name)
	select {
	case <-b.played:
	case <-time.After(20 * time.Second):
		t.Fatalf("bot %s never reached play", name)
	}
	return b
}

func (b *javaBot) read(out io.Reader) {
	var played sync.Once
	s := bufio.NewScanner(out)
	s.Buffer(make([]byte, 1<<16), 1<<24)
	for s.Scan() {
		var m struct {
			ID     *int   `json:"id"`
			Ev     string `json:"ev"`
			Reason string `json:"reason"`
		}
		if json.Unmarshal(s.Bytes(), &m) != nil {
			continue
		}
		switch {
		case m.ID != nil:
			b.mu.Lock()
			ch := b.reply[*m.ID]
			delete(b.reply, *m.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- append(json.RawMessage(nil), s.Bytes()...)
			}
		case m.Ev == "play":
			played.Do(func() { close(b.played) })
		case m.Ev == "kick":
			select {
			case b.kicked <- m.Reason:
			default:
			}
		case m.Ev == "error":
			b.errMu.Lock()
			b.errs = append(b.errs, m.Reason)
			b.errMu.Unlock()
		case m.Ev == "end":
			b.end.Store(true)
		}
	}
	b.end.Store(true)
}

func (b *javaBot) ended() bool { return b.end.Load() }

func (b *javaBot) clearErrors() {
	b.errMu.Lock()
	b.errs = nil
	b.errMu.Unlock()
}

func (b *javaBot) kill() {
	if b.cmd.Process != nil {
		_ = b.cmd.Process.Kill()
		_, _ = b.cmd.Process.Wait()
	}
	b.end.Store(true)
}

func (b *javaBot) waitKicked(t *testing.T) string {
	t.Helper()
	select {
	case r := <-b.kicked:
		return r
	case <-time.After(20 * time.Second):
		t.Fatal("bot was never kicked")
		return ""
	}
}

// call sends a command and waits for its answer; a failure fails the test.
// Once the test is over (an Eventually condition can still be running), a
// failure is dropped instead: testing panics on a failure reported after the
// test completed.
func (b *javaBot) call(cmd string, args map[string]any, out any) {
	b.t.Helper()
	if err := b.tryCall(cmd, args, out); err != nil && !b.over.Load() {
		b.t.Fatal(err)
	}
}

// tryCall is call that reports a failure instead of failing the test, and
// never touches the test.
func (b *javaBot) tryCall(cmd string, args map[string]any, out any) error {
	b.mu.Lock()
	b.nextID++
	id := b.nextID
	ch := make(chan json.RawMessage, 1)
	b.reply[id] = ch
	b.mu.Unlock()
	msg := map[string]any{"id": id, "cmd": cmd}
	for k, v := range args {
		msg[k] = v
	}
	if err := b.stdin.Encode(msg); err != nil {
		return fmt.Errorf("bot %s: %w", cmd, err)
	}
	select {
	case raw := <-ch:
		var e struct {
			Err string `json:"err"`
		}
		_ = json.Unmarshal(raw, &e)
		if e.Err != "" {
			return fmt.Errorf("bot %s: %s", cmd, e.Err)
		}
		if out != nil {
			if err := json.Unmarshal(raw, out); err != nil {
				return fmt.Errorf("bot %s: %w", cmd, err)
			}
		}
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("bot did not answer %s", cmd)
	}
}

func (b *javaBot) state() botState {
	var s botState
	b.call("state", nil, &s)
	return s
}

func (b *javaBot) column(x, z, from, to int) columnReply {
	var c columnReply
	b.call("column", map[string]any{"x": x, "z": z, "from": from, "to": to}, &c)
	return c
}

func (b *javaBot) stateName(state int32) string {
	var r struct {
		Name string `json:"name"`
	}
	b.call("stateName", map[string]any{"state": state}, &r)
	return r.Name
}

func (b *javaBot) walk(angle float64) { b.call("walk", map[string]any{"angle": angle}, nil) }
func (b *javaBot) walkFast(angle, speed float64) {
	b.call("walk", map[string]any{"angle": angle, "speed": speed}, nil)
}
func (b *javaBot) stop()            { b.call("stop", nil, nil) }
func (b *javaBot) climb(y float64)  { b.call("climb", map[string]any{"y": y}, nil) }
func (b *javaBot) respawn()         { b.call("respawn", nil, nil) }
func (b *javaBot) jump(dx float64)  { b.call("jump", map[string]any{"dx": dx}, nil) }
func (b *javaBot) junk(kind string) { b.call("junk", map[string]any{"kind": kind}, nil) }

func (b *javaBot) dig(status int, pos cube.Pos, face, seq int) {
	b.call("dig", map[string]any{"status": status, "x": pos[0], "y": pos[1], "z": pos[2], "face": face, "sequence": seq}, nil)
}

func (b *javaBot) block(pos cube.Pos) blockReply {
	var r blockReply
	b.call("block", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2]}, &r)
	return r
}

// look turns the bot, in Java's degrees: yaw 0 faces +Z, pitch 90 looks down.
func (b *javaBot) look(yaw, pitch float64) {
	b.call("look", map[string]any{"yaw": yaw, "pitch": pitch}, nil)
}

// place sends block_place with the main hand, the cursor at (cx, cy, cz) on
// the face.
func (b *javaBot) place(pos cube.Pos, face cube.Face, cx, cy, cz float64, seq int) {
	b.call("place", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2], "face": int(face), "cx": cx, "cy": cy, "cz": cz, "hand": 0, "sequence": seq}, nil)
}

// useItem sends use_item with the main hand.
func (b *javaBot) useItem(seq int) {
	b.call("useItem", map[string]any{"hand": 0, "sequence": seq}, nil)
}

// placeHand is place with a hand (0 the main hand, 1 the off hand), and the
// bot guesses it used one, as a vanilla client does.
func (b *javaBot) placeHand(pos cube.Pos, face cube.Face, cx, cy, cz float64, hand, seq int) {
	b.call("place", map[string]any{"x": pos[0], "y": pos[1], "z": pos[2], "face": int(face), "cx": cx, "cy": cy, "cz": cz, "hand": hand, "sequence": seq, "predict": true}, nil)
}

// useItemHand is useItem with a hand.
func (b *javaBot) useItemHand(hand, seq int) {
	b.call("useItem", map[string]any{"hand": hand, "sequence": seq, "predict": true}, nil)
}

// release stops using the held item.
func (b *javaBot) release() { b.call("release", nil, nil) }

func (b *javaBot) players() map[string]seenPlayer {
	var r struct {
		Players map[string]seenPlayer `json:"players"`
	}
	b.call("players", nil, &r)
	return r.Players
}

// botEntity is an entity the bot was shown.
type botEntity struct {
	Type          string  `json:"typeName"`
	Name          *string `json:"name"` // a player's, from the player list
	X, Y, Z       float64
	SpawnVelocity struct{ X, Y, Z float64 }  `json:"spawnVelocity"`
	Velocity      *struct{ X, Y, Z float64 } `json:"velocity"` // the last entity_velocity
	Metadata      map[string]json.RawMessage `json:"metadata"` // by index
}

// item is an item entity's item metadata (index 8 of an item entity).
func (e botEntity) item(t *testing.T) *botItem {
	raw, ok := e.Metadata["8"]
	if !ok {
		return nil
	}
	var it botItem
	require.NoError(t, json.Unmarshal(raw, &it))
	return &it
}

type botCollect struct{ Collected, Collector, Count int32 }

type botEntities struct {
	Entities map[int32]botEntity `json:"entities"`
	Collects []botCollect        `json:"collects"`
	Events   struct {
		Statuses   map[int32][]int `json:"statuses"`
		Animations map[int32][]int `json:"animations"`
		Damage     map[int32][]struct {
			Type, Cause, Direct int32
		} `json:"damage"`
		Hurt []struct {
			ID  int32
			Yaw float64
		} `json:"hurt"`
		Deaths []struct {
			ID      int32           `json:"id"`
			Message json.RawMessage `json:"message"`
		} `json:"deaths"`
	} `json:"events"`
	Self struct {
		Metadata   map[string]json.RawMessage  `json:"metadata"`
		Velocities []struct{ X, Y, Z float64 } `json:"velocities"`
	} `json:"self"`
}

func (b *javaBot) entities() botEntities {
	var r botEntities
	b.call("entities", nil, &r)
	return r
}

// idOf is the entity ID the bot knows a player by, or -1.
func (r botEntities) idOf(name string) int32 {
	for id, e := range r.Entities {
		if e.Name != nil && *e.Name == name {
			return id
		}
	}
	return -1
}

// meta reads one metadata value by index into out; false if never sent.
func (e botEntity) meta(t *testing.T, index int, out any) bool {
	raw, ok := e.Metadata[strconv.Itoa(index)]
	if !ok {
		return false
	}
	require.NoError(t, json.Unmarshal(raw, out))
	return true
}

// input sends player_input with these keys held.
func (b *javaBot) input(keys ...string) { b.call("input", map[string]any{"keys": keys}, nil) }

// playerCommand sends entity_action.
func (b *javaBot) playerCommand(action string) {
	b.call("playerCommand", map[string]any{"action": action}, nil)
}

// settings sends the client settings, with these skin layers.
func (b *javaBot) settings(skinParts int) {
	b.call("settings", map[string]any{"skinParts": skinParts}, nil)
}

func (b *javaBot) swing()          { b.call("swing", nil, nil) }
func (b *javaBot) attack(id int32) { b.call("attack", map[string]any{"target": id}, nil) }

// goTo walks the bot to a point, 0.3 blocks a tick.
func (b *javaBot) goTo(x, z float64) { b.call("goto", map[string]any{"x": x, "z": z}, nil) }

// chatLine is one chat packet as the bot got it. Msg and Name are components
// simplified from NBT: a string, or an object with text, translate, with and
// color.
type chatLine struct {
	Kind string          `json:"kind"`
	Msg  json.RawMessage `json:"msg"`
	Name json.RawMessage `json:"name"`
	Type json.RawMessage `json:"type"`
}

// component is a simplified text component.
type component struct {
	Text      string      `json:"text"`
	Translate string      `json:"translate"`
	Color     string      `json:"color"`
	With      []component `json:"with"`
}

// plain reads a simplified component that may be a bare string.
func plain(raw json.RawMessage) component {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return component{Text: s}
	}
	var c component
	_ = json.Unmarshal(raw, &c)
	return c
}

func (b *javaBot) chat() []chatLine {
	var r struct {
		Chat []chatLine `json:"chat"`
	}
	b.call("chat", nil, &r)
	return r.Chat
}

func (b *javaBot) tab() []string {
	var r struct {
		Names []string `json:"names"`
	}
	b.call("tab", nil, &r)
	return r.Names
}

// quit leaves the server as a client does.
func (b *javaBot) quit() { b.call("quit", nil, nil) }

// botItem is an item as the bot read it from a slot.
type botItem struct {
	Name       string `json:"name"`
	Count      int    `json:"count"`
	Components []struct {
		Type string          `json:"type"`
		Data json.RawMessage `json:"data"`
	} `json:"components"`
}

type botInventory struct {
	Slots  []*botItem `json:"slots"`
	Cursor *botItem   `json:"cursor"`
	Held   int        `json:"held"`
	State  int        `json:"state"`
}

func (b *javaBot) inventory() botInventory {
	var r botInventory
	b.call("inventory", nil, &r)
	return r
}

// click sends window_click: mode 0 pick up (button 0 left, 1 right), 1 shift,
// 2 swap with a hotbar slot, 4 throw.
func (b *javaBot) click(slot, button, mode int) {
	b.call("click", map[string]any{"slot": slot, "button": button, "mode": mode}, nil)
}
func (b *javaBot) hold(slot int)     { b.call("hold", map[string]any{"slot": slot}, nil) }
func (b *javaBot) action(status int) { b.call("action", map[string]any{"status": status}, nil) }
func (b *javaBot) closeWindow()      { b.call("closeWindow", nil, nil) }
func (b *javaBot) creative(slot int, item string, count int) {
	b.call("creative", map[string]any{"slot": slot, "item": item, "count": count}, nil)
}

// equipment is what the bot was shown a player holds and wears, by
// entity_equipment slot.
func (b *javaBot) equipment(name string) map[string]*botItem {
	var r struct {
		Equipment map[string]*botItem `json:"equipment"`
	}
	b.call("equipment", map[string]any{"name": name}, &r)
	return r.Equipment
}

// say sends a chat message, command a command without its slash.
func (b *javaBot) say(text string)     { b.call("say", map[string]any{"text": text}, nil) }
func (b *javaBot) command(text string) { b.call("command", map[string]any{"text": text}, nil) }

func waitOnGround(t *testing.T, srv *testserver.Server, name string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, onGround, ok := srv.JavaPosition(name)
		return ok && onGround
	}, 20*time.Second, 50*time.Millisecond, "%s never stood on the ground", name)
}
