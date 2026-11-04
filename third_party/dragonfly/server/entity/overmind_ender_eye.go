package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Overmind fork: a thrown eye of ender. minecraft.wiki "Eye of Ender" (oldid 3835744): it flies about 12
// blocks toward the nearest stronghold, through any blocks, upward while the stronghold is over 12 blocks
// away and downward to it once closer; after two or three seconds of flight it floats a moment, then falls
// as an item (80%) or shatters (20%).

const (
	// eyeReach is how far toward the stronghold the eye flies, in blocks across.
	eyeReach = 12
	// eyeRise is how far the eye rises while the stronghold is farther than eyeReach. The wiki says only
	// that it travels upward; 8 is ours.
	eyeRise = 8
	// eyeFlight is the eye's flight in ticks, 2.5 s, within the wiki's two or three seconds.
	eyeFlight = 50
	// eyeLife is the eye's life in ticks, the flight and a moment's float after it.
	eyeLife = 80
)

// NewEnderEye creates an eye of ender thrown from opts.Position toward the stronghold at stronghold. Whether
// it survives is decided now: 4 in 5 fall as an item.
func NewEnderEye(opts world.EntitySpawnOpts, stronghold cube.Pos) *world.EntityHandle {
	return opts.New(EnderEyeType, EnderEyeBehaviourConfig{
		Target:  EnderEyeTarget(opts.Position, stronghold),
		Survive: rand.IntN(5) > 0,
	})
}

// EnderEyeTarget is where an eye thrown at from toward a stronghold flies to: eyeReach blocks toward it and
// eyeRise up while it is farther than that, else down to the stronghold itself.
func EnderEyeTarget(from mgl64.Vec3, stronghold cube.Pos) mgl64.Vec3 {
	to := stronghold.Vec3()
	dx, dz := to[0]-from[0], to[2]-from[2]
	if d := math.Hypot(dx, dz); d > eyeReach {
		return mgl64.Vec3{from[0] + dx/d*eyeReach, from[1] + eyeRise, from[2] + dz/d*eyeReach}
	}
	return to
}

// EnderEyeBehaviourConfig holds the parameters of an EnderEyeBehaviour.
type EnderEyeBehaviourConfig struct {
	// Target is where the eye flies to.
	Target mgl64.Vec3
	// Survive is whether the eye falls as an item at the end of its life, rather than shattering.
	Survive bool
}

func (conf EnderEyeBehaviourConfig) Apply(data *world.EntityData) {
	data.Data = conf.New()
}

// New creates an EnderEyeBehaviour.
func (conf EnderEyeBehaviourConfig) New() *EnderEyeBehaviour {
	return &EnderEyeBehaviour{conf: conf}
}

// EnderEyeBehaviour moves a thrown eye of ender in a straight line to its target over eyeFlight ticks, then
// holds it there until the end of its life.
type EnderEyeBehaviour struct {
	conf  EnderEyeBehaviourConfig
	from  mgl64.Vec3
	age   int
	close bool
}

// Target returns where the eye flies to.
func (b *EnderEyeBehaviour) Target() mgl64.Vec3 { return b.conf.Target }

// Survives reports whether the eye falls as an item at the end of its life.
func (b *EnderEyeBehaviour) Survives() bool { return b.conf.Survive }

// Tick moves the eye, and ends its life after eyeLife ticks.
func (b *EnderEyeBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	if b.close {
		_ = e.Close()
		return nil
	}
	old, oldVel := e.data.Pos, e.data.Vel
	if b.age == 0 {
		b.from = old
	}
	b.age++
	f := min(float64(b.age)/eyeFlight, 1)
	pos := b.from.Add(b.conf.Target.Sub(b.from).Mul(f))
	e.data.Pos, e.data.Vel = pos, pos.Sub(old)
	m := &Movement{v: tx.Viewers(old), e: e, pos: pos, vel: e.data.Vel, dpos: pos.Sub(old), dvel: e.data.Vel.Sub(oldVel), rot: e.data.Rot}
	if b.age >= eyeLife {
		b.close = true
		if b.conf.Survive {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.EnderEye{}, 1)))
		}
	}
	return m
}

// EnderEyeType is a world.EntityType implementation for a thrown eye of ender.
var EnderEyeType enderEyeType

type enderEyeType struct{}

func (enderEyeType) Open(tx *world.Tx, handle *world.EntityHandle, data *world.EntityData) world.Entity {
	return &Ent{tx: tx, handle: handle, data: data}
}

func (enderEyeType) EncodeEntity() string { return "minecraft:eye_of_ender_signal" }
func (enderEyeType) BBox(world.Entity) cube.BBox {
	return cube.Box(-0.125, 0, -0.125, 0.125, 0.25, 0.125)
}

// DecodeNBT reads an eye saved in flight. It starts its flight again from where it was.
func (enderEyeType) DecodeNBT(m map[string]any, data *world.EntityData) {
	data.Data = EnderEyeBehaviourConfig{
		Target:  nbtconv.Vec3(m, "Target"),
		Survive: nbtconv.Bool(m, "Survive"),
	}.New()
}

func (enderEyeType) EncodeNBT(data *world.EntityData) map[string]any {
	b := data.Data.(*EnderEyeBehaviour)
	return map[string]any{"Target": nbtconv.Vec3ToFloat32Slice(b.conf.Target), "Survive": b.conf.Survive}
}
