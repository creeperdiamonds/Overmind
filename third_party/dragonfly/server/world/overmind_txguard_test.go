package world_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
)

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// Overmind fork (overmind_txguard.go): a transaction that calls runtime.Goexit, as t.FailNow in a
// world callback does, used to end the world's transaction goroutine silently, and the world never
// ran anything again. Now it is logged with its stack, its task fails, and the world keeps running.
func TestWorldSurvivesGoexitInTransaction(t *testing.T) {
	var logs syncBuffer
	w := world.Config{Entities: entity.DefaultRegistry, Log: slog.New(slog.NewTextHandler(&logs, nil))}.New()
	t.Cleanup(func() { _ = w.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A transaction of the world's own machinery (exec, used by the tick) is guarded too, but Do is
	// what callers use, so it is the one tested.
	err := w.Do(func(*world.Tx) { runtime.Goexit() }).Wait(ctx)
	if !errors.Is(err, world.ErrTransactionExited) {
		t.Fatalf("task that called Goexit: err %v, want ErrTransactionExited", err)
	}
	for i := range 3 {
		ran := false
		if err := w.Do(func(*world.Tx) { ran = true }).Wait(ctx); err != nil || !ran {
			t.Fatalf("transaction %d after the Goexit: ran %v, err %v; the world stopped", i, ran, err)
		}
	}
	if s := logs.String(); !strings.Contains(s, "runtime.Goexit") || !strings.Contains(s, "overmind_txguard_test.go") {
		t.Fatalf("the Goexit was not logged with its stack; log:\n%s", s)
	}
}
