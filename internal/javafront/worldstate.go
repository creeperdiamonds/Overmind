package javafront

import (
	"math/rand/v2"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/particle"
	"github.com/df-mc/dragonfly/server/world/sound"
	"github.com/go-gl/mathgl/mgl64"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
)

// The world's state as the Java client sees it: the time of day, the weather, and the sounds and block
// breaks of the world that both editions hear. A world calls these on its own goroutine, and each is
// dropped unless the conn views that world (viewing).

// overworldClock is the Overworld's place in the minecraft:world_clock registry, the clock its dimension
// type follows.
var overworldClock = registryIndex("minecraft:world_clock", "minecraft:overworld")

// ViewTime sets the client's clock. The world calls it every 20 ticks and when the time is set, as when
// everyone has slept.
func (c *conn) ViewTime(t int) {
	if c.viewing() {
		c.dayTime = int64(t)
		c.sendTime()
	}
}

// ViewTimeCycle starts or stops the client's clock.
func (c *conn) ViewTimeCycle(cycle bool) {
	if c.viewing() {
		c.dayStill = !cycle
		c.sendTime()
	}
}

// sendTime sends the Overworld clock, advancing a clock tick a client tick while the day cycles. Only the
// Overworld has a day: Dragonfly's Nether and End have no time cycle.
func (c *conn) sendTime() {
	if c.dim.name != overworldName {
		return
	}
	rate := float32(1)
	if c.dayStill {
		rate = 0
	}
	c.send(javaproto.UpdateTime(c.dayTime, overworldClock(), c.dayTime, rate))
}

// ViewWeather shows rain and thunder when they change. The world calls it every 20 ticks.
func (c *conn) ViewWeather(raining, thunder bool) {
	if c.viewing() {
		c.showWeather(raining, thunder)
	}
}

// showWeather sends what changed, as the vanilla server does: rain starting is "begin raining" and the
// rain level, thunder is its level.
func (c *conn) showWeather(raining, thunder bool) {
	if raining != c.raining {
		c.raining = raining
		if raining {
			c.send(javaproto.GameEvent(javaproto.EventBeginRaining, 0))
			c.send(javaproto.GameEvent(javaproto.EventRainLevelChange, 1))
		} else {
			c.send(javaproto.GameEvent(javaproto.EventEndRaining, 0))
			c.send(javaproto.GameEvent(javaproto.EventRainLevelChange, 0))
		}
	}
	if thunder != c.thunder {
		c.thunder = thunder
		c.send(javaproto.GameEvent(javaproto.EventThunderLevelChange, level(thunder)))
	}
}

func level(on bool) float32 {
	if on {
		return 1
	}
	return 0
}

// sendWorldState shows the world's time and weather afresh, to a client that has just entered a level
// (joined, or changed dimension), which starts it with no rain.
func (c *conn) sendWorldState(tx *world.Tx) {
	w := tx.World()
	c.dayTime, c.dayStill = int64(w.Time()), !w.TimeCycle()
	c.sendTime()
	c.raining, c.thunder = false, false
	if w.Dimension().WeatherCycle() {
		c.showWeather(tx.Raining(), tx.Thundering())
	}
}

// javaSound is a Java sound with the volume, the pitch range and the category the vanilla game plays it
// with, from each sound's row on its minecraft.wiki page.
type javaSound struct {
	name             string
	category         int32
	volume           float32
	pitchLo, pitchHi float32
}

var (
	// "Explosion", oldid 3866492; "TNT", oldid 3878615.
	explodeSound = javaSound{"entity.generic.explode", javaproto.SoundBlock, 4, 0.56, 0.84}
	tntSound     = javaSound{"entity.tnt.primed", javaproto.SoundBlock, 1, 1, 1}
	// "Thunderstorm", oldid 3806763.
	thunderSound = javaSound{"entity.lightning_bolt.thunder", javaproto.SoundWeather, 10000, 0.8, 1}
	impactSound  = javaSound{"entity.lightning_bolt.impact", javaproto.SoundWeather, 2, 0.5, 0.7}
	// "Lava", oldid 3870569; "Fire", oldid 3869722; "Flint and Steel", oldid 3867303.
	fizzSound       = javaSound{"block.lava.extinguish", javaproto.SoundBlock, 0.5, 1.8, 3.4}
	extinguishSound = javaSound{"block.fire.extinguish", javaproto.SoundBlock, 0.5, 1.8, 3.4}
	igniteSound     = javaSound{"item.flintandsteel.use", javaproto.SoundBlock, 1, 0.8, 1.2}
	// "Chest", oldid 3872794; "Barrel", oldid 3832972; "Ender Chest", oldid 3871172.
	chestOpen      = javaSound{"block.chest.open", javaproto.SoundBlock, 0.5, 0.9, 1}
	chestClose     = javaSound{"block.chest.close", javaproto.SoundBlock, 0.5, 0.9, 1}
	barrelOpen     = javaSound{"block.barrel.open", javaproto.SoundBlock, 0.5, 0.9, 1}
	barrelClose    = javaSound{"block.barrel.close", javaproto.SoundBlock, 0.5, 0.9, 1}
	enderChestOpen = javaSound{"block.ender_chest.open", javaproto.SoundBlock, 0.5, 0.9, 1}
	enderChestShut = javaSound{"block.ender_chest.close", javaproto.SoundBlock, 0.5, 0.9, 1}
)

// woodSound is a wooden door's or fence gate's sound ("Door", oldid 3874236; "Fence Gate", oldid
// 3849670): cherry, bamboo and the Nether's woods have their own; open is "open", else "close".
func woodSound(kind string, w block.WoodType, open bool) javaSound {
	s := javaSound{category: javaproto.SoundBlock, volume: 0.9, pitchLo: 0.9, pitchHi: 1}
	prefix := map[string]string{"door": "block.wooden_door", "fence_gate": "block.fence_gate"}[kind]
	switch w {
	case block.CherryWood():
		prefix = "block.cherry_wood_" + kind
		if kind == "fence_gate" {
			s.pitchLo, s.pitchHi = 0.99, 1.1
		}
	case block.BambooWood():
		prefix = "block.bamboo_wood_" + kind
		if kind == "fence_gate" {
			s.pitchLo, s.pitchHi = 0.99, 1.1
		}
	case block.CrimsonWood(), block.WarpedWood():
		prefix, s.volume = "block.nether_wood_"+kind, 1
	}
	s.name = prefix + ".close"
	if open {
		s.name = prefix + ".open"
	}
	return s
}

// javaSoundsOf is what a Java client hears for a Dragonfly sound, nothing for one it has no counterpart
// for or makes itself. Fire and water ambience, and a block's footsteps and hits, are the Java client's
// own. A placed block's sound is left out: it depends on the block's sound type, which no table of ours
// has (Task W report).
func javaSoundsOf(s world.Sound) []javaSound {
	switch s := s.(type) {
	case sound.Explosion:
		return []javaSound{explodeSound}
	case sound.Thunder:
		return []javaSound{thunderSound, impactSound}
	case sound.TNT:
		return []javaSound{tntSound}
	case sound.Fizz:
		return []javaSound{fizzSound}
	case sound.FireExtinguish:
		return []javaSound{extinguishSound}
	case sound.Ignite:
		return []javaSound{igniteSound}
	case sound.ChestOpen:
		return []javaSound{chestOpen}
	case sound.ChestClose:
		return []javaSound{chestClose}
	case sound.BarrelOpen:
		return []javaSound{barrelOpen}
	case sound.BarrelClose:
		return []javaSound{barrelClose}
	case sound.EnderChestOpen:
		return []javaSound{enderChestOpen}
	case sound.EnderChestClose:
		return []javaSound{enderChestShut}
	case sound.DoorOpen:
		return doorSound(s.Block, true)
	case sound.DoorClose:
		return doorSound(s.Block, false)
	case sound.FenceGateOpen:
		if g, ok := s.Block.(block.WoodFenceGate); ok {
			return []javaSound{woodSound("fence_gate", g.Wood, true)}
		}
	case sound.FenceGateClose:
		if g, ok := s.Block.(block.WoodFenceGate); ok {
			return []javaSound{woodSound("fence_gate", g.Wood, false)}
		}
	}
	return nil
}

// doorSound is a door's sound: wooden by its wood, copper ("Door": volume 1, pitch 0.9 to 1).
func doorSound(b world.Block, open bool) []javaSound {
	switch d := b.(type) {
	case block.WoodDoor:
		return []javaSound{woodSound("door", d.Wood, open)}
	case block.CopperDoor:
		s := javaSound{"block.copper_door.close", javaproto.SoundBlock, 1, 0.9, 1}
		if open {
			s.name = "block.copper_door.open"
		}
		return []javaSound{s}
	}
	return nil
}

// pendingSound is a sound heard this tick, sent at its end.
type pendingSound struct {
	s   javaSound
	pos mgl64.Vec3
}

// ViewSound queues the world's sound for the end of the tick. A sound at a block the player itself used
// or broke this tick is left out: the Java client played it already, as the vanilla server leaves the
// player out.
func (c *conn) ViewSound(pos mgl64.Vec3, s world.Sound) {
	if !c.viewing() {
		return
	}
	if _, own := c.ownActs[cube.PosFromVec3(pos)]; own {
		return
	}
	for _, js := range javaSoundsOf(s) {
		c.sounds = append(c.sounds, pendingSound{js, pos})
	}
}

// ViewParticle shows a block breaking, with its sound (world event 2001), unless the player broke it: its
// client showed that itself.
func (c *conn) ViewParticle(pos mgl64.Vec3, p world.Particle) {
	if !c.viewing() {
		return
	}
	if b, ok := p.(particle.BlockBreak); ok {
		at := cube.PosFromVec3(pos)
		if _, own := c.ownActs[at]; own {
			return
		}
		state, _ := stateOf(at, world.BlockRuntimeID(b.Block), airRID(), b.Block)
		c.events = append(c.events, javaproto.WorldEvent(javaproto.WorldEventBlockBreak, at[0], at[1], at[2], state, false))
	}
}

// own notes a block the player uses or breaks this tick, whose sounds its client makes itself.
func (c *conn) own(pos cube.Pos) { c.ownActs[pos] = struct{}{} }

// flushSounds sends the tick's sounds and block breaks, after its block changes. Lightning is heard as
// Java's thunder and impact, and not as the explosion Dragonfly also plays for it.
func (c *conn) flushSounds() {
	for _, e := range c.events {
		c.send(e)
	}
	c.events = c.events[:0]
	thunder := map[cube.Pos]bool{}
	for _, s := range c.sounds {
		if s.s.name == thunderSound.name {
			thunder[cube.PosFromVec3(s.pos)] = true
		}
	}
	for _, s := range c.sounds {
		if s.s.name == explodeSound.name && thunder[cube.PosFromVec3(s.pos)] {
			continue
		}
		pitch := s.s.pitchLo + rand.Float32()*(s.s.pitchHi-s.s.pitchLo)
		c.send(javaproto.SoundEffect(s.s.name, s.s.category, s.pos[0], s.pos[1], s.pos[2], s.s.volume, pitch, rand.Int64()))
	}
	c.sounds = c.sounds[:0]
	clear(c.ownActs)
}
