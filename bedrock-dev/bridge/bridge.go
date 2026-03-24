// Package bridge provides a lightweight TCP event-streaming server that pushes
// Bedrock player lifecycle events (join / move / leave) to connected Java bridge
// clients as newline-delimited JSON.  The Java side (DragonflybridgeClient) opens
// a persistent TCP connection on start-up and fans every received event into the
// shared PlayerRegistry so Java clients see Bedrock players in real time.
package bridge

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"sync"
)

// EventType identifies the kind of player event (Go → Java).
type EventType string

const (
	EventJoin  EventType = "join"
	EventLeave EventType = "leave"
	EventMove  EventType = "move"
)

// CommandType identifies the kind of command (Java → Go).
type CommandType string

const (
	CmdKick      CommandType = "kick"
	CmdMessage   CommandType = "message"
	CmdBroadcast CommandType = "broadcast"
	CmdTeleport  CommandType = "teleport"
)

// Command is a newline-delimited JSON payload sent from the Java server to Go.
type Command struct {
	Cmd    CommandType `json:"cmd"`
	Name   string      `json:"name,omitempty"`   // target player username
	Reason string      `json:"reason,omitempty"` // kick reason
	Text   string      `json:"text,omitempty"`   // chat text / broadcast message
	X      float64     `json:"x,omitempty"`
	Y      float64     `json:"y,omitempty"`
	Z      float64     `json:"z,omitempty"`
}

// PlayerEvent is the newline-delimited JSON payload broadcast to Java bridge clients.
type PlayerEvent struct {
	Type  EventType `json:"type"`
	Name  string    `json:"name"`
	X     float64   `json:"x,omitempty"`
	Y     float64   `json:"y,omitempty"`
	Z     float64   `json:"z,omitempty"`
	Yaw   float64   `json:"yaw,omitempty"`
	Pitch float64   `json:"pitch,omitempty"`
}

// CommandHandler is called for every Command received from a Java bridge client.
type CommandHandler func(Command)

// Bridge is a TCP server that streams PlayerEvents to all connected Java bridge clients
// and receives Commands from them.
type Bridge struct {
	log     *slog.Logger
	mu      sync.RWMutex
	conn    map[net.Conn]struct{}
	cmdMu   sync.RWMutex
	handler CommandHandler
}

// SetCommandHandler registers the function called for every inbound Command from Java.
// Only one handler is supported; calling this again replaces the previous one.
func (b *Bridge) SetCommandHandler(h CommandHandler) {
	b.cmdMu.Lock()
	b.handler = h
	b.cmdMu.Unlock()
}

// New creates a Bridge and starts accepting connections on addr (e.g. ":25566").
// The Bridge begins streaming as soon as the first client connects.
func New(log *slog.Logger, addr string) (*Bridge, error) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("bridge: listen %s: %w", addr, err)
	}
	b := &Bridge{
		log:  log,
		conn: make(map[net.Conn]struct{}),
	}
	log.Info("Bridge listening.", "addr", addr)
	go b.accept(ln)
	return b, nil
}

// Emit broadcasts e to every connected Java bridge client.
// Clients that have disconnected are removed silently.
func (b *Bridge) Emit(e PlayerEvent) {
	data, err := json.Marshal(e)
	if err != nil {
		b.log.Error("bridge: marshal event", "err", err)
		return
	}
	data = append(data, '\n')

	b.mu.RLock()
	defer b.mu.RUnlock()
	for c := range b.conn {
		if _, err := c.Write(data); err != nil {
			// Mark as dead — removed on next accept/handleConn goroutine exit.
			_ = c.Close()
		}
	}
}

func (b *Bridge) accept(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		b.log.Info("Bridge: Java client connected.", "remote", c.RemoteAddr())
		b.mu.Lock()
		b.conn[c] = struct{}{}
		b.mu.Unlock()
		go b.readCommands(c)
	}
}

// readCommands reads newline-delimited JSON Commands from a Java bridge client,
// dispatching each to the registered CommandHandler.  When the connection closes
// the client is removed from the active set.
func (b *Bridge) readCommands(c net.Conn) {
	s := bufio.NewScanner(c)
	for s.Scan() {
		line := s.Bytes()
		if len(line) == 0 {
			continue
		}
		var cmd Command
		if err := json.Unmarshal(line, &cmd); err != nil {
			b.log.Warn("Bridge: bad command JSON", "err", err, "raw", string(line))
			continue
		}
		b.cmdMu.RLock()
		h := b.handler
		b.cmdMu.RUnlock()
		if h != nil {
			go h(cmd)
		}
	}
	b.mu.Lock()
	delete(b.conn, c)
	b.mu.Unlock()
	_ = c.Close()
	b.log.Info("Bridge: Java client disconnected.", "remote", c.RemoteAddr())
}
