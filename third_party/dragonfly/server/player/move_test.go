package player

import (
	"context"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

func moveWorld(t *testing.T, f func(p *Player)) {
	t.Helper()
	w := world.Config{Entities: entity.DefaultRegistry, Generator: world.NopGenerator{}, Dim: world.Overworld}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := mgl64.Vec3{0.5, 100, 0.5}
	id := uuid.New()
	h := world.EntitySpawnOpts{Position: pos, ID: id}.New(Type, Config{Name: "m", UUID: id, Position: pos})
	if _, err := world.Call(context.Background(), w, func(tx *world.Tx) (struct{}, error) {
		f(tx.AddEntity(h).(*Player))
		return struct{}{}, nil
	}); err != nil {
		t.Fatal(err)
	}
}

// One long move used to hold the world for seconds while it loaded every chunk
// under the swept box.
func TestLongMoveIsRefusedQuickly(t *testing.T) {
	moveWorld(t, func(p *Player) {
		start := p.Position()
		began := time.Now()
		p.Move(mgl64.Vec3{4096, 0, 4096}, 0, 0)
		if d := time.Since(began); d > 100*time.Millisecond {
			t.Errorf("a refused move held the world %v", d)
		}
		if p.Position() != start {
			t.Errorf("a refused move changed the position to %v", p.Position())
		}
	})
}

// Moves up to the limit behave exactly as before.
func TestOrdinaryMovesAreUnchanged(t *testing.T) {
	moveWorld(t, func(p *Player) {
		for _, d := range []mgl64.Vec3{{0.2, 0, 0}, {0, -0.4, 0.2}, {3, 1, 3}, {20, 0, 20}, {maxMoveDelta, 0, 0}} {
			before, rot := p.Position(), p.Rotation()
			p.Move(d, 10, 5)
			if got, want := p.Position(), before.Add(d); !got.ApproxEqual(want) {
				t.Errorf("move %v: at %v, want %v", d, got, want)
			}
			if got, want := p.Rotation(), rot.Add(cube.Rotation{10, 5}); got != want {
				t.Errorf("move %v: rotation %v, want %v", d, got, want)
			}
		}
	})
}
