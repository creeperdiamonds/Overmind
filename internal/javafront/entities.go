package javafront

import (
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/tables/java261"
)

// Entity types and metadata, by minecraft-data's names (java261).
var (
	itemEntity = java261.EntityType("item")

	metaItem      = metaIndex("item", "item")
	metaItemStack = java261.MetadataType("item_stack")
)

// metaIndex is a metadata index from the tables. One missing is a build error.
func metaIndex(entity, key string) byte {
	i := java261.MetadataIndex(entity, key)
	if i < 0 {
		panic("javafront: no metadata " + key + " for " + entity)
	}
	return byte(i)
}

// showItem shows an item entity as Java's own server would: the entity, then
// its item, then its velocity.
func (c *conn) showItem(e *entity.Ent) {
	b, ok := e.Behaviour().(*entity.ItemBehaviour)
	if !ok {
		return
	}
	id := c.nextEnt
	c.nextEnt++
	pos, vel := e.Position(), e.Velocity()
	c.ents[e.H()] = &shown{id: id, uuid: e.H().UUID(), x: fixed(pos[0]), y: fixed(pos[1]), z: fixed(pos[2])}
	c.send(javaproto.SpawnEntity(id, e.H().UUID(), itemEntity, pos[0], pos[1], pos[2], 0, 0, vel))
	var m javaproto.Meta
	m.Item(metaItem, metaItemStack, javaItem(b.Item()))
	c.send(javaproto.EntityMetadata(id, m))
	c.send(javaproto.EntityVelocity(id, vel[0], vel[1], vel[2]))
}

// entityID is the ID this client knows an entity by: its own player's, or a
// shown entity's.
func (c *conn) entityID(e world.Entity) (int32, bool) {
	if e.H() == c.handle {
		return selfEntityID, true
	}
	s, ok := c.ents[e.H()]
	if !ok {
		return 0, false
	}
	return s.id, true
}

// ViewEntityVelocity sets the velocity of a shown entity, or of the client's own
// player, which is how a push (knockback) reaches it.
func (c *conn) ViewEntityVelocity(e world.Entity, vel mgl64.Vec3) {
	if !c.viewing() {
		return
	}
	if id, ok := c.entityID(e); ok {
		c.send(javaproto.EntityVelocity(id, vel[0], vel[1], vel[2]))
	}
}

// ViewEntityAction shows what an entity did.
func (c *conn) ViewEntityAction(e world.Entity, a world.EntityAction) {
	if !c.viewing() {
		return
	}
	switch act := a.(type) {
	case entity.SwingArmAction:
		if e.H() == c.handle {
			c.swungAt = time.Now() // the client swings its own
			return
		}
		if s, ok := c.ents[e.H()]; ok {
			c.send(javaproto.Animation(s.id, javaproto.AnimationSwingMainArm))
		}
	case entity.HurtAction:
		c.hurt(e)
	case entity.CriticalHitAction:
		if s, ok := c.ents[e.H()]; ok {
			c.send(javaproto.Animation(s.id, javaproto.AnimationCritical))
		}
	case entity.EnchantedHitAction:
		if s, ok := c.ents[e.H()]; ok {
			c.send(javaproto.Animation(s.id, javaproto.AnimationMagicCritical))
		}
	case entity.DeathAction:
		c.died(e)
	case entity.PickedUpAction:
		// The item entity goes right after, which removes it.
		// The count is what was taken (the fork's PickedUpAction.Count): a full
		// inventory takes part of a stack, and the rest stays as a new entity.
		item, ok := c.ents[e.H()]
		collector, known := c.entityID(act.Collector)
		if !ok || !known {
			return
		}
		c.send(javaproto.Collect(item.id, collector, int32(max(act.Count, 1))))
	}
}
