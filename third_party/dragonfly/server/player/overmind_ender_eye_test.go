package player

import (
	"math"
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Overmind fork: throwing an eye of ender (item/ender_eye.go, entity/overmind_ender_eye.go). Strongholds do
// not generate yet, so a stub generator stands in for one.

// strongholdAt is a generator with one stronghold, or none if at is nil.
type strongholdAt struct {
	world.NopGenerator
	at *cube.Pos
}

func (s strongholdAt) NearestStronghold(cube.Pos) (cube.Pos, bool) {
	if s.at == nil {
		return cube.Pos{}, false
	}
	return *s.at, true
}

// eyeWorld is a synchronous world of dim with the generator gen and a player holding three eyes at
// 0.5, 100, 0.5.
func eyeWorld(t *testing.T, dim world.Dimension, gen world.Generator) (*world.World, *world.EntityHandle) {
	t.Helper()
	w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry, Generator: gen, Dim: dim}.New()
	t.Cleanup(func() { _ = w.Close() })
	pos := mgl64.Vec3{0.5, 100, 0.5}
	id := uuid.New()
	h := world.EntitySpawnOpts{Position: pos, ID: id}.New(Type, Config{Name: "thrower", UUID: id, Position: pos})
	w.Do(func(tx *world.Tx) {
		p := tx.AddEntity(h).(*Player)
		p.SetGameMode(world.GameModeSurvival)
		p.SetHeldItems(item.NewStack(item.EnderEye{}, 3), item.Stack{})
	})
	return w, h
}

func throw(w *world.World, h *world.EntityHandle) (left int) {
	w.Do(func(tx *world.Tx) {
		p, _ := h.Entity(tx)
		p.(*Player).UseItem()
		held, _ := p.(*Player).HeldItems()
		left = held.Count()
	})
	return left
}

// eyes is every eye of ender in the world and its behaviour.
func eyes(w *world.World) (out []*entity.EnderEyeBehaviour, at []mgl64.Vec3) {
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if e.H().Type() == entity.EnderEyeType {
				out = append(out, e.(*entity.Ent).Behaviour().(*entity.EnderEyeBehaviour))
				at = append(at, e.Position())
			}
		}
	})
	return out, at
}

func droppedEyes(w *world.World) (n int) {
	w.Do(func(tx *world.Tx) {
		for e := range tx.Entities() {
			if it, ok := e.(*entity.Ent); ok && e.H().Type() == entity.ItemType {
				if _, eye := it.Behaviour().(*entity.ItemBehaviour).Item().Item().(item.EnderEye); eye {
					n++
				}
			}
		}
	})
	return n
}

// A thrown eye flies 12 blocks across toward a far stronghold and up, reaching that point by the end of its
// flight, then is gone: an item or nothing.
func TestEnderEyeFliesTowardFarStronghold(t *testing.T) {
	at := cube.Pos{1000, 30, 0}
	w, h := eyeWorld(t, world.Overworld, strongholdAt{at: &at})
	if left := throw(w, h); left != 2 {
		t.Fatalf("the throw left %d eyes in hand, want 2", left)
	}
	bs, pos := eyes(w)
	if len(bs) != 1 {
		t.Fatalf("%d eyes in the world, want 1", len(bs))
	}
	start := pos[0]
	target := bs[0].Target()
	if d := math.Hypot(target[0]-start[0], target[2]-start[2]); math.Abs(d-12) > 1e-9 {
		t.Errorf("the eye flies %.3f blocks across, want 12", d)
	}
	if target[0] <= start[0] || math.Abs(target[2]-start[2]) > 0.6 {
		t.Errorf("the eye flies to %v from %v, not toward the stronghold at %v", target, start, at)
	}
	if target[1] <= start[1] {
		t.Errorf("toward a far stronghold the eye flies to Y %.1f from Y %.1f, want up", target[1], start[1])
	}
	ticks := 0
	// Where vanilla's EyeOfEnder.tick puts an eye thrown at rest toward this target, from the thrower, after
	// as many ticks (worked out apart from this code): slow at first, over the target by tick 50 and
	// bobbing about its height after.
	for _, c := range []struct {
		tick int
		at   mgl64.Vec3
	}{
		{10, mgl64.Vec3{1.3166, 0.6487, -0.0007}},
		{30, mgl64.Vec3{10.6125, 5.6972, -0.0053}},
		{50, mgl64.Vec3{11.9935, 8.1449, -0.0060}},
		{80, mgl64.Vec3{12.0000, 8.0998, -0.0060}},
	} {
		for ; ticks < c.tick; ticks++ {
			w.AdvanceTick()
		}
		want := start.Add(c.at)
		if _, pos := eyes(w); len(pos) != 1 || !pos[0].ApproxEqualThreshold(want, 1e-3) {
			t.Fatalf("after %d ticks the eye is at %v, want %v", c.tick, pos, want)
		}
		if c.tick >= 50 && !eyes1(w).ApproxEqualThreshold(target, 0.2) {
			t.Errorf("after %d ticks the eye is not over its target %v", c.tick, target)
		}
	}
	w.AdvanceTick()
	if bs, _ := eyes(w); len(bs) != 0 {
		t.Fatalf("the eye is still there after its life of 80 ticks")
	}
}

// eyes1 is where the one eye in the world is.
func eyes1(w *world.World) mgl64.Vec3 { _, pos := eyes(w); return pos[0] }

// Near a stronghold the eye flies down to it.
func TestEnderEyeFliesDownToNearStronghold(t *testing.T) {
	at := cube.Pos{5, 40, 7}
	w, h := eyeWorld(t, world.Overworld, strongholdAt{at: &at})
	throw(w, h)
	bs, _ := eyes(w)
	if len(bs) != 1 || bs[0].Target() != at.Vec3() {
		t.Fatalf("the eye flies to %v, want the stronghold at %v", bs, at)
	}
}

// With no stronghold, or outside the Overworld, nothing is thrown and the eye is kept.
func TestEnderEyeNotThrownWithoutStronghold(t *testing.T) {
	at := cube.Pos{1000, 30, 0}
	for _, c := range []struct {
		name string
		dim  world.Dimension
		gen  world.Generator
	}{
		{"no stronghold", world.Overworld, strongholdAt{}},
		{"the Nether", world.Nether, strongholdAt{at: &at}},
		{"the End", world.End, strongholdAt{at: &at}},
	} {
		w, h := eyeWorld(t, c.dim, c.gen)
		if left := throw(w, h); left != 3 {
			t.Errorf("%s: %d eyes left in hand, want 3", c.name, left)
		}
		if bs, _ := eyes(w); len(bs) != 0 {
			t.Errorf("%s: an eye was thrown", c.name)
		}
	}
}

// At the end of its life an eye falls as an item if it survives, and shatters otherwise; 4 in 5 survive.
func TestEnderEyeEndsAsItemOrShatters(t *testing.T) {
	for _, survive := range []bool{true, false} {
		w, _ := eyeWorld(t, world.Overworld, strongholdAt{})
		w.Do(func(tx *world.Tx) {
			tx.AddEntity(world.EntitySpawnOpts{Position: mgl64.Vec3{0.5, 100, 0.5}}.New(entity.EnderEyeType,
				entity.EnderEyeBehaviourConfig{Target: mgl64.Vec3{10, 105, 0}, Survive: survive}))
		})
		for range 90 {
			w.AdvanceTick()
		}
		want := 0
		if survive {
			want = 1
		}
		if n := droppedEyes(w); n != want {
			t.Errorf("survive %v: %d eyes dropped, want %d", survive, n, want)
		}
	}
	survived := 0
	const n = 2000
	w, _ := eyeWorld(t, world.Overworld, strongholdAt{})
	w.Do(func(tx *world.Tx) {
		for range n {
			tx.AddEntity(entity.NewEnderEye(world.EntitySpawnOpts{}, cube.Pos{100, 0, 0}))
		}
	})
	bs, _ := eyes(w)
	for _, b := range bs {
		if b.Survives() {
			survived++
		}
	}
	if f := float64(survived) / n; f < 0.77 || f > 0.83 {
		t.Errorf("%.3f of eyes survive, want 0.8", f)
	}
}
