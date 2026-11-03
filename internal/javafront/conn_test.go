package javafront

import (
	"context"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/biome"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/df-mc/dragonfly/server/world/generator"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/internal/javalogin"
	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/javawire"
)

var registryOnce sync.Once

// finalizeRegistry does what starting a server does, once for the test binary.
func finalizeRegistry() {
	registryOnce.Do(func() {
		var conf server.Config
		_ = conf.New()
	})
}

// blockedConn models a peer whose TCP window is closed: Write returns only on
// Close, or at a deadline if honourDeadline is set.
type blockedConn struct {
	net.Conn
	closed         chan struct{}
	once           sync.Once
	writes         atomic.Int32
	honourDeadline bool
	deadline       atomic.Pointer[time.Time]
}

func newBlocked() *blockedConn { return &blockedConn{closed: make(chan struct{})} }

func (b *blockedConn) Write([]byte) (int, error) {
	b.writes.Add(1)
	for {
		select {
		case <-b.closed:
			return 0, net.ErrClosed
		case <-time.After(10 * time.Millisecond):
			if d := b.deadline.Load(); b.honourDeadline && d != nil && time.Now().After(*d) {
				return 0, context.DeadlineExceeded
			}
		}
	}
}
func (b *blockedConn) Read([]byte) (int, error) { <-b.closed; return 0, net.ErrClosed }
func (b *blockedConn) Close() error             { b.once.Do(func() { close(b.closed) }); return nil }
func (b *blockedConn) SetDeadline(t time.Time) error {
	return b.SetWriteDeadline(t)
}
func (b *blockedConn) SetWriteDeadline(t time.Time) error { b.deadline.Store(&t); return nil }
func (b *blockedConn) RemoteAddr() net.Addr               { return &net.TCPAddr{} }

// Bytes, not packets: big frames past the budget drop the client, and the writer
// comes back even though the socket never moves and ignores deadlines.
func TestOverflowDropsClientAndFreesWriter(t *testing.T) {
	bc := newBlocked()
	c := newConn(&Front{}, javawire.NewConn(bc), bc, nil)
	go c.writer()
	big := make([]byte, 1<<20)
	for i := 0; i < maxQueuedBytes>>20+2; i++ {
		c.send(big)
	}
	select {
	case <-c.done:
	default:
		t.Fatal("not dropped past the byte budget")
	}
	select {
	case <-c.writerEnd:
	case <-time.After(10 * time.Second):
		t.Fatal("writer still stuck 10 s after the drop")
	}
	select {
	case <-bc.closed:
	default:
		t.Fatal("socket never closed")
	}
}

// Small packets alone must not hit a limit meant for bytes.
func TestManySmallPacketsDoNotDrop(t *testing.T) {
	bc := newBlocked()
	c := newConn(&Front{}, javawire.NewConn(bc), bc, nil)
	defer c.CloseConnection()
	for i := 0; i < 5000; i++ {
		c.send(make([]byte, 30))
	}
	select {
	case <-c.done:
		t.Fatal("5000 small packets dropped the client")
	default:
	}
}

// No overflow needed: one frame stuck past its deadline ends the connection.
func TestStalledFrameEndsConnection(t *testing.T) {
	bc := newBlocked()
	bc.honourDeadline = true
	c := newConn(&Front{}, javawire.NewConn(bc), bc, nil)
	go c.writer()
	c.send(make([]byte, 100))
	select {
	case <-c.done:
	case <-time.After(writeLimit + 3*time.Second):
		t.Fatal("a stalled frame did not end the connection")
	}
}

func proofWorld(t *testing.T) *world.World {
	t.Helper()
	finalizeRegistry()
	w := world.Config{
		Entities:  entity.DefaultRegistry,
		Generator: generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Bedrock{}}),
		Dim:       world.Overworld,
	}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func testFront(t *testing.T, w *world.World, view int) *Front {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	return &Front{
		opts: Options{World: w, ViewDistance: view, Log: slog.New(slog.DiscardHandler)},
		l:    l, names: map[string]*conn{}, perAddr: map[string]int{}, raw: map[net.Conn]struct{}{},
	}
}

func testIdentity() *javalogin.Identity {
	return &javalogin.Identity{Name: "T", UUID: uuid.New()}
}

// A stalled client must not hold up closing, or the world never saves.
func TestFrontCloseWithStalledClient(t *testing.T) {
	f := testFront(t, proofWorld(t), 2)
	bc := newBlocked()
	c := newConn(f, javawire.NewConn(bc), bc, testIdentity())
	f.wg.Add(1)
	go func() { defer f.wg.Done(); c.run() }()
	require.Eventually(t, c.visible.Load, 5*time.Second, 10*time.Millisecond)

	closed := make(chan struct{})
	go func() { f.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(10 * time.Second):
		t.Fatal("Front.Close hung on a stalled client")
	}
	require.Zero(t, f.Count())
}

// A join cancelled before it reaches the world leaves nothing behind.
func TestCancelledJoinCleansUp(t *testing.T) {
	f := testFront(t, proofWorld(t), 2)
	a, b := net.Pipe()
	defer b.Close()
	go func() {
		wc := javawire.NewConn(b)
		for {
			if _, err := wc.ReadFrame(); err != nil {
				return
			}
		}
	}()
	c := newConn(f, javawire.NewConn(a), a, testIdentity())
	c.cancel() // as Front.Close does while the join waits for the world
	done := make(chan struct{})
	go func() { c.run(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("run never returned")
	}
	require.Zero(t, f.Count(), "the name stayed taken")
	require.False(t, c.visible.Load())
	require.True(t, c.handle.Closed(), "the handle was left open")
}

func TestAcknowledgementsCannotExceedBatches(t *testing.T) {
	bc := newBlocked()
	c := newConn(&Front{}, javawire.NewConn(bc), bc, nil)
	defer c.CloseConnection()
	for i := 0; i < 100; i++ {
		c.ackBatch()
	}
	require.Zero(t, c.openBatches.Load())
	c.pending = []pendingChunk{{frame: []byte{1}}}
	c.flushChunks()
	require.EqualValues(t, 1, c.openBatches.Load(), "a hundred early acknowledgements bought chunks")
}

func TestRateLimit(t *testing.T) {
	now := time.Now()
	l := rateLimit{tokens: packetBurst, last: now}
	allowed := 0
	for i := 0; i < int(packetBurst)*2; i++ {
		if l.allow(now) {
			allowed++
		}
	}
	require.Equal(t, int(packetBurst), allowed)
	require.True(t, l.allow(now.Add(time.Second)), "the bucket should fill again")
}

func TestJavaNameFits(t *testing.T) {
	require.Equal(t, "Steve", javaName("Steve"))
	require.Equal(t, "ABCDEFGHIJKLMNOP", javaName("ABCDEFGHIJKLMNOPQRST"))
	require.Len(t, []rune(javaName("ééééééééééééééééééééé")), 16)
}

// The centre must come before any chunk around it, or the client ignores the
// chunk and is left with a hole. This reads the raw frames, with no bot in the way.
func TestCenterChunkGoesBeforeChunks(t *testing.T) {
	w := proofWorld(t)
	const view = 2
	f := testFront(t, w, view)

	// Keep the far chunks loaded, so a Load delivers them within the same tick.
	far := mgl64.Vec3{200.5, -60, 0.5}
	_, err := world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		l := world.NewLoader(view+2, w, world.NopViewer{})
		l.Move(tx, far)
		l.Load(tx, 64)
		return struct{}{}, nil
	})
	require.NoError(t, err)
	time.Sleep(500 * time.Millisecond)

	a, b := net.Pipe()
	defer b.Close()
	c := newConn(f, javawire.NewConn(a), a, testIdentity())
	go c.run()
	defer c.CloseConnection()
	require.Eventually(t, c.visible.Load, 5*time.Second, 10*time.Millisecond)

	var center [2]int32
	var chunks atomic.Int32
	var violation atomic.Pointer[string]
	go func() {
		wc := javawire.NewConn(b)
		for {
			frame, err := wc.ReadFrame()
			if err != nil {
				return
			}
			r := javaproto.NewReader(frame)
			switch r.VarInt() {
			case javaproto.PlayCenterChunk:
				center = [2]int32{r.VarInt(), r.VarInt()}
			case javaproto.PlayChunk:
				x, z := r.Int(), r.Int()
				chunks.Add(1)
				if d := max(abs(x-center[0]), abs(z-center[1])); d > view {
					s := "a chunk came before its centre"
					violation.Store(&s)
				}
			}
		}
	}()
	require.Eventually(t, func() bool { return chunks.Load() > 3 }, 5*time.Second, 10*time.Millisecond)

	player.Do(c.handle, func(_ *world.Tx, p *player.Player) { p.Teleport(far) })
	want := chunks.Load() + 8
	require.Eventually(t, func() bool { return chunks.Load() > want }, 5*time.Second, 10*time.Millisecond, "no chunks after the teleport")
	if v := violation.Load(); v != nil {
		t.Fatal(*v)
	}
}

// countingGenerator records the chunks the world asked for.
type countingGenerator struct {
	world.Generator
	far atomic.Int32
}

func (g *countingGenerator) GenerateChunk(pos world.ChunkPos, c *chunk.Chunk) {
	if pos[0] > 1000 {
		g.far.Add(1)
	}
	g.Generator.GenerateChunk(pos, c)
}

// A dig is acknowledged and goes no further: it must not load the chunk.
func TestDigOutsideSentChunksLoadsNothing(t *testing.T) {
	finalizeRegistry()
	gen := &countingGenerator{Generator: generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Bedrock{}})}
	w := world.Config{Entities: entity.DefaultRegistry, Generator: gen, Dim: world.Overworld}.New()
	t.Cleanup(func() { _ = w.Close() })
	f := testFront(t, w, 2)
	a, b := net.Pipe()
	defer b.Close()
	c := newConn(f, javawire.NewConn(a), a, testIdentity())
	go c.run()
	defer c.CloseConnection()
	require.Eventually(t, c.visible.Load, 5*time.Second, 10*time.Millisecond)

	var acked atomic.Bool
	client := javawire.NewConn(b)
	go func() {
		for {
			frame, err := client.ReadFrame()
			if err != nil {
				return
			}
			if r := javaproto.NewReader(frame); r.VarInt() == javaproto.PlayAckDig {
				acked.Store(true)
			}
		}
	}()
	var w2 javaproto.Writer
	w2.VarInt(javaproto.PlayDig)
	w2.VarInt(0)
	w2.Position(2_000_000, 0, 2_000_000)
	w2.Byte(1)
	w2.VarInt(9)
	require.NoError(t, client.WriteFrame(w2.B))

	require.Eventually(t, acked.Load, 5*time.Second, 10*time.Millisecond, "the dig was not acknowledged")
	require.Zero(t, gen.far.Load(), "the dig made the world generate a far chunk")
}

// queuedIDs lists the packet IDs waiting in c's queue, in order.
func queuedIDs(c *conn) []int32 {
	c.qmu.Lock()
	defer c.qmu.Unlock()
	ids := make([]int32, 0, len(c.queue))
	for _, p := range c.queue {
		ids = append(ids, javaproto.NewReader(p).VarInt())
	}
	return ids
}

// A chunk encoded before an edit must reach the client first, or the chunk
// undoes the edit.
func TestChunkGoesBeforeItsEdits(t *testing.T) {
	finalizeRegistry()
	bc := newBlocked()
	c := newConn(&Front{opts: Options{ViewDistance: 2}}, javawire.NewConn(bc), bc, nil)
	defer c.CloseConnection()
	ch := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	c.ViewChunk(world.ChunkPos{0, 0}, world.Overworld, nil, ch)
	c.blockUpdate(cube.Pos{1, 2, 3}, block.Stone{}, 0) // the conn is in no world: past the viewing check
	c.flushWorld()

	ids := queuedIDs(c)
	chunkAt, editAt := slices.Index(ids, javaproto.PlayChunk), slices.Index(ids, javaproto.PlayBlockChange)
	require.NotEqual(t, -1, chunkAt)
	require.NotEqual(t, -1, editAt)
	require.Less(t, chunkAt, editAt, "the edit went before the chunk it belongs to")
}

// The client takes back its prediction at the acknowledgement, so the real
// block must come first.
func TestBlockChangeGoesBeforeDigAck(t *testing.T) {
	f := testFront(t, proofWorld(t), 2)
	a, b := net.Pipe()
	defer b.Close()
	c := newConn(f, javawire.NewConn(a), a, testIdentity())
	go c.run()
	defer c.CloseConnection()

	client := javawire.NewConn(b)
	target := cube.Pos{1, -62, 1} // the grass of proofWorld
	var mu sync.Mutex
	var order []string
	chunkSeen := make(chan struct{})
	var once sync.Once
	go func() {
		for {
			frame, err := client.ReadFrame()
			if err != nil {
				return
			}
			r := javaproto.NewReader(frame)
			switch r.VarInt() {
			case javaproto.PlayChunk:
				if r.Int() == 0 && r.Int() == 0 {
					once.Do(func() { close(chunkSeen) })
				}
			case javaproto.PlayBlockChange:
				if x, y, z := r.Position(); (cube.Pos{x, y, z}) == target {
					mu.Lock()
					order = append(order, "block")
					mu.Unlock()
				}
			case javaproto.PlayAckDig:
				if r.VarInt() == 6 {
					mu.Lock()
					order = append(order, "ack")
					mu.Unlock()
				}
			}
		}
	}()
	select {
	case <-chunkSeen:
	case <-time.After(5 * time.Second):
		t.Fatal("chunk (0,0) never sent")
	}
	dig := func(status, seq int32) {
		var w javaproto.Writer
		w.VarInt(javaproto.PlayDig)
		w.VarInt(status)
		w.Position(target[0], target[1], target[2])
		w.Byte(1)
		w.VarInt(seq)
		require.NoError(t, client.WriteFrame(w.B))
	}
	dig(0, 5)
	time.Sleep(100 * time.Millisecond)
	dig(2, 6)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(order, "ack") && slices.Contains(order, "block")
	}, 5*time.Second, 10*time.Millisecond, "no block change or no acknowledgement")
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "block", order[0], "the acknowledgement came before the block: %v", order)
}

// A newcomer with the same account must not be refused while the old session is
// still on its way in, its reader waiting on the socket.
func TestRejoinWhileOldSessionStillJoining(t *testing.T) {
	f := testFront(t, proofWorld(t), 2)
	id := testIdentity()
	a, b := net.Pipe()
	defer b.Close()
	old := newConn(f, javawire.NewConn(a), a, id)
	f.names[strings.ToLower(id.Name)] = old
	go func() {
		_, _ = old.wc.ReadFrame() // as readLoop, until something unblocks it
		close(old.left)
	}()

	a2, b2 := net.Pipe()
	defer b2.Close()
	fresh := newConn(f, javawire.NewConn(a2), a2, &javalogin.Identity{Name: id.Name, UUID: id.UUID})
	start := time.Now()
	msg := f.claim(id.Name, fresh)
	require.Empty(t, msg, "the newcomer was refused")
	require.Less(t, time.Since(start), time.Second, "the newcomer waited on the old session's socket")
}

func abs(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

func BenchmarkEncodeChunk(b *testing.B) {
	finalizeRegistry()
	flat := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	generator.NewFlat(biome.Plains{}, []world.Block{block.Grass{}, block.Dirt{}, block.Dirt{}, block.Bedrock{}}).GenerateChunk(world.ChunkPos{}, flat)

	mixed := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
	kinds := []world.Block{block.Stone{}, block.Dirt{}, block.Grass{}, block.Andesite{}, block.Diorite{}, block.Granite{}, block.Bedrock{}, block.Gravel{}}
	for y := int16(-64); y < 64; y++ {
		for z := uint8(0); z < 16; z++ {
			for x := uint8(0); x < 16; x++ {
				mixed.SetBlock(x, y, z, 0, world.BlockRuntimeID(kinds[(int(x)+int(z)*3+int(y)*5+640)%len(kinds)]))
				mixed.SetBiome(x, y, z, uint32(biome.Plains{}.EncodeBiome()))
			}
		}
	}
	for name, c := range map[string]*chunk.Chunk{"flat": flat, "mixed": mixed} {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				encodeChunk(0, 0, c, nil, overworldDim)
			}
		})
	}
}

// A Bedrock player on the way holds a slot against Java too.
func TestClaimCountsPlayersOnTheWay(t *testing.T) {
	f := testFront(t, proofWorld(t), 2)
	f.opts.MaxPlayers = 2
	f.opts.OtherPlayerCount = func() int { return 1 } // one Bedrock player joining
	a, b := net.Pipe()
	defer b.Close()
	first := newConn(f, javawire.NewConn(a), a, &javalogin.Identity{Name: "One", UUID: uuid.New()})
	require.Empty(t, f.claim("One", first), "the last slot was refused")
	second := newConn(f, javawire.NewConn(a), a, &javalogin.Identity{Name: "Two", UUID: uuid.New()})
	require.Equal(t, "The server is full.", f.claim("Two", second))
	require.Equal(t, 1, f.Count(), "a refused join kept its slot")
}
