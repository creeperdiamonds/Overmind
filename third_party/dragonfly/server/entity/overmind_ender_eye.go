package entity

import (
	"math"
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/internal/nbtconv"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/go-gl/mathgl/mgl64"
)

// Overmind fork: a thrown eye of ender. minecraft.wiki "Eye of Ender" (oldid 3835744): it flies about 12
// blocks toward the nearest stronghold, through any blocks, upward while the stronghold is over 12 blocks
// away and downward to it once closer; after two or three seconds of flight it floats a moment, then falls
// as an item (80%) or shatters (20%). The numbers below are vanilla Java's EyeOfEnder (signalTo and tick),
// studied outside the repo; Bedrock's eye was not measured.

const (
	// eyeReach is how far toward the stronghold the eye flies, in blocks across (EyeOfEnder.signalTo).
	eyeReach = 12
	// eyeRise is how far the eye rises while the stronghold is farther than eyeReach (EyeOfEnder.signalTo:
	// the target is 8 blocks above the thrower).
	eyeRise = 8
	// eyeLife is the eye's life: it ends on the tick its age passes 80 (EyeOfEnder.tick).
	eyeLife = 80
	// eyeEase is how much of the way from its horizontal speed to the distance left the eye's speed moves
	// each tick, eyeClimb how much of the way its vertical speed moves to 1 block a tick up or down, toward
	// the target's height, and eyeBrake what is left of both while it is within a block across of the
	// target (EyeOfEnder.tick).
	eyeEase, eyeClimb, eyeBrake = 0.0025, 0.015, 0.8
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

// EnderEyeBehaviour moves a thrown eye of ender toward its target as vanilla's does: from rest it speeds up
// across, slowly, eases to a stop over the target and bobs about its height until the end of its life.
// It flies about 2 seconds to 12 blocks away.
type EnderEyeBehaviour struct {
	conf EnderEyeBehaviourConfig
	age  int
}

// Target returns where the eye flies to.
func (b *EnderEyeBehaviour) Target() mgl64.Vec3 { return b.conf.Target }

// Survives reports whether the eye falls as an item at the end of its life.
func (b *EnderEyeBehaviour) Survives() bool { return b.conf.Survive }

// Tick moves the eye, and ends its life after eyeLife ticks.
func (b *EnderEyeBehaviour) Tick(e *Ent, tx *world.Tx) *Movement {
	old, oldVel := e.data.Pos, e.data.Vel
	pos := old.Add(oldVel)
	e.data.Pos, e.data.Vel = pos, b.velocity(old, pos, oldVel)
	m := &Movement{v: tx.Viewers(old), e: e, pos: pos, vel: e.data.Vel, dpos: pos.Sub(old), dvel: e.data.Vel.Sub(oldVel), rot: e.data.Rot}
	b.age++
	if b.age > eyeLife {
		// Its life ends where it moved to this tick.
		if b.conf.Survive {
			tx.AddEntity(NewItem(world.EntitySpawnOpts{Position: pos}, item.NewStack(item.EnderEye{}, 1)))
		} else {
			tx.AddParticle(pos, particle.EnderEyeShatter{})
		}
		_ = e.Close()
		return nil
	}
	return m
}

// velocity is the eye's next velocity, once it has moved from old to pos with the velocity vel (EyeOfEnder.tick):
// across, toward the target, its speed eases from what it was to the distance left; up or down, toward the
// target's height from where it was before the move. The distance, the angle and the climb are floats in
// vanilla's code, and rounded so here.
func (b *EnderEyeBehaviour) velocity(old, pos, vel mgl64.Vec3) mgl64.Vec3 {
	t := b.conf.Target
	dx, dz := t[0]-pos[0], t[2]-pos[2]
	left := float64(float32(math.Hypot(dx, dz)))
	angle := float64(float32(math.Atan2(dz, dx)))
	speed, vy := math.Hypot(vel[0], vel[2]), vel[1]
	speed += eyeEase * (left - speed)
	if left < 1 {
		speed, vy = speed*eyeBrake, vy*eyeBrake
	}
	sign := -1.0
	if old[1] < t[1] {
		sign = 1
	}
	return mgl64.Vec3{math.Cos(angle) * speed, vy + (sign-vy)*float64(float32(eyeClimb)), math.Sin(angle) * speed}
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
