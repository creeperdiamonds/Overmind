package javafront

import (
	"strings"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/overmind/overmind/internal/javalogin"
	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/tables/java261"
)

// Player metadata, by minecraft-data's indexes and types (java261).
var (
	metaFlags      = metaIndex("player", "shared_flags")
	metaPose       = metaIndex("player", "pose")
	metaLiving     = metaIndex("player", "living_entity_flags")
	metaHealth     = metaIndex("player", "health")
	metaSkinParts  = metaIndex("player", "player_mode_customisation")
	metaTypeByte   = java261.MetadataType("byte")
	metaTypeFloat  = java261.MetadataType("float")
	metaTypePose   = java261.MetadataType("pose")
	metaTypesReady = metaTypeByte >= 0 && metaTypeFloat >= 0 && metaTypePose >= 0 && metaItemStack >= 0
)

// The bits of shared_flags and living_entity_flags, and the poses. Neither is
// in minecraft-data (it gives the value types only); these are minecraft.wiki's
// "Java Edition protocol/Entity metadata" page.
const (
	flagOnFire    = 0x01
	flagCrouching = 0x02
	flagSprinting = 0x08
	flagSwimming  = 0x10
	flagInvisible = 0x20
	flagGliding   = 0x80

	livingHandActive = 0x01

	poseStanding   = 0
	poseFallFlying = 1
	poseSleeping   = 2
	poseSwimming   = 3
	poseCrouching  = 5
	poseDying      = 7
)

func init() {
	if !metaTypesReady {
		panic("javafront: metadata types missing from the tables")
	}
}

// sharedFlags is a player's shared_flags byte.
func sharedFlags(p *player.Player) byte {
	var f byte
	if p.OnFireDuration() > 0 {
		f |= flagOnFire
	}
	if p.Sneaking() {
		f |= flagCrouching
	}
	if p.Sprinting() {
		f |= flagSprinting
	}
	if p.Swimming() || p.Crawling() {
		f |= flagSwimming
	}
	if p.Invisible() {
		f |= flagInvisible
	}
	if p.Gliding() {
		f |= flagGliding
	}
	return f
}

// pose is the pose Java draws a player in. Java works out its own player's;
// others' come from the server.
func pose(p *player.Player) int32 {
	_, sleeping := p.Sleeping()
	switch {
	case p.Dead():
		return poseDying
	case sleeping:
		return poseSleeping
	case p.Gliding():
		return poseFallFlying
	case p.Swimming() || p.Crawling():
		return poseSwimming
	case p.Sneaking():
		return poseCrouching
	}
	return poseStanding
}

// playerMeta is what Java shows of another player: flags, pose, a hand in use,
// health and the skin layers.
func (c *conn) playerMeta(p *player.Player) javaproto.Meta {
	var m javaproto.Meta
	m.Byte(metaFlags, metaTypeByte, sharedFlags(p))
	m.VarInt(metaPose, metaTypePose, pose(p))
	var living byte
	if p.UsingItem() {
		living |= livingHandActive
	}
	m.Byte(metaLiving, metaTypeByte, living)
	m.Float(metaHealth, metaTypeFloat, float32(p.Health()))
	m.Byte(metaSkinParts, metaTypeByte, c.f.skinParts(p))
	return m
}

// selfMeta is what the client is told of its own player: the flags the server
// sets (fire, invisible) and the skin layers it chose, which it draws from this.
// Its pose and health it has already.
func (c *conn) selfMeta(p *player.Player) javaproto.Meta {
	var m javaproto.Meta
	m.Byte(metaFlags, metaTypeByte, sharedFlags(p))
	m.Byte(metaSkinParts, metaTypeByte, byte(c.skin.Load()))
	return m
}

// ViewEntityState shows a player's state: sneaking, sprinting and the rest.
func (c *conn) ViewEntityState(e world.Entity) {
	if !c.viewing() {
		return
	}
	p, ok := e.(*player.Player)
	if !ok {
		return
	}
	if e.H() == c.handle {
		c.send(javaproto.EntityMetadata(selfEntityID, c.selfMeta(p)))
		return
	}
	if s, ok := c.ents[e.H()]; ok {
		s.health = float32(p.Health())
		c.send(javaproto.EntityMetadata(s.id, c.playerMeta(p)))
	}
}

// syncHealth tells the client when a player it sees has more or less health.
// Dragonfly tells only the player itself, but Java shows others' health (as
// metadata) and draws a player dying from it.
func (c *conn) syncHealth(tx *world.Tx) {
	for h, s := range c.ents {
		if !s.player {
			continue
		}
		e, ok := h.Entity(tx)
		if !ok {
			continue
		}
		p, ok := e.(*player.Player)
		if hp := float32(p.Health()); ok && hp != s.health {
			s.health = hp
			var m javaproto.Meta
			m.Float(metaHealth, metaTypeFloat, hp)
			c.send(javaproto.EntityMetadata(s.id, m))
		}
	}
}

// skinParts is the skin layers Java shows for a player: a Java player's own
// choice, and every layer for a Bedrock player, whose skins have no layers to
// turn off.
func (f *Front) skinParts(p *player.Player) byte {
	f.mu.Lock()
	c, ok := f.names[strings.ToLower(p.Name())]
	f.mu.Unlock()
	if ok && c.id.UUID == p.UUID() {
		return byte(c.skin.Load())
	}
	return javalogin.AllSkinParts
}

// input applies the keys a client holds (player_input): sneaking follows the
// shift key, and a jump is the moment the jump key goes down.
func (c *conn) input(p *player.Player, keys byte) {
	was := c.keys
	c.keys = keys
	if sneak := keys&javaproto.InputShift != 0; sneak != p.Sneaking() {
		if sneak {
			p.StartSneaking()
		} else {
			p.StopSneaking()
		}
	}
	if keys&javaproto.InputJump != 0 && was&javaproto.InputJump == 0 {
		p.Jump()
	}
}

// command applies an entity_action (Java's "player command").
func (c *conn) command(p *player.Player, a javaproto.EntityAction) {
	switch a.Action {
	case javaproto.ActionStartSprinting:
		p.StartSprinting()
	case javaproto.ActionStopSprinting:
		p.StopSprinting()
	case javaproto.ActionStartFallFlying:
		// Dragonfly checks the elytra, as Java's server does.
		if !p.OnGround() {
			p.StartGliding()
		}
	case javaproto.ActionStopSleeping:
		p.Wake()
	}
}

// settings applies the client's settings during play: its skin layers, shown to
// itself and to everyone who sees it.
func (c *conn) settings(tx *world.Tx, p *player.Player, s javaproto.ClientSettings) {
	if byte(c.skin.Load()) == s.SkinParts {
		return
	}
	c.skin.Store(uint32(s.SkinParts))
	// The client's own loader is among the viewers, so it is told too.
	for _, v := range tx.Viewers(p.Position()) {
		v.ViewEntityState(p)
	}
}

// moveState applies Java's own rules for what a client does not say: swimming
// starts when a sprinting player's eyes are in water and lasts while it
// sprints in water; gliding ends on the ground or in water (minecraft.wiki,
// "Swimming" and "Elytra").
func (c *conn) moveState(tx *world.Tx, p *player.Player) {
	inWater := func(pos cube.Pos) bool {
		l, ok := tx.Liquid(pos)
		_, water := l.(block.Water)
		return ok && water
	}
	feet := inWater(cube.PosFromVec3(p.Position()))
	if p.Swimming() {
		if !p.Sprinting() || !feet {
			p.StopSwimming()
		}
	} else if p.Sprinting() && inWater(cube.PosFromVec3(entity.EyePosition(p))) {
		p.StartSwimming()
	}
	if p.Gliding() && (p.OnGround() || feet) {
		p.StopGliding()
	}
}
