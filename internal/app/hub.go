package app

import (
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"

	"github.com/creeperdiamonds/overmind/internal/give"
	"github.com/creeperdiamonds/overmind/internal/javafront"
	"github.com/creeperdiamonds/overmind/internal/skins"
)

// edition is the client a player plays on.
type edition int

const (
	bedrock edition = iota
	java
)

func (e edition) String() string {
	if e == java {
		return "java"
	}
	return "bedrock"
}

// hub is the one place both editions' players join and leave through. It
// announces them in the chat, puts them in both editions' player lists, counts
// them and saves them now and then. Each edition's front end calls it: Bedrock
// from the server's Accept loop and the player's quit, Java from the Java front
// end's hooks.
type hub struct {
	log  *slog.Logger
	java atomic.Pointer[javafront.Front]
	// stores keep each edition's player data, for the autosave.
	stores map[edition]player.Provider

	// blockOddShapes replaces Bedrock skins that go beyond the standard player
	// model with a default skin, for everyone (the skins.blockOddShapes setting).
	blockOddShapes bool
	// showCoordinates turns on Bedrock's coordinates for each Bedrock player as
	// they join (the ShowCoordinates setting).
	showCoordinates bool
	// operators may use /give (the Operators setting).
	operators give.Operators

	mu      sync.Mutex
	members map[memberKey]*member
}

// memberKey is a player by edition and account. The editions' accounts are
// separate even with the same UUID; while one is online the other is refused at
// login, since Dragonfly and the player lists key by UUID alone.
type memberKey struct {
	edition edition
	id      uuid.UUID
}

// member is one player online.
type member struct {
	name    string
	edition edition
	handle  *world.EntityHandle
	// chat shows a player's chat message to this member in its edition's own
	// format.
	chat func(sender, msg string)
}

func newHub(log *slog.Logger) *hub {
	return &hub{log: log, members: map[memberKey]*member{}}
}

// count is how many players are online on both editions.
func (h *hub) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.members)
}

// joined runs on the player's world goroutine once it is in the world.
// playerChat is how the player's front end shows player chat; nil uses
// Dragonfly's line, as for Bedrock.
func (h *hub) joined(p *player.Player, ed edition, playerChat func(sender, msg string)) {
	m := &member{name: p.Name(), edition: ed, handle: p.H(), chat: playerChat}
	if m.chat == nil {
		// As Dragonfly formats chat for Bedrock. Message only writes to the
		// session, so it may be called from any goroutine, as chat.Global does.
		m.chat = func(sender, msg string) { p.Message("<" + sender + "> " + msg) }
	}
	// Before the player can run a command, or Bedrock's session lists its
	// commands, which it does once this transaction is over.
	give.Join(p.H(), give.Member{Operator: h.operators.Includes(p.Name(), ed == java, p.UUID(), p.XUID()), Java: ed == java})
	h.mu.Lock()
	h.members[memberKey{ed, p.UUID()}] = m
	h.mu.Unlock()
	p.Handle(&playerHandler{h: h, id: p.UUID(), ed: ed})

	if f := h.java.Load(); f != nil {
		f.List(javafront.TabEntry{UUID: p.UUID(), Name: p.Name(), GameMode: p.GameMode(), Skin: p.Skin(), Handle: p.H()})
	}
	if ed == java {
		// Bedrock lists its own players; a Java player has no Bedrock session.
		session.List(p.H(), p.Name(), p.Skin())
	}
	if ed == bedrock && h.showCoordinates {
		// The showcoordinates game rule; it only writes to the session.
		p.ShowCoordinates()
	}
	chat.Global.Writet(chat.MessageJoin, p.Name())
	h.log.Info("player joined", "name", p.Name(), "edition", ed)
}

// left runs once a player has gone. For a Java player its entity has already
// left the world, as session.Unlist needs.
func (h *hub) left(ed edition, id uuid.UUID) {
	h.mu.Lock()
	m, ok := h.members[memberKey{ed, id}]
	delete(h.members, memberKey{ed, id})
	h.mu.Unlock()
	if !ok {
		return
	}
	give.Leave(m.handle)
	if f := h.java.Load(); f != nil {
		f.Unlist(id)
	}
	if m.edition == java {
		session.Unlist(m.handle)
	}
	chat.Global.Writet(chat.MessageQuit, m.name)
	h.log.Info("player left", "name", m.name, "edition", m.edition)
}

// say shows a player's chat message to everyone online, each in its own
// edition's format. Bedrock text going to Java loses what Java's own players
// may not send: section signs and control characters, newlines included.
// Bedrock to Bedrock is left as it was.
func (h *hub) say(from edition, sender, msg string) {
	type target struct {
		ed   edition
		chat func(string, string)
	}
	h.mu.Lock()
	to := make([]target, 0, len(h.members))
	for _, m := range h.members {
		to = append(to, target{m.edition, m.chat})
	}
	h.mu.Unlock()
	forJava := msg
	if from == bedrock {
		forJava = javaSafe(msg)
	}
	for _, t := range to {
		if t.ed == java {
			t.chat(sender, forJava)
		} else {
			t.chat(sender, msg)
		}
	}
	h.log.Info("chat", "name", sender, "edition", from, "message", msg)
}

// javaSafe drops section signs and control characters, by Java's chat rule.
func javaSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '§' || r < ' ' || r == 0x7F {
			return -1
		}
		return r
	}, s)
}

// autosave saves every player online each period, on both editions, besides
// the saves on leave and on stop, so a crash loses at most a period of play.
// Each save is a snapshot on the player's world goroutine; the store marshals
// and writes it elsewhere. The returned func stops it and waits.
func (h *hub) autosave(every time.Duration) (stop func()) {
	if every <= 0 {
		return func() {}
	}
	done, finished := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(finished)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				h.saveAll()
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done); <-finished }) }
}

// saveAll asks each online player's world to snapshot it into its store.
func (h *hub) saveAll() {
	type job struct {
		id     uuid.UUID
		handle *world.EntityHandle
		store  player.Provider
	}
	h.mu.Lock()
	jobs := make([]job, 0, len(h.members))
	for k, m := range h.members {
		jobs = append(jobs, job{k.id, m.handle, h.stores[k.edition]})
	}
	h.mu.Unlock()
	for _, j := range jobs {
		if j.store == nil {
			continue
		}
		j.handle.Do(func(tx *world.Tx, e world.Entity) {
			p, ok := e.(*player.Player)
			if !ok {
				return
			}
			if err := j.store.Save(j.id, p.Data(), tx.World()); err != nil {
				h.log.Error("autosave player data", "name", p.Name(), "err", err)
			}
		})
	}
}

// playerHandler is the handler every player gets from the hub.
type playerHandler struct {
	player.NopHandler
	h  *hub
	id uuid.UUID
	ed edition
}

// HandleChat sends a player's message through the hub instead of chat.Global,
// so that Java clients get it as player chat and not as a server message. Each
// edition's own rules for what a player may send have been applied by then.
func (ph *playerHandler) HandleChat(ctx *player.Context, msg *string) {
	ctx.Cancel()
	ph.h.say(ph.ed, ctx.Player().Name(), *msg)
}

// HandleCommandExecution keeps the command line as the player typed it, for
// /give to read its item from (give.Remember).
func (ph *playerHandler) HandleCommandExecution(ctx *player.Context, _ cmd.Command, args []string) {
	give.Remember(ctx.Player().H(), args)
}

// HandleSkinChange follows a Bedrock player's new skin: with blockOddShapes an
// odd shape becomes the default skin for everyone, and Java viewers' rows
// follow it. A Java player's skin is its Mojang one, set by the Java front end.
func (ph *playerHandler) HandleSkinChange(_ *player.Context, s *skin.Skin) {
	if ph.ed != bedrock {
		return
	}
	*s = ph.h.allowedSkin(*s)
	if f := ph.h.java.Load(); f != nil {
		f.BedrockSkinChanged(ph.id, *s)
	}
}

// allowedSkin is a Bedrock skin as everyone is shown it: the skin itself,
// unless blockOddShapes is on and its shape goes beyond the standard player
// model, which then becomes our stand-in on the same arm width (Mojang's
// default images are not shipped). Java viewers are shown Steve or Alex for it.
func (h *hub) allowedSkin(s skin.Skin) skin.Skin {
	if !h.blockOddShapes {
		return s
	}
	odd, why := skins.OddShape(s)
	if !odd {
		return s
	}
	h.log.Info("bedrock skin replaced: its shape goes beyond the player model", "reason", why)
	return skins.StandIn(skins.Classify(s).Slim)
}

// HandleQuit tells the hub a Bedrock player has gone. A Java player is reported
// by the Java front end once its entity has left the world.
func (ph *playerHandler) HandleQuit(*player.Player) {
	if ph.ed == bedrock {
		ph.h.left(bedrock, ph.id)
	}
}

// statusProvider is what the Bedrock server list shows: the player count of
// both editions.
type statusProvider struct {
	name  string
	max   int
	count func() int
}

// ServerStatus implements minecraft.ServerStatusProvider. The count gophertunnel
// passes is Bedrock's alone, so it is not used.
func (s statusProvider) ServerStatus(_, maxPlayers int) minecraft.ServerStatus {
	if s.max > 0 {
		maxPlayers = s.max
	}
	return minecraft.ServerStatus{ServerName: s.name, PlayerCount: s.count(), MaxPlayers: maxPlayers}
}
