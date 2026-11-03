package javafront

import (
	"maps"
	"slices"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/tables/java261"
)

// javaDimension is what a Java client is told of a dimension: its name, its
// place in the dimension_type registry it was sent in configuration, its sea
// level, whether it has sky light and its height in sections, from
// java261.DimensionType (minecraft-data's loginPacket.json; the sea levels
// written by hand from minecraft.wiki). Dragonfly's dimensions start at the same height;
// Bedrock's Nether is 128 blocks tall, Java's 256 (encodeChunk pads it).
type javaDimension struct {
	name     string
	typ      func() int32
	seaLevel int32
	skyLight bool
	sections int // sections of 16 blocks in Java's height
}

func newDimension(name string, typ func() int32) javaDimension {
	d, ok := java261.DimensionType(name)
	if !ok {
		panic("javafront: no dimension type " + name + " in the tables")
	}
	return javaDimension{name, typ, d.SeaLevel, d.SkyLight, d.Height / 16}
}

var (
	overworldDim = newDimension(overworldName, overworldDimension)
	netherDim    = newDimension("minecraft:the_nether", registryIndex("minecraft:dimension_type", "minecraft:the_nether"))
	endDim       = newDimension("minecraft:the_end", registryIndex("minecraft:dimension_type", "minecraft:the_end"))
)

// dimensionOf is the Java dimension of a Dragonfly one.
func dimensionOf(d world.Dimension) javaDimension {
	switch d {
	case world.Nether:
		return netherDim
	case world.End:
		return endDim
	}
	return overworldDim
}

// Respawn data kept on a change of dimension: attributes and metadata, as the
// player is the same one.
const keepAll = 0x03

// leaveWorld starts following the player into another world (another
// dimension, or another world of the same one), as the Bedrock session does
// when its loader is in another world than its player. The world left keeps
// calling the conn, on its own goroutine, until it has run the loader's
// removal from its chunks, which Loader.ChangeWorld queues there. So the conn
// takes no world's callbacks (away, see viewing) until a task queued after
// that removal has run (oldWorldsDone); then arrive shows the new world.
func (c *conn) leaveWorld(tx *world.Tx, w *world.World) {
	old := c.loader.World()
	// away first, then viewW: viewing reads them the other way round.
	c.away.Store(true)
	c.closeWindow(tx, false)
	c.sign = nil
	c.loader.ChangeWorld(tx, w)
	c.viewW.Store(w)
	c.leaving = append(c.leaving, old.Do(func(*world.Tx) {}))
}

// oldWorldsDone reports whether every world the player left has run its task
// queued by leaveWorld, or failed it by closing.
func (c *conn) oldWorldsDone() bool {
	for _, t := range c.leaving {
		select {
		case <-t.Done():
		default:
			return false
		}
	}
	return true
}

// arrive shows the client the world the player is in now: a new level
// (Respawn, with the new dimension's type from the registries it was sent),
// then its position and the chunks of the new world. Everything it was shown
// of the old one goes: Java forgets a level's entities and chunks on a
// respawn. The worlds left behind call the conn no more, so it takes
// callbacks again.
func (c *conn) arrive(tx *world.Tx, p *player.Player) {
	w := tx.World()
	c.leaving = nil
	c.away.Store(false)
	c.reshow = false // the respawn below shows everything again
	c.cmu.Lock()
	clear(c.sent)
	clear(c.pending)
	c.pending = c.pending[:0]
	c.cmu.Unlock()
	clear(c.dirty)
	clear(c.edits)
	for h, s := range c.ents {
		delete(c.ents, h)
		if s.player && !c.f.listed(s.account) {
			c.hideInfo(s.uuid)
		}
	}
	c.dim = dimensionOf(w.Dimension())
	pos := p.Position()
	c.send(javaproto.Respawn(c.loginInfo(gameModeID(p.GameMode())), keepAll))
	c.send(javaproto.GameEvent(javaproto.EventStartWaitingForChunks, 0))
	spawn := w.Spawn()
	c.send(javaproto.SpawnPosition(c.dim.name, spawn[0], spawn[1], spawn[2]))
	c.center = chunkPosOf(pos)
	c.send(javaproto.CenterChunk(c.center[0], c.center[1]))
	c.teleportSelf(pos, p.Rotation())
	c.sendHealth()
	c.sendInventory()
	c.send(javaproto.HeldSlot(int32(*c.invs.held)))
	c.send(javaproto.EntityMetadata(selfEntityID, c.selfMeta(p)))
	c.loader.Move(tx, pos)
}

// showAgain shows the client again what it was shown before a respawn in the
// same world: a Java client's new level starts with no entities and no
// chunks. The chunks go again through the paced path, nearest first; the
// entities at once, with what they hold and wear.
func (c *conn) showAgain(tx *world.Tx) {
	c.cmu.Lock()
	sent := slices.Collect(maps.Keys(c.sent))
	c.cmu.Unlock()
	for _, pos := range sent {
		c.dirty[pos] = struct{}{}
	}
	for h, s := range c.ents {
		delete(c.ents, h)
		e, ok := h.Entity(tx)
		if !ok {
			if s.player && !c.f.listed(s.account) {
				c.hideInfo(s.uuid)
			}
			continue
		}
		c.ViewEntity(e)
		c.ViewEntityItems(e)
		c.ViewEntityArmour(e)
	}
	// And any entity of the chunks shown that the conn has not got: a respawn
	// takes the player out of its world for a moment, and the conn drops the
	// world's callbacks while it is in none (viewing), entities that came then
	// included.
	for _, pos := range sent {
		col, ok := c.loader.Chunk(pos)
		if !ok {
			continue
		}
		for _, h := range col.Entities {
			if _, shown := c.ents[h]; shown || h == c.handle {
				continue
			}
			if e, ok := h.Entity(tx); ok {
				c.ViewEntity(e)
				c.ViewEntityItems(e)
				c.ViewEntityArmour(e)
			}
		}
	}
}
