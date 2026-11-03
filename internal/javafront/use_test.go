package javafront

import (
	"math"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/javawire"
)

// A placed block reaches the client before the acknowledgement of the place,
// which is when the client takes back its own guess.
func TestPlacedBlockGoesBeforeAck(t *testing.T) {
	f := testFront(t, proofWorld(t), 2)
	a, b := net.Pipe()
	defer b.Close()
	c := newConn(f, javawire.NewConn(a), a, testIdentity())
	go c.run()
	defer c.CloseConnection()

	client := javawire.NewConn(b)
	on := cube.Pos{1, -62, 1} // the grass of proofWorld
	target := on.Side(cube.FaceUp)
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
				if r.VarInt() == 3 {
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
	done := make(chan struct{})
	player.Do(c.handle, func(_ *world.Tx, p *player.Player) {
		p.SetGameMode(world.GameModeSurvival)
		_ = p.Inventory().SetItem(0, item.NewStack(block.Stone{}, 2))
		close(done)
	})
	<-done

	var w javaproto.Writer
	w.VarInt(javaproto.PlayBlockPlace)
	w.VarInt(javaproto.HandMain)
	w.Position(on[0], on[1], on[2])
	w.VarInt(int32(cube.FaceUp))
	w.Float(0.5)
	w.Float(1)
	w.Float(0.5)
	w.Bool(false)
	w.Bool(false)
	w.VarInt(3)
	require.NoError(t, client.WriteFrame(w.B))

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return slices.Contains(order, "ack") && slices.Contains(order, "block")
	}, 5*time.Second, 10*time.Millisecond, "no block change or no acknowledgement: %v", order)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, "block", order[0], "the acknowledgement came before the block: %v", order)
}

func TestRayFindsTheFaceItEntered(t *testing.T) {
	cases := []struct {
		from, to cube.Pos
		want     cube.Face
	}{
		{cube.Pos{0, 0, 0}, cube.Pos{1, 0, 0}, cube.FaceWest},
		{cube.Pos{0, 0, 0}, cube.Pos{-1, 0, 0}, cube.FaceEast},
		{cube.Pos{0, 0, 0}, cube.Pos{0, -1, 0}, cube.FaceUp},
		{cube.Pos{0, 0, 0}, cube.Pos{0, 1, 0}, cube.FaceDown},
		{cube.Pos{0, 0, 0}, cube.Pos{0, 0, 1}, cube.FaceNorth},
		{cube.Pos{0, 0, 0}, cube.Pos{0, 0, -1}, cube.FaceSouth},
	}
	for _, c := range cases {
		got, ok := entered(c.from, c.to)
		require.True(t, ok)
		require.Equal(t, c.want, got, "from %v to %v", c.from, c.to)
	}
}

// A look from a client is kept as the server keeps one, and survives
// Dragonfly's rotation fix-up unchanged.
func TestJavaLook(t *testing.T) {
	for _, c := range []struct{ yaw, pitch, wantYaw, wantPitch float64 }{
		{30, 10, 30, 10},
		{750, 400, 30, 90},
		{-200, -95, 160, -90},
		{180, 90, -180, 90},
		{-540, 0, -180, 0},
	} {
		y, p := javaLook(c.yaw, c.pitch)
		r := cube.Rotation{}.Add(cube.Rotation{y, p})
		if dy := math.Mod(math.Abs(r.Yaw()-c.wantYaw), 360); math.Min(dy, 360-dy) > 1e-5 || math.Abs(r.Pitch()-c.wantPitch) > 1e-5 {
			t.Errorf("javaLook(%v, %v) kept as %v, %v; want %v, %v", c.yaw, c.pitch, r.Yaw(), r.Pitch(), c.wantYaw, c.wantPitch)
		}
	}
}
