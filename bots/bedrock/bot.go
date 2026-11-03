// Package bedrock is a small Bedrock test bot: it joins a server, walks and chats.
package bedrock

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	tick      = 50 * time.Millisecond
	walkSpeed = 0.2158 // blocks per tick, normal walking speed
)

// Bot is a connected Bedrock client.
type Bot struct {
	conn *minecraft.Conn
	name string

	mu       sync.Mutex
	pos      mgl32.Vec3
	angle    float64
	walking  bool
	tickNum  uint64
	chunks   atomic.Int64
	done     chan struct{}
	closeOne sync.Once
}

// Join connects to addr without Xbox login (the server must allow that) and
// waits until the bot has spawned.
func Join(addr, name string) (*Bot, error) {
	d := minecraft.Dialer{IdentityData: login.IdentityData{DisplayName: name}}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	conn, err := d.DialContext(ctx, "raknet", addr)
	if err != nil {
		return nil, err
	}
	if err := conn.DoSpawnContext(ctx); err != nil {
		_ = conn.Close()
		return nil, err
	}
	b := &Bot{conn: conn, name: name, pos: conn.GameData().PlayerPosition, done: make(chan struct{})}
	go b.read()
	go b.loop()
	return b, nil
}

// SetViewDistance asks the server for a view distance in chunks, as a client's render distance
// setting does. The server may hold it lower.
func (b *Bot) SetViewDistance(chunks int) error {
	return b.conn.WritePacket(&packet.RequestChunkRadius{ChunkRadius: int32(chunks), MaxChunkRadius: uint8(chunks)})
}

// read drains packets so the connection never backs up.
func (b *Bot) read() {
	for {
		pk, err := b.conn.ReadPacket()
		if err != nil {
			b.Close()
			return
		}
		switch pk.(type) {
		case *packet.LevelChunk, *packet.SubChunk:
			b.chunks.Add(1)
		}
	}
}

// loop sends one movement packet per tick, like a real client.
func (b *Bot) loop() {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-b.done:
			return
		case <-t.C:
			b.mu.Lock()
			b.tickNum++
			pk := &packet.PlayerAuthInput{Tick: b.tickNum, Position: b.pos}
			if b.walking {
				dx, dz := float32(math.Cos(b.angle)*walkSpeed), float32(math.Sin(b.angle)*walkSpeed)
				b.pos = b.pos.Add(mgl32.Vec3{dx, 0, dz})
				pk.Position = b.pos
				pk.Delta = mgl32.Vec3{dx, 0, dz}
				pk.Yaw = float32(b.angle*180/math.Pi) - 90
				pk.HeadYaw = pk.Yaw
				pk.MoveVector = mgl32.Vec2{0, 1}
			}
			b.mu.Unlock()
			if err := b.conn.WritePacket(pk); err != nil {
				return
			}
		}
	}
}

// Walk starts walking in the direction angle (radians; 0 is +X, pi/2 is +Z).
func (b *Bot) Walk(angle float64) {
	b.mu.Lock()
	b.angle, b.walking = angle, true
	b.mu.Unlock()
}

// ChunksReceived is how many chunk packets the server has sent so far.
func (b *Bot) ChunksReceived() int64 { return b.chunks.Load() }

// Say sends a chat message.
func (b *Bot) Say(text string) {
	_ = b.conn.WritePacket(&packet.Text{
		TextType:   packet.TextTypeChat,
		SourceName: b.name,
		Message:    text,
	})
}

// Close leaves the server.
func (b *Bot) Close() error {
	var err error
	b.closeOne.Do(func() {
		close(b.done)
		err = b.conn.Close()
	})
	return err
}
