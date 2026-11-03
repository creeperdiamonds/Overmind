// Package javafront is the native Java edition front end. It listens for Java
// clients, gives each a Dragonfly player of its own, shows that player the world
// in Java packets and turns its packets into player actions. No Bedrock
// connection or packet is involved.
package javafront

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"

	"github.com/overmind/overmind/internal/javalogin"
	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/javawire"
	"github.com/overmind/overmind/internal/skins"
)

// Limits on connections still logging in. javalogin holds a goroutine and
// buffers for up to 30 s per login, and each Mojang login makes a web request.
const (
	maxLogins        = 64
	maxLoginsPerAddr = 16
)

// Options configures a Front. The zero value of Login keeps Mojang login on.
type Options struct {
	Addr  string
	Login javalogin.Options
	World *world.World
	// Worlds gives the world of another dimension (the Nether, the End), so a
	// player saved there joins there. Nil, or a nil world, counts the data as
	// none.
	Worlds       func(world.Dimension) *world.World
	ViewDistance int
	MaxPlayers   int
	// OtherPlayerNames reports whether a player elsewhere (on Bedrock) holds a
	// name, and OtherPlayerCount how many there are, those still joining
	// included. Both may be nil.
	OtherPlayerNames func(name string) bool
	OtherPlayerCount func() int
	// OtherPlayerUUID reports whether a player elsewhere (on Bedrock), online or
	// joining, has this account UUID. May be nil.
	OtherPlayerUUID func(id uuid.UUID) bool
	// Players keeps each Java player's data by account UUID while it is away.
	// Nil keeps nothing.
	Players player.Provider
	// Joined is called on the world's goroutine once a player is in the world,
	// with how the player is shown player chat, and Left once it has left it,
	// entity and all. Either may be nil.
	Joined func(p *player.Player, playerChat func(sender, msg string))
	Left   func(id uuid.UUID)
	Log    *slog.Logger
	// FetchSkin fetches a Java player's skin PNG from its profile's URL. Nil
	// fetches it from textures.minecraft.net (skins.go).
	FetchSkin func(ctx context.Context, url string) ([]byte, error)
	// OfflineTextures, for tests, gives an offline login (Login.Offline) the
	// textures property Mojang would give it online. Nil gives none.
	OfflineTextures func(name string) string
	// Uploader has Bedrock players' Java-format skins signed by Mojang, so Java
	// clients can draw them (bedrockskins.go). Nil shows those as Steve or Alex.
	Uploader *skins.Uploader
}

// TabEntry is one row of the player list every Java client shows.
type TabEntry struct {
	UUID     uuid.UUID
	Name     string
	GameMode world.GameMode
	// Skin and Handle are a Bedrock player's: Java clients are shown it by its
	// skin, under a UUID of its own (bedrockskins.go). A Java player's are not
	// read.
	Skin   skin.Skin
	Handle *world.EntityHandle
}

// Front accepts Java players into one world.
type Front struct {
	opts Options
	l    net.Listener

	mu      sync.Mutex
	names   map[string]*conn // by lower-case name
	logins  int
	perAddr map[string]int
	raw     map[net.Conn]struct{} // connections still logging in
	tab     map[uuid.UUID]javaproto.PlayerInfoEntry
	closed  bool

	hmu     sync.Mutex
	strikes map[*world.EntityHandle]world.DamageSource // attacks by Java players under way

	skins *skinCache // Java players' skins, for Bedrock

	bmu    sync.Mutex
	blooks map[uuid.UUID]*bedrockLook // Bedrock players' looks for Java, by account

	wg sync.WaitGroup
}

// Start begins listening.
func Start(opts Options) (*Front, error) {
	if opts.World == nil {
		return nil, errors.New("javafront: no world")
	}
	if opts.Log == nil {
		opts.Log = slog.Default()
	}
	l, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("javafront: listen: %w", err)
	}
	f := &Front{opts: opts, l: l, names: map[string]*conn{}, perAddr: map[string]int{}, raw: map[net.Conn]struct{}{},
		tab: map[uuid.UUID]javaproto.PlayerInfoEntry{}, blooks: map[uuid.UUID]*bedrockLook{}}
	f.opts.Login.MaxPlayers = opts.MaxPlayers
	f.skins = newSkinCache(opts.FetchSkin)
	if f.opts.Login.OnlinePlayers == nil {
		f.opts.Login.OnlinePlayers = f.total
	}
	f.wg.Add(1)
	go f.accept()
	return f, nil
}

// Addr is the address the Front listens on.
func (f *Front) Addr() net.Addr { return f.l.Addr() }

// total counts players on both editions.
func (f *Front) total() int { return f.otherCount() + f.Count() }

func (f *Front) otherCount() int {
	if f.opts.OtherPlayerCount == nil {
		return 0
	}
	return f.opts.OtherPlayerCount()
}

// Count is how many Java players are online.
func (f *Front) Count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.names)
}

// NameTaken reports whether a Java player holds this name, in any case.
func (f *Front) NameTaken(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.names[strings.ToLower(name)]
	return ok
}

// Player finds an online Java player by name.
func (f *Front) Player(name string) (*world.EntityHandle, bool) {
	f.mu.Lock()
	c, ok := f.names[strings.ToLower(name)]
	f.mu.Unlock()
	if !ok || !c.visible.Load() {
		return nil, false
	}
	return c.handle, true
}

// claim reserves a name for c, or says why it can't be had. A player already
// online with the same account is replaced, as vanilla does. The name goes in
// the list before Bedrock is asked, and Bedrock's Allow reserves before it looks
// here, so two joins at once can't both get a name.
func (f *Front) claim(name string, c *conn) string {
	key := strings.ToLower(name)
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return "The server is shutting down."
	}
	if old, taken := f.names[key]; taken {
		f.mu.Unlock()
		if old.id.UUID != c.id.UUID {
			return "That name is already online."
		}
		old.shutdown("You logged in from another location.")
		select {
		case <-old.left:
			f.release(name, old) // in case it left without saying so
		case <-time.After(2 * drainLimit):
			return "That name is already online."
		}
		return f.claim(name, c)
	}
	f.names[key] = c
	f.mu.Unlock()
	// The slot is taken before the count is read, and Bedrock's Allow does the
	// same, so two joins for the last slot cannot both get it.
	if f.opts.MaxPlayers > 0 && f.otherCount()+f.Count() > f.opts.MaxPlayers {
		f.release(name, c)
		return "The server is full."
	}
	if f.opts.OtherPlayerNames != nil && f.opts.OtherPlayerNames(name) {
		f.release(name, c)
		return "That name is already online."
	}
	// One player per account UUID across editions: Dragonfly, the player lists
	// and the hub key by it. The account is in the list already (by its conn),
	// and Bedrock's Allow reserves its own before it looks here.
	if f.opts.OtherPlayerUUID != nil && f.opts.OtherPlayerUUID(c.id.UUID) {
		f.release(name, c)
		return SameAccountOnline
	}
	return ""
}

// SameAccountOnline refuses an account already online on the other edition.
const SameAccountOnline = "This account is already online on the other edition."

// UUIDTaken reports whether a Java player, online or joining, has this account.
func (f *Front) UUIDTaken(id uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.names {
		if c.id.UUID == id {
			return true
		}
	}
	return false
}

// List shows a player, of either edition, in every Java client's player list,
// now and for clients that join later. A Java player's own skin goes with it.
func (f *Front) List(e TabEntry) {
	info := javaproto.PlayerInfoEntry{UUID: e.UUID, Name: javaName(e.Name), GameMode: gameModeID(e.GameMode)}
	if c := f.javaConn(e.UUID); c != nil {
		info.Textures, info.TexturesSignature = c.id.Textures, c.id.TexturesSignature
	} else {
		look := f.bedrockView(e.UUID, e.Handle, e.Skin)
		info.UUID, info.Textures, info.TexturesSignature = look.uuid, look.textures, look.signature
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.tab == nil {
		f.tab = map[uuid.UUID]javaproto.PlayerInfoEntry{}
	}
	f.tab[e.UUID] = info // by account; info.UUID is what Java clients know it by
	for _, c := range f.names {
		c.showInfo(info)
	}
}

// Unlist reverses List.
func (f *Front) Unlist(id uuid.UUID) {
	f.forgetBedrock(id)
	f.mu.Lock()
	defer f.mu.Unlock()
	known := id
	if info, ok := f.tab[id]; ok {
		known = info.UUID
	}
	delete(f.tab, id)
	for _, c := range f.names {
		c.hideInfo(known)
	}
}

// BedrockSkinChanged is told a Bedrock player's new skin before it is set, so
// every Java client's row follows it; the clients that see the player show it
// again when Dragonfly tells them of the change (ViewSkin).
func (f *Front) BedrockSkinChanged(account uuid.UUID, s skin.Skin) {
	if f.javaConn(account) != nil {
		return
	}
	f.bedrockView(account, nil, s)
}

// openTab lets c receive player list rows from now on and sends it every row
// listed, its own first. It follows Login (play), which must come first.
func (f *Front) openTab(c *conn, self javaproto.PlayerInfoEntry) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c.lmu.Lock()
	c.tabOpen = true
	c.lmu.Unlock()
	c.showInfo(self)
	for _, info := range f.tab {
		c.showInfo(info)
	}
}

// listed reports whether a player is in the shared player list.
func (f *Front) listed(id uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.tab[id]
	return ok
}

// players is where Java players' data is kept.
func (f *Front) players() player.Provider {
	if f.opts.Players == nil {
		return player.NopProvider{}
	}
	return f.opts.Players
}

// release frees a claimed name.
func (f *Front) release(name string, c *conn) {
	key := strings.ToLower(name)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.names[key] == c {
		delete(f.names, key)
	}
}

func (f *Front) accept() {
	defer f.wg.Done()
	for {
		nc, err := f.l.Accept()
		if err != nil {
			return
		}
		host, _, _ := net.SplitHostPort(nc.RemoteAddr().String())
		if !f.beginLogin(host) {
			_ = nc.Close()
			continue
		}
		f.wg.Add(1)
		go f.serve(nc, host)
	}
}

func (f *Front) beginLogin(host string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || f.logins >= maxLogins || f.perAddr[host] >= maxLoginsPerAddr {
		return false
	}
	f.logins++
	f.perAddr[host]++
	return true
}

func (f *Front) endLogin(host string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logins--
	if f.perAddr[host]--; f.perAddr[host] == 0 {
		delete(f.perAddr, host)
	}
}

// serve takes one connection from login to the end of play.
func (f *Front) serve(nc net.Conn, host string) {
	defer f.wg.Done()
	wc := javawire.NewConn(nc)
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		f.endLogin(host)
		_ = nc.Close()
		return
	}
	f.raw[nc] = struct{}{}
	f.mu.Unlock()
	id, err := javalogin.Accept(wc, f.opts.Login)
	f.mu.Lock()
	delete(f.raw, nc)
	f.mu.Unlock()
	f.endLogin(host)
	if err != nil {
		if !errors.Is(err, javalogin.ErrStatusServed) {
			f.opts.Log.Debug("java login failed", "addr", nc.RemoteAddr(), "err", err)
		}
		_ = wc.Close()
		return
	}
	if f.opts.Login.Offline && f.opts.OfflineTextures != nil && id.Textures == "" {
		id.Textures = f.opts.OfflineTextures(id.Name)
	}
	newConn(f, wc, nc, id).run()
}

// Close stops listening and disconnects every Java player. It returns once
// they have left the world. Calling it twice is fine.
func (f *Front) Close() {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return
	}
	f.closed = true
	conns := make([]*conn, 0, len(f.names))
	for _, c := range f.names {
		conns = append(conns, c)
	}
	for nc := range f.raw {
		_ = nc.Close()
	}
	f.mu.Unlock()
	_ = f.l.Close()
	for _, c := range conns {
		c.shutdown("The server is closing.")
	}
	f.wg.Wait()
}

// worldOf is the world of a dimension, for loading saved players: the
// Overworld, and the others if Options.Worlds gives them.
func (f *Front) worldOf(d world.Dimension) *world.World {
	if d == f.opts.World.Dimension() {
		return f.opts.World
	}
	if f.opts.Worlds != nil {
		return f.opts.Worlds(d)
	}
	return nil
}
