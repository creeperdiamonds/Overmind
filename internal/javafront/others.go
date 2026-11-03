package javafront

import (
	"sync"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// Entities other than players and items: whatever Dragonfly has (arrows and
// other projectiles, falling blocks, TNT, experience orbs, fireworks, end
// crystals, lightning, mobs when it has them) is spawned as the Java entity
// of the same kind (java261.JavaEntityOf, from minecraft-data's entity
// lists), moves and is removed as any shown entity does, and carries the
// metadata Java draws it by. A kind Java has no counterpart for is not shown,
// and logged once.

// Metadata of other entities, by minecraft-data's indexes; -1 when the tables
// lack one, which then is not sent.
var (
	metaTNTFuse   = java261.MetadataIndex("tnt", "fuse")
	metaOrbValue  = java261.MetadataIndex("experience_orb", "value")
	metaTypeInt   = java261.MetadataType("int")
	unknownLogged sync.Map // Bedrock entity names already logged as not shown
)

// showEntity shows any entity but a player or an item.
func (c *conn) showEntity(e world.Entity) {
	name := e.H().Type().EncodeEntity()
	javaName, typ := java261.JavaEntityOf(name)
	if typ < 0 {
		if _, seen := unknownLogged.LoadOrStore(name, true); !seen {
			c.f.opts.Log.Info("java clients are not shown this entity: Java has no counterpart", "entity", name)
		}
		return
	}
	id := c.nextEnt
	c.nextEnt++
	pos, rot := e.Position(), e.Rotation()
	var vel [3]float64
	if v, ok := e.(interface{ Velocity() mgl64.Vec3 }); ok {
		vel = v.Velocity()
	}
	c.ents[e.H()] = &shown{id: id, uuid: e.H().UUID(), yaw: rot.Yaw(), x: fixed(pos[0]), y: fixed(pos[1]), z: fixed(pos[2])}
	c.send(javaproto.SpawnEntityData(id, e.H().UUID(), typ, pos[0], pos[1], pos[2], rot.Yaw(), rot.Pitch(), vel, c.spawnData(e)))
	var m javaproto.Meta
	if ent, ok := e.(*entity.Ent); ok {
		switch b := ent.Behaviour().(type) {
		case *entity.PassiveBehaviour:
			if javaName == "tnt" && metaTNTFuse >= 0 {
				m.VarInt(byte(metaTNTFuse), metaTypeInt, int32(b.Fuse()/tick))
			}
		case *entity.ExperienceOrbBehaviour:
			if metaOrbValue >= 0 {
				m.VarInt(byte(metaOrbValue), metaTypeInt, int32(b.Experience()))
			}
		}
	}
	if len(m.B) > 0 {
		c.send(javaproto.EntityMetadata(id, m))
	}
	if vel != ([3]float64{}) {
		c.send(javaproto.EntityVelocity(id, vel[0], vel[1], vel[2]))
	}
}

// spawnData is the spawn packet's data for an entity: a falling block's Java
// block state, a projectile's owner as this client knows it, else 0.
func (c *conn) spawnData(e world.Entity) int32 {
	ent, ok := e.(*entity.Ent)
	if !ok {
		return 0
	}
	switch b := ent.Behaviour().(type) {
	case *entity.FallingBlockBehaviour:
		return int32(java261.JavaState(world.BlockRuntimeID(b.Block())))
	case *entity.ProjectileBehaviour:
		return c.ownerID(b.Owner())
	case *entity.FireworkBehaviour:
		return c.ownerID(b.Owner())
	}
	return 0
}

// ownerID is the ID this client knows an entity's owner by, 0 if none.
func (c *conn) ownerID(h *world.EntityHandle) int32 {
	if h == nil {
		return 0
	}
	if h == c.handle {
		return selfEntityID
	}
	if s, ok := c.ents[h]; ok {
		return s.id
	}
	return 0
}
