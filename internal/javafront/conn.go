package javafront

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/chat"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"golang.org/x/text/language"

	"github.com/overmind/overmind/internal/javalogin"
	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/javawire"
	"github.com/overmind/overmind/internal/rules"
	"github.com/overmind/overmind/internal/tables/java261"
)

const (
	tick = time.Second / 20

	// maxQueuedBytes is how much may wait for a slow client, counted with
	// packetOverhead each. Past it the client is dropped: the world never waits
	// for a network write. Chunks are paced (chunksPerTick, maxOpenBatches), so
	// an honest client stays far below it.
	maxQueuedBytes = 8 << 20
	packetOverhead = 32

	// writeLimit is how long one frame may take to reach the socket.
	writeLimit = 10 * time.Second

	keepAliveEvery   = 10 * time.Second
	keepAliveTimeout = 30 * time.Second

	// chunksPerTick matches what the Bedrock session asks for per tick.
	chunksPerTick = 4
	// maxOpenBatches is how many chunk batches a client may leave
	// unacknowledged before it gets no more chunks.
	maxOpenBatches = 10

	// selfEntityID is the client's own entity ID.
	selfEntityID = 1

	coordinateLimit = 30_000_000.0

	// maxMovePerPacket is how far one packet may move a player. Vanilla resets a
	// player past 10 blocks a packet, and so do we.
	maxMovePerPacket = 10.0

	// packetRate and packetBurst bound what a client may send, in packets a
	// second and in one go. A client sends about 60 a second when busy.
	packetRate  = 500.0
	packetBurst = 1000.0

	// A chunk with more edits than this in one tick is sent again whole, through
	// the paced chunk path: that is smaller than the edits.
	maxSectionEditsPerChunk = 1024

	drainLimit = 2 * time.Second
)

var worldNames = []string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"}

const overworldName = "minecraft:overworld"

var (
	playerEntity = java261.EntityType("player")

	// The places in their registries, as configuration sent them, of the
	// Overworld's dimension type and of minecraft:chat.
	overworldDimension = registryIndex("minecraft:dimension_type", overworldName)
	chatType           = registryIndex("minecraft:chat_type", "minecraft:chat")
)

// registryIndex finds an entry's place in a registry the client was sent, once.
// An entry missing from the generated tables is a build error, so it panics.
func registryIndex(registry, entry string) func() int32 {
	return sync.OnceValue(func() int32 {
		for _, r := range java261.Registries() {
			if r.ID != registry {
				continue
			}
			for i, e := range r.Entries {
				if e == entry {
					return int32(i)
				}
			}
		}
		panic("javafront: no " + entry + " in " + registry)
	})
}

// conn is one Java client: the world.Viewer that shows it the world and the
// player.Reporter its Player reports to. The viewer and reporter methods run on
// the world's goroutine, so the fields under "world goroutine" need no lock.
type conn struct {
	silent

	f  *Front
	wc *javawire.Conn
	nc net.Conn
	id *javalogin.Identity

	handle  *world.EntityHandle
	visible atomic.Bool // handle is set and the player is in the world

	qmu    sync.Mutex
	queue  [][]byte
	qBytes int
	wake   chan struct{}

	done      chan struct{} // closed by CloseConnection
	writerEnd chan struct{}
	readerEnd chan struct{}
	stopOnce  sync.Once
	closeOnce sync.Once
	left      chan struct{} // closed when the player has left the world
	ctx       context.Context
	cancel    context.CancelFunc

	teleportSeq  atomic.Int32
	teleportWait atomic.Int32 // teleport the client must confirm; 0 is none
	openBatches  atomic.Int32
	latency      atomic.Int64 // nanoseconds, half a round trip
	aliveSent    atomic.Int64 // nanoseconds since the Unix epoch; 0 is answered

	// The client's player list. Rows come from the Front's list (any world) and
	// from players this client sees, so they are kept under a lock.
	lmu     sync.Mutex
	tabOpen bool                                    // Login (play) has gone, so rows may follow
	rows    map[uuid.UUID]javaproto.PlayerInfoEntry // by the UUID the client knows the player by

	// The world whose callbacks the conn takes, and whether a change of world
	// is under way (see viewing). Written on the player's world goroutine.
	viewW atomic.Pointer[world.World]
	away  atomic.Bool
	// leaving are tasks queued on the worlds the player left, after the
	// loader's removal from them: once all are done, those worlds call the
	// conn no more. World goroutine.
	leaving []*world.Task

	// Chunks sent and waiting: the loader's ViewChunk adds to them from the
	// loader's world, which during a change of world is not yet the player's.
	cmu     sync.Mutex
	sent    map[world.ChunkPos]struct{}
	pending []pendingChunk // chunks waiting for this tick's batch

	// World goroutine.
	loader   *world.Loader
	edits    map[cube.Pos]world.Block    // blocks changed this tick: the first layer's block, nil for a change to the second only
	dirty    map[world.ChunkPos]struct{} // to be sent again whole
	center   world.ChunkPos
	health   float32
	food     int32
	foodSat  float32
	ents     map[*world.EntityHandle]*shown
	nextEnt  int32
	lastPing time.Time
	joined   bool // Options.Joined was called, so Options.Left is owed
	invs     inventories
	// usingSince is when the client started using the held item (eating).
	usingSince time.Time
	// keys is the last player_input: the keys the client holds.
	keys byte
	// skin is the skin layers the client shows (its settings), read by other
	// clients' viewers.
	skin atomic.Uint32

	// rules are the player's own edition's: Java's.
	rules rules.Player
	// attackReset is when the attack cooldown last started again: the last
	// attack or change of held slot.
	attackReset time.Time
	// swungAt is when Dragonfly last showed the player's arm swing.
	swungAt time.Time
	// reshow is set when the client was sent a new level (a respawn), which
	// forgets the chunks and entities it was shown.
	reshow bool
	// dim is the dimension the client is in.
	dim javaDimension
	// win is the block window open, nil if none; nextWin the last window ID.
	win     *window
	nextWin int32
	// drag is a drag being painted over a window's slots.
	drag dragState
	// sign is the sign side the client was asked to edit, nil if none.
	sign *openSign
	// hurtBy is what last hurt the player.
	hurtBy world.DamageSource
}

// pendingChunk is an encoded chunk waiting for the batch.
type pendingChunk struct {
	pos   world.ChunkPos
	frame []byte
}

// shown is an entity the client can see.
type shown struct {
	id   int32
	uuid uuid.UUID // the UUID the client knows it by
	// account is a player's own UUID, and look how it was shown to Java.
	account uuid.UUID
	look    viewLook
	player  bool
	health  float32 // a player's health last sent
	x, y, z int64   // position last sent, in 1/4096 of a block
	yaw     float64
}

var _ player.Reporter = (*conn)(nil)

func newConn(f *Front, wc *javawire.Conn, nc net.Conn, id *javalogin.Identity) *conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &conn{
		f: f, wc: wc, nc: nc, id: id,
		wake:      make(chan struct{}, 1),
		edits:     map[cube.Pos]world.Block{},
		dirty:     map[world.ChunkPos]struct{}{},
		health:    20,
		food:      20,
		foodSat:   5,
		done:      make(chan struct{}),
		writerEnd: make(chan struct{}),
		readerEnd: make(chan struct{}),
		left:      make(chan struct{}),
		ctx:       ctx, cancel: cancel,
		sent:    map[world.ChunkPos]struct{}{},
		rows:    map[uuid.UUID]javaproto.PlayerInfoEntry{},
		ents:    map[*world.EntityHandle]*shown{},
		nextEnt: selfEntityID + 1,
		rules:   rules.Java{},
		dim:     overworldDim,
	}
	if id != nil {
		c.skin.Store(uint32(id.SkinParts))
	}
	return c
}

// run takes the client from the end of configuration to the end of play.
func (c *conn) run() {
	go c.writer()
	defer c.finishReading()
	if msg := c.f.claim(c.id.Name, c); msg != "" {
		c.send(javaproto.Disconnect(msg))
		c.CloseConnection()
		close(c.left)
		return
	}
	if err := c.join(); err != nil {
		c.f.opts.Log.Debug("java join failed", "name", c.id.Name, "err", err)
		c.f.release(c.id.Name, c)
		if c.handle != nil {
			_ = c.handle.Close() // never added to a world
		}
		c.send(javaproto.Disconnect("Could not join."))
		c.CloseConnection()
		close(c.left)
		return
	}
	go c.ticker()
	c.loadSkin()
	err := c.readLoop()
	c.leave(err)
}

// finishReading reads and drops whatever the client still sends until the writer
// has shut the connection. Closing a socket with unread data resets it, and a
// reset can destroy our disconnect message before the client reads it.
func (c *conn) finishReading() {
	for {
		if _, err := c.wc.ReadFrame(); err != nil {
			break
		}
	}
	close(c.readerEnd)
	<-c.writerEnd
}

// join sends what the client needs to enter play and adds the player to the
// world. Nothing has been read from the client yet.
func (c *conn) join() error {
	conf, w := c.loadData()
	c.dim = dimensionOf(w.Dimension())
	spawn := w.Spawn()
	pos := conf.Position
	gm := gameModeID(conf.GameMode)

	c.send(javaproto.Login(c.loginInfo(gm)))
	c.send(javaproto.SpawnPosition(c.dim.name, spawn[0], spawn[1], spawn[2]))
	c.send(javaproto.GameEvent(javaproto.EventStartWaitingForChunks, 0))
	c.f.openTab(c, javaproto.PlayerInfoEntry{
		UUID: c.id.UUID, Name: c.id.Name, Textures: c.id.Textures, TexturesSignature: c.id.TexturesSignature, GameMode: gm,
	})
	c.center = chunkPosOf(pos)
	c.send(javaproto.CenterChunk(c.center[0], c.center[1]))

	teleport := c.teleportSeq.Add(1)
	c.teleportWait.Store(teleport)
	c.send(javaproto.PlayerPosition(teleport, pos[0], pos[1], pos[2], float32(conf.Rotation.Yaw()), float32(conf.Rotation.Pitch())))

	c.handle = world.EntitySpawnOpts{Position: pos, Rotation: conf.Rotation, ID: c.id.UUID}.New(player.Type, conf)
	_, err := world.Call(c.ctx, w, func(tx *world.Tx) (struct{}, error) {
		p := tx.AddEntity(c.handle).(*player.Player)
		d := p.Data()
		c.health, c.food, c.foodSat = float32(d.Health), int32(d.Food), float32(d.Saturation)
		c.sendHealth()
		c.viewW.Store(w)
		c.loader = world.NewLoader(c.f.opts.ViewDistance, w, c)
		c.loader.Move(tx, pos)
		c.sendInventory()
		c.send(javaproto.HeldSlot(int32(d.HeldSlot)))
		c.send(javaproto.EntityMetadata(selfEntityID, c.selfMeta(p)))
		// As a Bedrock session does: the player hears what the server says.
		chat.Global.Subscribe(p)
		c.joined = true
		if c.f.opts.Joined != nil {
			c.f.opts.Joined(p, c.PlayerChat)
		}
		// After Joined, which may set the player's handler.
		c.wrapHandler(p)
		c.attackReset = time.Now()
		return struct{}{}, nil
	})
	if err == nil {
		c.visible.Store(true)
	}
	return err
}

// loadData is the player as it was when it last left, and the world it left
// (the Overworld, or the Nether or the End when Options.Worlds has them), or a
// new player at the Overworld spawn.
func (c *conn) loadData() (player.Config, *world.World) {
	w := c.f.opts.World
	conf, from, err := c.safeLoad()
	if err != nil || from == nil {
		conf = player.Config{Position: w.Spawn().Vec3Centre()}
	} else {
		w = from
	}
	if conf.GameMode == nil {
		conf.GameMode = w.DefaultGameMode()
	}
	conf.Name, conf.UUID, conf.Skin, conf.Session = c.id.Name, c.id.UUID, c.joinSkin(), c
	return conf, w
}

// safeLoad asks the store for the player, and never lets a bad record or a
// broken store panic the process: the player then starts fresh. A store that
// can read nothing says so itself (playerstore logs and moves the record aside).
func (c *conn) safeLoad() (conf player.Config, from *world.World, err error) {
	defer func() {
		if r := recover(); r != nil {
			c.f.opts.Log.Error("java player data could not be loaded", "name", c.id.Name, "panic", r)
			conf, from, err = player.Config{}, nil, fmt.Errorf("load: %v", r)
		}
	}()
	return c.f.players().Load(c.id.UUID, c.f.worldOf)
}

func (c *conn) loginInfo(gm int32) javaproto.LoginInfo {
	return javaproto.LoginInfo{
		EntityID:         selfEntityID,
		WorldNames:       worldNames,
		MaxPlayers:       int32(c.f.opts.MaxPlayers),
		ViewDistance:     int32(c.f.opts.ViewDistance),
		DimensionType:    c.dim.typ(),
		DimensionName:    c.dim.name,
		GameMode:         byte(gm),
		PreviousGameMode: -1,
		SeaLevel:         c.dim.seaLevel,
	}
}

func gameModeID(m world.GameMode) int32 {
	switch m {
	case world.GameModeCreative:
		return 1
	case world.GameModeAdventure:
		return 2
	case world.GameModeSpectator:
		return 3
	}
	return 0
}

// send queues a packet. It never blocks: a client too slow to keep up is dropped.
func (c *conn) send(p []byte) {
	select {
	case <-c.done:
		return
	default:
	}
	cost := len(p) + packetOverhead
	c.qmu.Lock()
	if c.qBytes+cost > maxQueuedBytes {
		c.qmu.Unlock()
		c.CloseConnection()
		return
	}
	c.qBytes += cost
	c.queue = append(c.queue, p)
	c.qmu.Unlock()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// takeQueue returns everything queued. The bytes stay counted until written.
func (c *conn) takeQueue(spare [][]byte) [][]byte {
	c.qmu.Lock()
	defer c.qmu.Unlock()
	q := c.queue
	c.queue = spare[:0]
	return q
}

// writeAll writes frames in order, and reports whether all went out.
func (c *conn) writeAll(frames [][]byte, limitEach bool) bool {
	for i, p := range frames {
		if limitEach {
			_ = c.nc.SetWriteDeadline(time.Now().Add(writeLimit))
		}
		if c.wc.WriteFrame(p) != nil {
			return false
		}
		c.qmu.Lock()
		c.qBytes -= len(p) + packetOverhead
		c.qmu.Unlock()
		frames[i] = nil
	}
	return true
}

// writer owns compression, encryption and the socket. It finishes what is queued
// when asked to stop, within the deadline CloseConnection set, then closes the
// connection. A frame that cannot be written in time ends the connection.
func (c *conn) writer() {
	defer close(c.writerEnd)
	defer c.hangUp()
	defer c.CloseConnection()
	var spare [][]byte
	for {
		select {
		case <-c.wake:
			batch := c.takeQueue(spare)
			if !c.writeAll(batch, true) {
				return
			}
			spare = batch
		case <-c.done:
			c.writeAll(c.takeQueue(nil), false)
			return
		}
	}
}

// hangUp closes our side, gives the client a moment to see that and close its
// own, and then closes the socket.
func (c *conn) hangUp() {
	if t, ok := c.nc.(interface{ CloseWrite() error }); ok && t.CloseWrite() == nil {
		select {
		case <-c.readerEnd:
		case <-time.After(drainLimit):
		}
	}
	_ = c.wc.Close()
}

// ticker is the equivalent of the Bedrock session's background loop.
func (c *conn) ticker() {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-t.C:
			// A player between worlds (a dimension change, a respawn elsewhere) is in
			// none for a moment and the call fails; the next tick finds it. A player
			// that left ends the connection, which closes done.
			_, _ = player.Call(c.ctx, c.handle, func(tx *world.Tx, p *player.Player) (struct{}, error) {
				c.tick(tx, p)
				return struct{}{}, nil
			})
		case <-c.done:
			return
		}
	}
}

// tick moves the chunk loader with the player, sends finished chunks and keeps
// the connection alive.
func (c *conn) tick(tx *world.Tx, p *player.Player) {
	if w := tx.World(); w != c.loader.World() {
		// Moved to another world (a portal, a respawn elsewhere, a plugin).
		c.leaveWorld(tx, w)
	}
	if len(c.leaving) > 0 {
		if !c.oldWorldsDone() {
			c.keepAlive(p)
			return
		}
		c.arrive(tx, p)
	}
	pos := p.Position()
	c.loader.Move(tx, pos)
	// The centre goes first: the client ignores a chunk outside its view.
	if now := chunkPosOf(pos); now != c.center {
		c.center = now
		c.send(javaproto.CenterChunk(now[0], now[1]))
		c.forgetFarChunks()
	}
	if c.openBatches.Load() < maxOpenBatches {
		if n := chunksPerTick - c.resendDirty(); n > 0 {
			c.loader.Load(tx, n)
		}
	}
	c.wrapHandler(p) // again, in case something replaced the handler since
	if c.reshow {
		c.reshow = false
		c.showAgain(tx)
	}
	c.checkWindow(tx, p)
	c.finishEating(p)
	c.moveState(tx, p)
	c.syncHealth(tx)
	c.flushWorld()
	c.keepAlive(p)
}

// flushWorld sends what the world changed, in the order that keeps the client
// right: chunks encoded earlier first, then later edits to them.
func (c *conn) flushWorld() {
	c.flushChunks()
	c.flushEdits()
}

// resendDirty encodes up to chunksPerTick dirty chunks again, nearest the
// player first, into this tick's batch. It returns how many it took, so the
// loader gets the rest of the tick's share.
func (c *conn) resendDirty() int {
	if len(c.dirty) == 0 {
		return 0
	}
	near := make([]world.ChunkPos, 0, len(c.dirty))
	for pos := range c.dirty {
		near = append(near, pos)
	}
	dist := func(p world.ChunkPos) int32 {
		dx, dz := p[0]-c.center[0], p[1]-c.center[1]
		return dx*dx + dz*dz
	}
	slices.SortFunc(near, func(a, b world.ChunkPos) int { return int(dist(a) - dist(b)) })
	taken := 0
	for _, pos := range near {
		if taken == chunksPerTick {
			break
		}
		delete(c.dirty, pos)
		col, ok := c.loader.Chunk(pos)
		if !ok || !c.chunkSent(pos) {
			continue // gone from the view; it comes whole when it is back
		}
		frame := encodeChunk(pos[0], pos[1], col.Chunk, col.BlockEntities, c.dim)
		c.cmu.Lock()
		c.pending = append(c.pending, pendingChunk{pos, frame})
		c.cmu.Unlock()
		taken++
	}
	return taken
}

// flushChunks sends the chunks that arrived since the last tick as one batch.
func (c *conn) flushChunks() {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	if len(c.pending) == 0 {
		return
	}
	c.send(javaproto.BatchStart())
	for _, p := range c.pending {
		c.send(p.frame)
	}
	c.send(javaproto.BatchFinished(int32(len(c.pending))))
	c.openBatches.Add(1)
	clear(c.pending)
	c.pending = c.pending[:0]
}

// chunkSent reports whether the chunk was sent to the client.
func (c *conn) chunkSent(pos world.ChunkPos) bool {
	c.cmu.Lock()
	defer c.cmu.Unlock()
	_, ok := c.sent[pos]
	return ok
}

// flushEdits sends this tick's block changes, one to a block at most: a single
// change alone, several in a section together. A chunk with very many is marked
// to be sent again whole instead, paced like any other chunk, so a big edit
// costs a viewer one chunk packet per chunk, spread over ticks. Edits to a chunk
// waiting for that are dropped: the chunk will carry them.
func (c *conn) flushEdits() {
	if len(c.edits) == 0 {
		return
	}
	type sectionKey struct{ x, y, z int32 }
	sections := map[sectionKey][]cube.Pos{}
	perChunk := map[world.ChunkPos]int{}
	for pos := range c.edits {
		k := sectionKey{int32(pos[0] >> 4), int32(pos[1] >> 4), int32(pos[2] >> 4)}
		sections[k] = append(sections[k], pos)
		perChunk[world.ChunkPos{k.x, k.z}]++
	}
	for cp, n := range perChunk {
		if n > maxSectionEditsPerChunk {
			c.dirty[cp] = struct{}{}
		}
	}
	for k, list := range sections {
		if _, later := c.dirty[world.ChunkPos{k.x, k.z}]; later {
			continue
		}
		records := make([]int64, 0, len(list))
		var entities [][]byte // block entity data, which follows the blocks
		for _, pos := range list {
			if s, ok := c.editState(pos); ok {
				records = append(records, int64(s)<<12|int64(pos[0]&15)<<8|int64(pos[2]&15)<<4|int64(pos[1]&15))
			}
			if e, ok := c.editBlockEntity(pos); ok {
				entities = append(entities, e)
			}
		}
		switch len(records) {
		case 0:
		case 1:
			r := records[0]
			x, y, z := int(k.x)<<4|int(r>>8&15), int(k.y)<<4|int(r&15), int(k.z)<<4|int(r>>4&15)
			c.send(javaproto.BlockChange(x, y, z, int32(r>>12)))
		default:
			c.send(javaproto.SectionChange(k.x, k.y, k.z, records))
		}
		for _, e := range entities {
			c.send(e)
		}
	}
	clear(c.edits)
}

// forgetFarChunks tells the client about chunks the loader has dropped, by the
// loader's own distance rule. The Bedrock client works this out for itself. A
// chunk still waiting to be sent is simply not sent.
func (c *conn) forgetFarChunks() {
	r := float64(c.f.opts.ViewDistance)
	far := func(pos world.ChunkPos) bool {
		dx, dz := float64(pos[0]-c.center[0]), float64(pos[1]-c.center[1])
		return math.Round(math.Sqrt(dx*dx+dz*dz)) > r
	}
	unsent := map[world.ChunkPos]bool{}
	c.cmu.Lock()
	defer c.cmu.Unlock()
	c.pending = slices.DeleteFunc(c.pending, func(p pendingChunk) bool {
		if far(p.pos) {
			unsent[p.pos] = true
			return true
		}
		return false
	})
	for pos := range c.dirty {
		if far(pos) {
			delete(c.dirty, pos)
		}
	}
	for pos := range c.sent {
		if far(pos) {
			delete(c.sent, pos)
			if !unsent[pos] {
				c.send(javaproto.UnloadChunk(pos[0], pos[1]))
			}
		}
	}
}

func (c *conn) keepAlive(p *player.Player) {
	now := time.Now()
	if waiting := c.aliveSent.Load(); waiting != 0 {
		if now.Sub(time.Unix(0, waiting)) > keepAliveTimeout {
			p.Disconnect("Timed out.")
		}
		return
	}
	if now.Sub(c.lastPing) < keepAliveEvery {
		return
	}
	c.lastPing = now
	c.aliveSent.Store(now.UnixNano())
	c.send(javaproto.KeepAlive(now.UnixNano()))
}

// errKick is a protocol error the client is told about.
type errKick struct{ msg string }

func (e errKick) Error() string { return e.msg }

// readLoop handles packets until the connection ends or a packet is bad.
func (c *conn) readLoop() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = errKick{fmt.Sprintf("Bad packet (%v)", r)}
		}
	}()
	limit := rateLimit{tokens: packetBurst, last: time.Now()}
	for {
		frame, err := c.wc.ReadFrame()
		if err != nil {
			return err
		}
		if !limit.allow(time.Now()) {
			return errKick{"Too many packets."}
		}
		r := javaproto.NewReader(frame)
		pid := r.VarInt()
		if r.Err() != nil {
			return errKick{"Bad packet."}
		}
		if err := c.handlePacket(pid, r); err != nil {
			return err
		}
	}
}

// rateLimit is a token bucket for packets from one client.
type rateLimit struct {
	tokens float64
	last   time.Time
}

func (l *rateLimit) allow(now time.Time) bool {
	l.tokens = min(packetBurst, l.tokens+now.Sub(l.last).Seconds()*packetRate)
	l.last = now
	if l.tokens < 1 {
		return false
	}
	l.tokens--
	return true
}

// handlePacket handles one packet. Unknown packet IDs and malformed bodies of the
// packets we read are errors; valid packets we do not use yet are skipped.
func (c *conn) handlePacket(pid int32, r *javaproto.Reader) error {
	switch pid {
	case javaproto.PlayTeleportConfirm:
		tid := r.VarInt()
		if r.Done() != nil {
			return errKick{"Bad teleport confirm."}
		}
		c.teleportWait.CompareAndSwap(tid, 0)
	case javaproto.PlayKeepAliveIn:
		id, err := javaproto.ReadLong(r)
		if err != nil || r.Done() != nil {
			return errKick{"Bad keep-alive."}
		}
		if c.aliveSent.CompareAndSwap(id, 0) {
			c.latency.Store(int64(time.Since(time.Unix(0, id))) / 2)
		}
	case javaproto.PlayBatchReceived:
		r.Float()
		if r.Done() != nil {
			return errKick{"Bad chunk acknowledgement."}
		}
		c.ackBatch()
	case javaproto.PlayPosition:
		return c.move(r, true, false)
	case javaproto.PlayPositionLook:
		return c.move(r, true, true)
	case javaproto.PlayLook:
		return c.move(r, false, true)
	case javaproto.PlayDig:
		return c.dig(r)
	case javaproto.PlayPlayerInput:
		keys, err := javaproto.ReadPlayerInput(r)
		if err != nil {
			return errKick{"Bad input."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.input(p, keys) })
	case javaproto.PlayEntityAction:
		a, err := javaproto.ReadEntityAction(r)
		if err != nil {
			return errKick{"Bad player command."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.command(p, a) })
	case javaproto.PlaySettings:
		st, err := javaproto.ReadClientSettings(r)
		if err != nil {
			return errKick{"Bad settings."}
		}
		return c.onPlayer(func(tx *world.Tx, p *player.Player) { c.settings(tx, p, st) })
	case javaproto.PlayAttack:
		target, err := javaproto.ReadAttack(r)
		if err != nil {
			return errKick{"Bad attack."}
		}
		return c.onPlayer(func(tx *world.Tx, p *player.Player) { c.attack(tx, p, target) })
	case javaproto.PlayUseEntity:
		u, err := javaproto.ReadUseEntity(r)
		if err != nil || u.Hand < javaproto.HandMain || u.Hand > javaproto.HandOff {
			return errKick{"Bad use of an entity."}
		}
		return c.onPlayer(func(tx *world.Tx, p *player.Player) { c.interact(tx, p, u) })
	case javaproto.PlayArmAnimation:
		if _, err := javaproto.ReadArmAnimation(r); err != nil {
			return errKick{"Bad swing."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.swing(p) })
	case javaproto.PlayBlockPlace:
		u, err := javaproto.ReadBlockPlace(r)
		if err != nil || u.Face < 0 || u.Face > 5 || u.Hand < javaproto.HandMain || u.Hand > javaproto.HandOff {
			return errKick{"Bad use of an item."}
		}
		if !validCursor(u) {
			// As vanilla: a click outside its block is ignored.
			return c.onPlayer(func(_ *world.Tx, _ *player.Player) {
				c.resendHand(u.Hand) // the client took one for its guess
				c.send(javaproto.AckDig(u.Sequence))
			})
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.useOn(p, u) })
	case javaproto.PlayUseItem:
		u, err := javaproto.ReadUseItem(r)
		if err != nil || u.Hand < javaproto.HandMain || u.Hand > javaproto.HandOff || !finite(float64(u.Yaw), float64(u.Pitch)) {
			return errKick{"Bad use of an item."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.useInAir(p, u) })
	case javaproto.PlayChatMessage:
		msg, err := javaproto.ReadChatMessage(r)
		if err != nil {
			return errKick{"Bad chat message."}
		}
		if !allowedInChat(msg) {
			return errKick{"Illegal characters in chat."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { p.Chat(msg) })
	case javaproto.PlayChatCommand, javaproto.PlayChatCommandSigned:
		read := javaproto.ReadChatCommand
		if pid == javaproto.PlayChatCommandSigned {
			read = javaproto.ReadChatCommandSigned
		}
		command, err := read(r)
		if err != nil {
			return errKick{"Bad command."}
		}
		if !allowedInChat(command) {
			return errKick{"Illegal characters in chat."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { p.ExecuteCommand("/" + command) })
	case javaproto.PlayHeldSlotIn:
		slot, err := javaproto.ReadHeldSlot(r)
		if err != nil {
			return errKick{"Bad held slot."}
		}
		// Out of the hotbar is ignored, as vanilla does. A change of item starts
		// the attack cooldown again.
		return c.onPlayer(func(_ *world.Tx, p *player.Player) {
			changed := c.invs.held == nil || int(*c.invs.held) != int(slot)
			if p.SetHeldSlot(int(slot)) == nil && changed {
				c.attackReset = time.Now()
			}
		})
	case javaproto.PlayWindowClick:
		k, err := javaproto.ReadClick(r)
		if err != nil {
			return errKick{"Bad window click."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.click(p, k) })
	case javaproto.PlayCreativeSlot:
		ci, err := javaproto.ReadCreativeSlot(r)
		if err != nil {
			return errKick{"Bad creative slot."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.setCreative(p, ci) })
	case javaproto.PlayCloseWindow:
		id := r.VarInt()
		if r.Done() != nil {
			return errKick{"Bad close window."}
		}
		// As on Java: what was on the cursor and in a crafting grid goes back
		// to the inventory, and the block is told its viewer has gone.
		return c.onPlayer(func(tx *world.Tx, p *player.Player) { c.closeWindowIn(tx, p, id) })
	case javaproto.PlayUpdateSign:
		u, err := javaproto.ReadUpdateSign(r)
		if err != nil {
			return errKick{"Bad sign."}
		}
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.editSign(p, u) })
	case javaproto.PlayCraftRecipeAsk:
		// The recipe book's "fill the grid": optional, as a grid filled by
		// hand crafts the same.
		if javaproto.ReadCraftRecipeRequest(r) != nil {
			return errKick{"Bad recipe request."}
		}
	case javaproto.PlayClientCommand:
		action := r.VarInt()
		if r.Done() != nil {
			return errKick{"Bad client command."}
		}
		if action == javaproto.CommandRespawn {
			return c.onPlayer(func(_ *world.Tx, p *player.Player) { p.Respawn() })
		}
	default:
		if !knownPacket[pid] {
			return errKick{"Unknown packet."}
		}
	}
	return nil
}

// allowedInChat applies Java's rule for what a player may type: no section sign,
// no control characters and no DEL. A vanilla server disconnects a client that
// sends one, and so do we. The length limit is the reader's.
func allowedInChat(s string) bool {
	for _, r := range s {
		if r == '§' || r < ' ' || r == 0x7F {
			return false
		}
	}
	return true
}

// finite reports whether every value is a number and not infinite.
func finite(vs ...float64) bool {
	for _, v := range vs {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return false
		}
	}
	return true
}

// ackBatch counts one acknowledged chunk batch. A client cannot acknowledge
// more than it was sent, so it cannot earn chunks ahead of time.
func (c *conn) ackBatch() {
	for {
		open := c.openBatches.Load()
		if open <= 0 || c.openBatches.CompareAndSwap(open, open-1) {
			return
		}
	}
}

// move applies a client's movement. The client decides where it is, as a
// Bedrock client does; the player works out on-ground and falling from that.
func (c *conn) move(r *javaproto.Reader, hasPos, hasLook bool) error {
	m, err := javaproto.ReadMovement(r, hasPos, hasLook)
	if err != nil {
		return errKick{"Bad movement."}
	}
	if !finite(m.X, m.Y, m.Z, float64(m.Yaw), float64(m.Pitch)) {
		return errKick{"Bad movement."}
	}
	if math.Abs(m.X) > coordinateLimit || math.Abs(m.Z) > coordinateLimit || math.Abs(m.Y) > coordinateLimit {
		return errKick{"Bad movement."}
	}
	if c.teleportWait.Load() != 0 {
		// The client has not seen our teleport, so this is from before it.
		return nil
	}
	return c.onPlayer(func(_ *world.Tx, p *player.Player) {
		pos := p.Position()
		delta := mgl64.Vec3{}
		if hasPos {
			delta = mgl64.Vec3{m.X, m.Y, m.Z}.Sub(pos)
			if delta.Len() > maxMovePerPacket && !p.Dead() {
				// As vanilla: send it back to where the server has it.
				p.Teleport(pos)
				return
			}
		}
		yaw, pitch := p.Rotation().Elem()
		dYaw, dPitch := 0.0, 0.0
		if hasLook {
			newYaw, newPitch := javaLook(float64(m.Yaw), float64(m.Pitch))
			dYaw, dPitch = newYaw-yaw, newPitch-pitch
		}
		p.Move(delta, dYaw, dPitch)
	})
}

// dig turns a Player Action into breaking, or into the item keys: drop the
// held stack (Ctrl+Q), drop one (Q) and swap hands (F). Stopping using an item
// comes with item use.
func (c *conn) dig(r *javaproto.Reader) error {
	d, err := javaproto.ReadDig(r)
	if err != nil || d.Face > 5 {
		return errKick{"Bad player action."}
	}
	const (
		startDig  = 0
		cancelDig = 1
		finishDig = 2
		dropStack = 3
		dropOne   = 4
		release   = 5
		swapHands = 6
	)
	switch d.Status {
	case release:
		// A bow shoots; food not yet eaten is put away.
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { p.ReleaseItem() })
	case dropStack, dropOne:
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.dropHeld(p, d.Status == dropStack) })
	case swapHands:
		return c.onPlayer(func(_ *world.Tx, p *player.Player) { c.swapHands(p) })
	}
	if d.Status > finishDig || d.Status < startDig {
		return nil
	}
	pos, face := cube.Pos{d.X, d.Y, d.Z}, cube.Face(d.Face)
	return c.onPlayer(func(_ *world.Tx, p *player.Player) {
		// Only blocks the client was sent: looking one up would load its chunk.
		if !c.chunkSent(world.ChunkPos{int32(d.X >> 4), int32(d.Z >> 4)}) {
			c.send(javaproto.AckDig(d.Sequence))
			return
		}
		switch d.Status {
		case startDig:
			// The player's own break time: tool, haste, in water, in the air.
			instant := p.GameMode().CreativeInventory() || p.BreakDuration(pos) <= tick
			p.StartBreaking(pos, face)
			if instant {
				// A client that breaks this at once sends nothing more.
				p.FinishBreaking()
			}
		case cancelDig:
			p.AbortBreaking()
		case finishDig:
			p.FinishBreaking()
		}
		// The client takes back what it predicted when acknowledged, so the
		// true block must already be there.
		c.flushWorld()
		c.send(javaproto.AckDig(d.Sequence))
	})
}

// knownPacket holds every play packet ID a 26.1 client can send.
var knownPacket = func() map[int32]bool {
	m := map[int32]bool{}
	for id := int32(0); id < 256; id++ {
		if java261.PacketName("play", "toServer", id) != "" {
			m[id] = true
		}
	}
	return m
}()

var errGone = errors.New("javafront: player gone")

// onPlayer runs f on the player's world goroutine and waits.
func (c *conn) onPlayer(f func(tx *world.Tx, p *player.Player)) error {
	_, err := player.Call(c.ctx, c.handle, func(tx *world.Tx, p *player.Player) (struct{}, error) {
		f(tx, p)
		return struct{}{}, nil
	})
	if err != nil {
		return errGone
	}
	return nil
}

// leave ends a player whose connection ended or misbehaved. Close of the
// player leads back to Reporter.Close below.
func (c *conn) leave(err error) {
	var kick errKick
	msg := ""
	if errors.As(err, &kick) {
		msg = kick.msg
		c.f.opts.Log.Debug("java packet refused", "name", c.id.Name, "reason", msg)
	}
	// A background context: c.ctx may be cancelled already, and the player must
	// still leave.
	_, callErr := player.Call(context.Background(), c.handle, func(_ *world.Tx, p *player.Player) (struct{}, error) {
		if msg != "" {
			p.Disconnect(msg)
		} else {
			_ = p.Close()
		}
		return struct{}{}, nil
	})
	if callErr != nil {
		// The player can no longer be reached (its world closed): clean up here.
		c.Close(nil, nil)
	}
	c.CloseConnection()
}

// shutdown makes the player leave because the server is closing. Its readLoop
// then ends, since the connection closes.
func (c *conn) shutdown(msg string) {
	if !c.visible.Load() {
		// Still on its way in. Its reader may be waiting on the socket: free it
		// now, so the join ends at once and a newcomer is not kept waiting.
		c.CloseConnection()
		_ = c.nc.SetReadDeadline(time.Now())
		return
	}
	player.Do(c.handle, func(_ *world.Tx, p *player.Player) { p.Disconnect(msg) })
	select {
	case <-c.left:
	case <-time.After(5 * time.Second):
		c.CloseConnection()
	}
}

// --- player.Reporter ---

// Addr ...
func (c *conn) Addr() net.Addr { return c.nc.RemoteAddr() }

// Latency ...
func (c *conn) Latency() time.Duration { return time.Duration(c.latency.Load()) }

// ChunkRadius ...
func (c *conn) ChunkRadius() int32 { return int32(c.f.opts.ViewDistance) }

// Disconnect tells the client why it is being disconnected. Closing follows.
func (c *conn) Disconnect(msg string) { c.send(javaproto.Disconnect(msg)) }

// Close takes the player out of the world, once. tx is nil when the player is
// in no world any more.
func (c *conn) Close(tx *world.Tx, ctrl session.Controllable) {
	c.closeOnce.Do(func() {
		defer close(c.left)
		defer c.f.release(c.id.Name, c)
		chat.Global.Unsubscribe(subscriberID(c.id.UUID))
		if tx != nil {
			c.closeWindow(tx, false)
			ctrl.MoveItemsToInventory()
			// As a Bedrock session does, the data is saved on the world's
			// goroutine, before the player leaves it.
			if err := c.f.players().Save(c.id.UUID, ctrl.(*player.Player).Data(), tx.World()); err != nil {
				c.f.opts.Log.Error("save java player data", "name", c.id.Name, "err", err)
			}
			if c.loader != nil {
				c.loader.Close(tx)
			}
			tx.RemoveEntity(ctrl)
			_ = c.handle.Close()
		} else if c.joined {
			c.f.opts.Log.Error("java player's world closed before it left; data not saved", "name", c.id.Name)
		}
		if c.joined && c.f.opts.Left != nil {
			c.f.opts.Left(c.id.UUID)
		}
	})
}

// CloseConnection ends the connection once what is queued has been sent.
func (c *conn) CloseConnection() {
	c.stopOnce.Do(func() {
		close(c.done)
		c.cancel()
		// Unblock a reader or writer stuck on the socket. The writer may just have
		// set a later write deadline, so closing is the backstop. It waits for the
		// whole orderly close, the drain and hangUp's wait for the client, both
		// drainLimit, so it never cuts off a disconnect message on its way.
		_ = c.nc.SetDeadline(time.Now().Add(drainLimit))
		time.AfterFunc(2*drainLimit+time.Second, func() { _ = c.wc.Close() })
	})
}

// SendHealth shows the health bar. At zero the client shows its death screen and
// later asks to respawn.
func (c *conn) SendHealth(health, _, _ float64) {
	c.health = float32(health)
	c.sendHealth()
}

// SendFood shows the hunger bar.
func (c *conn) SendFood(food int, saturation, _ float64) {
	c.food, c.foodSat = int32(food), float32(saturation)
	c.sendHealth()
}

func (c *conn) sendHealth() { c.send(javaproto.Health(c.health, c.food, c.foodSat)) }

// SendRespawn brings the client back from its death screen. Respawn makes it a
// new player on the client, so it needs its position again after this.
func (c *conn) SendRespawn(pos mgl64.Vec3, ctrl session.Controllable) {
	c.send(javaproto.Respawn(c.loginInfo(gameModeID(ctrl.GameMode())), 0))
	c.send(javaproto.GameEvent(javaproto.EventStartWaitingForChunks, 0))
	c.teleportSelf(pos, ctrl.Rotation())
	c.sendHealth()
	c.sendInventory() // the client starts the new player with nothing
	// A new level forgets what it was shown: the next tick shows it again.
	c.reshow = true
}

// --- world.Viewer ---

// viewing reports whether a world's callback may touch the conn: the player is
// in the world the conn views and no change of world is under way. A world
// calls its viewers on its own goroutine, and the conn's state belongs to the
// goroutine of the player's world. A world the player has left still calls
// until its goroutine has run the loader's removal from its chunks (Loader.
// ChangeWorld queues it there), and those calls are dropped: they are of the
// old world, and the client is sent the new one whole (arrive).
//
// It takes no lock. A call that passes before the player leaves a world runs
// on that world's goroutine, which is the one that removes the player, so it
// ends before the player can be anywhere else. Every Viewer method that
// touches the conn checks it, except ViewChunk: the loader passes on only
// chunks of the world it is in, and the chunk state has its own lock (cmu).
func (c *conn) viewing() bool {
	w := c.viewW.Load()
	if w == nil || c.handle == nil || c.handle.World() != w {
		return false
	}
	// leaveWorld sets away before viewW: a world left behind that sees the new
	// viewW sees away as well.
	return !c.away.Load()
}

// ViewChunk encodes the chunk now, since the chunk changes after this call, and
// sends it with this tick's batch.
func (c *conn) ViewChunk(pos world.ChunkPos, dim world.Dimension, blockEntities map[cube.Pos]world.Block, ch *chunk.Chunk) {
	frame := encodeChunk(pos[0], pos[1], ch, blockEntities, dimensionOf(dim))
	c.cmu.Lock()
	defer c.cmu.Unlock()
	c.pending = append(c.pending, pendingChunk{pos, frame})
	c.sent[pos] = struct{}{}
}

// ViewBlockUpdate notes a changed block, on either layer, for the end of the
// tick, after any chunk already encoded. Its Java state is worked out then from
// the chunk, which holds both layers and the block entity: water on the second
// layer makes the block waterlogged, and a chest's pair is in its entity.
func (c *conn) ViewBlockUpdate(pos cube.Pos, b world.Block, layer int) {
	if c.viewing() {
		c.blockUpdate(pos, b, layer)
	}
}

func (c *conn) blockUpdate(pos cube.Pos, b world.Block, layer int) {
	if c.sign != nil && c.sign.pos == pos {
		c.sign = nil // the sign the client was asked to edit is not that sign any more
	}
	if layer == 0 {
		c.edits[pos] = b
	} else if _, ok := c.edits[pos]; !ok {
		c.edits[pos] = nil
	}
}

// editState is the Java state of an edited block now, from the loader's
// chunk. Without the chunk (a conn with no loader yet) it is the first layer's
// block as the update gave it; false if there is none.
func (c *conn) editState(pos cube.Pos) (int32, bool) {
	if c.loader != nil {
		if col, ok := c.loader.Chunk(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)}); ok {
			return columnState(col, pos), true
		}
	}
	b := c.edits[pos]
	if b == nil {
		return 0, false
	}
	s, _ := stateOf(pos, world.BlockRuntimeID(b), airRID(), b)
	return s, true
}

// ViewEntity shows another player, an item entity, or any other entity Java
// has a counterpart for (others.go).
func (c *conn) ViewEntity(e world.Entity) {
	if !c.viewing() {
		return
	}
	if e.H() == c.handle {
		return
	}
	if _, already := c.ents[e.H()]; already {
		return
	}
	if it, ok := e.(*entity.Ent); ok && e.H().Type() == entity.ItemType {
		c.showItem(it)
		return
	}
	p, ok := e.(*player.Player)
	if !ok {
		c.showEntity(e)
		return
	}
	id := c.nextEnt
	c.nextEnt++
	pos := e.Position()
	rot := e.Rotation()
	// A Bedrock player is known to Java by a UUID of its own, which picks its
	// default skin, and may carry signed textures (bedrockskins.go).
	look := c.f.lookOf(p)
	u := look.uuid
	c.ents[e.H()] = &shown{id: id, uuid: u, account: p.UUID(), look: look, player: true, health: float32(p.Health()), yaw: rot.Yaw(), x: fixed(pos[0]), y: fixed(pos[1]), z: fixed(pos[2])}
	// The client must know a player before it can show one. Usually the shared
	// list has it already; a player seen before it is listed gets a row now.
	c.showInfo(c.infoFor(p, look))
	c.send(javaproto.SpawnEntity(id, u, playerEntity, pos[0], pos[1], pos[2], rot.Yaw(), rot.Pitch(), [3]float64{}))
	c.send(javaproto.EntityMetadata(id, c.playerMeta(p)))
}

// HideEntity stops showing an entity.
func (c *conn) HideEntity(e world.Entity) {
	if !c.viewing() {
		return
	}
	s, ok := c.ents[e.H()]
	if !ok {
		return
	}
	delete(c.ents, e.H())
	c.send(javaproto.EntitiesRemoved(s.id))
	if s.player && !c.f.listed(s.account) {
		c.hideInfo(s.uuid) // a row that only came with the entity
	}
}

// showInfo adds a row to the client's player list, once.
func (c *conn) showInfo(e javaproto.PlayerInfoEntry) {
	c.lmu.Lock()
	defer c.lmu.Unlock()
	if _, ok := c.rows[e.UUID]; !c.tabOpen || ok {
		return
	}
	c.rows[e.UUID] = e
	c.send(javaproto.PlayerInfoAdd(e))
}

// hideInfo takes a row off the client's player list.
func (c *conn) hideInfo(id uuid.UUID) {
	c.lmu.Lock()
	defer c.lmu.Unlock()
	if _, ok := c.rows[id]; !ok {
		return
	}
	delete(c.rows, id)
	c.send(javaproto.PlayerRemove(id))
}

// SendMessage shows a server message as system chat. Dragonfly's colour codes
// stay in the text, and the Java client draws them as Bedrock does.
func (c *conn) SendMessage(msg string) {
	c.send(javaproto.SystemChat(javaproto.Text{Text: msg}))
}

// SendTranslation shows a translated message as system chat. Messages Java has
// in its own language files go as a translation the client renders itself;
// others as their English text.
func (c *conn) SendTranslation(t chat.Translation, l language.Tag, a []any) {
	c.send(javaproto.SystemChat(javaTranslation(t, l, a)))
}

// PlayerChat shows a player's chat message, from either edition, as Java shows
// player chat: unsigned (profileless) chat of the minecraft:chat type, which
// the client draws as "<name> message". Nothing is signed, so nothing can fail
// to verify, whether the sender logged in with Mojang or not.
func (c *conn) PlayerChat(sender, msg string) {
	c.send(javaproto.DisguisedChat(javaproto.Text{Text: msg}, chatType(), javaproto.Text{Text: sender}))
}

// SendCommandOutput shows what a command said as system chat, errors in red.
func (c *conn) SendCommandOutput(o *cmd.Output, _ language.Tag) {
	for _, m := range o.Messages() {
		c.send(javaproto.SystemChat(javaproto.Text{Text: m.String()}))
	}
	for _, e := range o.Errors() {
		c.send(javaproto.SystemChat(javaproto.Text{Text: e.Error(), Color: "red"}))
	}
}

// javaTranslations are Dragonfly messages Java has under its own key, each
// taking one argument.
var javaTranslations = []struct {
	t     chat.Translation
	key   string
	color string
}{
	{chat.MessageJoin, "multiplayer.player.joined", "yellow"},
	{chat.MessageQuit, "multiplayer.player.left", "yellow"},
}

func javaTranslation(t chat.Translation, l language.Tag, a []any) javaproto.Text {
	tr := t.F(a...)
	for _, j := range javaTranslations {
		// A translation's key resolves the same whatever its arguments.
		if len(a) != 1 || tr.Resolve(l) != j.t.F(a[0]).Resolve(l) {
			continue
		}
		var with []javaproto.Text
		for _, p := range tr.Params(l) {
			with = append(with, javaproto.Text{Text: p})
		}
		return javaproto.Text{Translate: j.key, Color: j.color, With: with}
	}
	return javaproto.Text{Text: tr.String()}
}

// subscriberID stands for a player in chat.Global, which knows subscribers by
// UUID, when only the UUID is left.
type subscriberID uuid.UUID

func (s subscriberID) UUID() uuid.UUID { return uuid.UUID(s) }
func (subscriberID) Message(...any)    {}

// ViewEntityMovement moves a shown entity.
func (c *conn) ViewEntityMovement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if !c.viewing() {
		return
	}
	c.moveShown(e, pos, rot, onGround, false)
}

// ViewEntityDisplacement moves any entity the server moved, the client's own
// player included.
func (c *conn) ViewEntityDisplacement(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround bool) {
	if !c.viewing() {
		return
	}
	if e.H() == c.handle {
		c.teleportSelf(pos, rot)
		return
	}
	c.moveShown(e, pos, rot, onGround, true)
}

// ViewEntityTeleport teleports an entity, the client's own player included.
func (c *conn) ViewEntityTeleport(e world.Entity, pos mgl64.Vec3) {
	if !c.viewing() {
		return
	}
	if e.H() == c.handle {
		c.teleportSelf(pos, e.Rotation())
		return
	}
	c.moveShown(e, pos, e.Rotation(), false, true)
}

func (c *conn) teleportSelf(pos mgl64.Vec3, rot cube.Rotation) {
	t := c.teleportSeq.Add(1)
	c.teleportWait.Store(t)
	c.send(javaproto.PlayerPosition(t, pos[0], pos[1], pos[2], float32(rot.Yaw()), float32(rot.Pitch())))
}

func (c *conn) moveShown(e world.Entity, pos mgl64.Vec3, rot cube.Rotation, onGround, absolute bool) {
	s, ok := c.ents[e.H()]
	if !ok {
		return
	}
	x, y, z := fixed(pos[0]), fixed(pos[1]), fixed(pos[2])
	dx, dy, dz := x-s.x, y-s.y, z-s.z
	if absolute || !fitsShort(dx) || !fitsShort(dy) || !fitsShort(dz) {
		c.send(javaproto.EntitySync(s.id, pos[0], pos[1], pos[2], rot.Yaw(), rot.Pitch(), onGround))
	} else {
		c.send(javaproto.EntityMoveLook(s.id, int16(dx), int16(dy), int16(dz), rot.Yaw(), rot.Pitch(), onGround))
	}
	s.x, s.y, s.z = x, y, z
	if rot.Yaw() != s.yaw {
		s.yaw = rot.Yaw()
		c.send(javaproto.EntityHeadYaw(s.id, rot.Yaw()))
	}
}

// javaName fits a name in the 16 characters a Java client accepts. Bedrock names
// can be longer, and a client disconnects on a longer one.
func javaName(name string) string {
	r := []rune(name)
	if len(r) > 16 {
		return string(r[:16])
	}
	return name
}

// fixed is a coordinate in the 1/4096 block units of entity deltas.
func fixed(v float64) int64 { return int64(math.Round(v * 4096)) }

func fitsShort(v int64) bool { return v >= math.MinInt16 && v <= math.MaxInt16 }

func chunkPosOf(pos mgl64.Vec3) world.ChunkPos {
	return world.ChunkPos{int32(math.Floor(pos[0])) >> 4, int32(math.Floor(pos[2])) >> 4}
}
