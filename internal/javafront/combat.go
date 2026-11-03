package javafront

import (
	"math"
	"strings"
	"sync"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// swingEcho is how long after Dragonfly swung a player's arm a swing from the
// client is taken as the same one. A Java client sends one after each attack
// and each place, which Dragonfly has already shown.
const swingEcho = 150 * time.Millisecond

var _ player.AttackRules = (*conn)(nil)

// AttackDamage is the fork's player.AttackRules: the player's attacks follow
// Java's rules (cooldown, weapon damage), whatever they hit.
func (c *conn) AttackDamage(held item.Stack, dmg float64, critical bool) (float64, bool) {
	return c.rules.Attack(held, dmg, critical, time.Since(c.attackReset))
}

// attack handles attack: the client hit an entity it was shown. Java's reach
// applies, then Dragonfly's attack, with the damage AttackDamage gives. Every
// attack starts the cooldown again, as on the client.
func (c *conn) attack(tx *world.Tx, p *player.Player, target int32) {
	defer func() { c.attackReset = time.Now() }()
	e, ok := c.shownEntity(tx, target)
	if !ok {
		return
	}
	box := e.H().Type().BBox(e).Translate(e.Position())
	if !c.rules.CanReachEntity(entity.EyePosition(p), box, p.GameMode().CreativeInventory()) {
		return
	}
	// Viewers are shown the hurt during the attack, and are told what did it.
	c.f.striking(e, entity.AttackDamageSource{Attacker: p})
	defer c.f.striking(e, nil)
	p.AttackEntity(e)
}

// interact handles use_entity: the client used its held item on an entity.
func (c *conn) interact(tx *world.Tx, p *player.Player, u javaproto.UseEntity) {
	if u.Hand != javaproto.HandMain {
		return // Dragonfly uses the main hand only
	}
	e, ok := c.shownEntity(tx, u.Target)
	if !ok {
		return
	}
	box := e.H().Type().BBox(e).Translate(e.Position())
	if c.rules.CanReachEntity(entity.EyePosition(p), box, p.GameMode().CreativeInventory()) {
		p.UseItemOnEntity(e)
	}
}

// swing handles arm_animation. A swing Dragonfly has just shown (an attack, a
// place) is not shown again.
func (c *conn) swing(p *player.Player) {
	if time.Since(c.swungAt) > swingEcho {
		p.SwingArm()
	}
}

// shownEntity is the entity this client knows by id.
func (c *conn) shownEntity(tx *world.Tx, id int32) (world.Entity, bool) {
	for h, s := range c.ents {
		if s.id == id {
			return h.Entity(tx)
		}
	}
	return nil, false
}

// javaHandler wraps the player's handler to learn what hurt it, which the
// player does not pass on to its Reporter, and to show the death screen with
// its message.
type javaHandler struct {
	player.Handler
	c *conn
}

// HandleHurt notes the source of damage that goes ahead.
func (h javaHandler) HandleHurt(ctx *player.Context, dmg *float64, immune bool, immunity *time.Duration, src world.DamageSource) {
	h.Handler.HandleHurt(ctx, dmg, immune, immunity, src)
	if !ctx.Cancelled() {
		h.c.hurtBy = src
	}
}

// HandleDeath shows the client its death screen with Java's death message.
func (h javaHandler) HandleDeath(p *player.Player, src world.DamageSource, keepInv *bool) {
	h.Handler.HandleDeath(p, src, keepInv)
	_, key, by := javaDamage(src)
	msg := javaproto.Text{Translate: key, With: []javaproto.Text{{Text: javaName(p.Name())}}}
	if n, ok := by.(interface{ Name() string }); ok { // the keys with a second argument
		msg.With = append(msg.With, javaproto.Text{Text: javaName(n.Name())})
	}
	h.c.send(javaproto.DeathCombat(selfEntityID, msg))
}

// javaDamage names a Dragonfly damage source as Java's damage type, Java's
// death message key (both from minecraft-data: the damage_type registry and
// language.json) and the entity behind it, if any.
func javaDamage(src world.DamageSource) (typ, death string, by world.Entity) {
	switch s := src.(type) {
	case entity.AttackDamageSource:
		if _, ok := s.Attacker.(*player.Player); ok {
			return "player_attack", "death.attack.player", s.Attacker
		}
		return "mob_attack", "death.attack.mob", s.Attacker
	case entity.ProjectileDamageSource:
		return "arrow", "death.attack.arrow", s.Owner
	case entity.FallDamageSource:
		return "fall", "death.attack.fall", nil
	case entity.GlideDamageSource:
		return "fly_into_wall", "death.attack.flyIntoWall", nil
	case entity.VoidDamageSource:
		return "out_of_world", "death.attack.outOfWorld", nil
	case entity.DrowningDamageSource:
		return "drown", "death.attack.drown", nil
	case entity.SuffocationDamageSource:
		return "in_wall", "death.attack.inWall", nil
	case entity.ExplosionDamageSource:
		return "explosion", "death.attack.explosion", nil
	case entity.LightningDamageSource:
		return "lightning_bolt", "death.attack.lightningBolt", nil
	case block.FireDamageSource:
		return "on_fire", "death.attack.onFire", nil
	case block.LavaDamageSource:
		return "lava", "death.attack.lava", nil
	}
	return "generic", "death.attack.generic", nil
}

// damageTypes is the damage_type registry the client was sent, by key.
var damageTypes = sync.OnceValue(func() map[string]int32 {
	m := map[string]int32{}
	for _, r := range java261.Registries() {
		if r.ID == "minecraft:damage_type" {
			for i, e := range r.Entries {
				m[e] = int32(i)
			}
		}
	}
	if _, ok := m["minecraft:generic"]; !ok {
		panic("javafront: no minecraft:generic damage type")
	}
	return m
})

func damageType(name string) int32 {
	if id, ok := damageTypes()["minecraft:"+name]; ok {
		return id
	}
	return damageTypes()["minecraft:generic"]
}

// striking notes that an attack by a Java player on e is under way, or with
// nil that it is over.
func (f *Front) striking(e world.Entity, src world.DamageSource) {
	f.hmu.Lock()
	defer f.hmu.Unlock()
	if f.strikes == nil {
		f.strikes = map[*world.EntityHandle]world.DamageSource{}
	}
	if src == nil {
		delete(f.strikes, e.H())
	} else {
		f.strikes[e.H()] = src
	}
}

// hurtBy is what is hurting an entity, as far as the Java front end knows: an
// attack by a Java player under way, or what last hurt a Java player. Nil
// otherwise: Dragonfly does not tell viewers.
func (f *Front) hurtBy(e world.Entity) world.DamageSource {
	f.hmu.Lock()
	src, ok := f.strikes[e.H()]
	f.hmu.Unlock()
	if ok {
		return src
	}
	p, ok := e.(*player.Player)
	if !ok {
		return nil
	}
	f.mu.Lock()
	c, ok := f.names[strings.ToLower(p.Name())]
	f.mu.Unlock()
	if ok && c.id.UUID == p.UUID() {
		return c.hurtBy
	}
	return nil
}

// hurt shows an entity taking damage: damage_event to everyone who sees it, and
// to the client itself its own view tilting away from where the damage came
// from (hurt_animation).
func (c *conn) hurt(e world.Entity) {
	id, ok := c.entityID(e)
	if !ok {
		return
	}
	typ, _, by := javaDamage(c.f.hurtBy(e))
	var cause int32
	if by != nil {
		if bid, ok := c.entityID(by); ok {
			cause = bid + 1
		}
	}
	c.send(javaproto.DamageEvent(id, damageType(typ), cause, cause))
	if e.H() == c.handle {
		var yaw float64
		if by != nil {
			d := by.Position().Sub(e.Position())
			yaw = math.Atan2(d[2], d[0])*180/math.Pi - e.Rotation().Yaw()
		}
		c.send(javaproto.HurtAnimation(selfEntityID, float32(yaw)))
	}
}

// died shows another player dying: the death sound, and the health and pose
// that make Java draw it falling over. Dragonfly tells viewers before the
// health drops, so the health goes as 0 here.
func (c *conn) died(e world.Entity) {
	s, ok := c.ents[e.H()]
	if !ok || !s.player || e.H() == c.handle {
		return
	}
	c.send(javaproto.EntityStatus(s.id, javaproto.StatusDeath))
	s.health = 0
	var m javaproto.Meta
	m.Float(metaHealth, metaTypeFloat, 0)
	m.VarInt(metaPose, metaTypePose, poseDying)
	c.send(javaproto.EntityMetadata(s.id, m))
}

// wrapHandler puts a javaHandler around the player's handler, unless one for
// this client is already there. It runs at join and again every tick, so a
// handler set later (p.Handle by a plugin or the hub) is wrapped by the next
// tick: Dragonfly lets anything replace a player's handler at any time, and
// without the wrap the client would get no death screen. A hurt or death in
// the tick between is shown without its cause or message.
func (c *conn) wrapHandler(p *player.Player) {
	if h, ok := p.Handler().(javaHandler); ok && h.c == c {
		return
	}
	p.Handle(javaHandler{Handler: p.Handler(), c: c})
}
