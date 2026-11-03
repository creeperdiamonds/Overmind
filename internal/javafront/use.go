package javafront

import (
	"math"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
)

// cursorSlack is how far outside its block a click point may be: the client
// writes floats, and an edge can round past 1.
const cursorSlack = 1e-3

// useOn handles block_place, Java's "use item on a block". It goes through
// Dragonfly's own item use, so what is placed, how it faces and what it
// replaces are the server's. Whatever happens, the true blocks go out before
// the acknowledgement: the client takes back what it guessed when acknowledged.
func (c *conn) useOn(p *player.Player, u javaproto.BlockPlace) {
	defer func() {
		c.flushWorld()
		c.send(javaproto.AckDig(u.Sequence))
	}()
	pos, face := cube.Pos{u.X, u.Y, u.Z}, cube.Face(u.Face)
	held := handItem(p, u.Hand)
	used := false
	// Whatever is refused or uses nothing up, the client took one for its
	// guess, so the hand's slot goes again.
	defer func() {
		if !used || handItem(p, u.Hand).Equal(held) {
			c.resendHand(u.Hand)
		}
	}()
	// Only blocks the client was sent, and the block next to them that a place
	// may fill: looking one up would load its chunk.
	if !c.wasSent(pos) || !c.wasSent(pos.Side(face)) {
		return
	}
	if !c.rules.CanReachBlock(entity.EyePosition(p), pos, p.GameMode().CreativeInventory()) {
		return
	}
	_, opens := p.Tx().Block(pos).(block.Activatable)
	opens = opens && !p.Sneaking()
	if u.Hand == javaproto.HandOff && opens {
		// The client sends the off hand only when the main hand did nothing, and
		// the main hand has already opened what opens.
		return
	}
	if _, bucket := held.Item().(item.Bucket); bucket && !opens {
		// Java uses a bucket in the air (use_item follows), finding the fluid
		// itself. On a block it only opens what opens.
		return
	}
	click := mgl64.Vec3{float64(u.CursorX), float64(u.CursorY), float64(u.CursorZ)}
	for i := range click {
		click[i] = math.Min(math.Max(click[i], 0), 1)
	}
	used = true
	c.withHand(p, u.Hand, func() { p.UseItemOnBlock(pos, face, click) })
}

// handItem is what the player holds in a hand, as Java numbers them.
func handItem(p *player.Player, hand int32) item.Stack {
	main, off := p.HeldItems()
	if hand == javaproto.HandOff {
		return off
	}
	return main
}

// withHand runs f, which uses the main hand, with the item of the hand given.
// Dragonfly uses items from the main hand only; for the off hand its item is
// lent to the main hand while f runs, and what is left of it goes back.
// Dragonfly's off hand holds only Bedrock's off-hand items (arrows, fireworks,
// totems, nautilus shells), so in practice this launches fireworks. Viewers
// and the client are shown the hands once, after.
func (c *conn) withHand(p *player.Player, hand int32, f func()) {
	if hand != javaproto.HandOff {
		f()
		return
	}
	main, off := p.HeldItems()
	if off.Empty() {
		return
	}
	c.invs.quiet, c.invs.lending = true, true
	p.SetHeldItems(off, item.Stack{})
	f()
	left, extra := p.HeldItems()
	p.SetHeldItems(main, left)
	if _, got := p.HeldItems(); !got.Equal(left) {
		// The off hand does not take what the item turned into: keep it anyway.
		extra = left
		_ = c.invs.offHand.SetItem(0, item.Stack{})
	}
	if !extra.Empty() {
		if n, _ := p.Inventory().AddItem(extra); n < extra.Count() {
			p.Drop(extra.Grow(-n))
		}
	}
	c.invs.quiet, c.invs.lending = false, false
	c.sendInventory()
	for _, v := range p.Tx().Viewers(p.Position()) {
		v.ViewEntityItems(p)
	}
}

// javaLook is a look as the server keeps it: yaw wrapped to -180 to 180 and
// pitch held to -90 (up) to 90 (down), the ranges minecraft.wiki's protocol
// page (oldid=3543069, "Set Player Rotation") gives. A client may send anything.
// The pitch is held just inside ±90: Dragonfly's cube.Rotation wraps exactly 90
// to -90 (its fix), which would turn a player looking straight down to look up.
func javaLook(yaw, pitch float64) (float64, float64) {
	yaw = math.Mod(yaw, 360)
	if yaw >= 180 {
		yaw -= 360
	} else if yaw < -180 {
		yaw += 360
	}
	return yaw, math.Max(lowestPitch, math.Min(highestPitch, pitch))
}

var (
	highestPitch = 90 - 1e-7
	lowestPitch  = -90 + 1e-7
)

// validCursor reports whether a click point is inside its block.
func validCursor(u javaproto.BlockPlace) bool {
	for _, v := range []float32{u.CursorX, u.CursorY, u.CursorZ} {
		f := float64(v)
		if math.IsNaN(f) || f < -cursorSlack || f > 1+cursorSlack {
			return false
		}
	}
	return true
}

// useInAir handles use_item: eating, drinking, throwing, drawing a bow,
// buckets, which Java aims at fluids itself, and blocks, which bridge as on
// Bedrock (bridge.go).
func (c *conn) useInAir(p *player.Player, u javaproto.UseItem) {
	defer func() {
		c.flushWorld()
		c.send(javaproto.AckDig(u.Sequence))
	}()
	// The look the client used it with.
	c.turnTo(p, float64(u.Yaw), float64(u.Pitch))
	held := handItem(p, u.Hand)
	if c.bridge(p, u.Hand) {
		return
	}
	if b, ok := held.Item().(item.Bucket); ok {
		c.withHand(p, u.Hand, func() { c.useBucket(p, b) })
		return
	}
	c.withHand(p, u.Hand, p.UseItem)
	if p.UsingItem() {
		c.usingSince = time.Now()
	}
	if after := handItem(p, u.Hand); after.Equal(held) && !p.UsingItem() {
		c.resendHand(u.Hand) // the client may have guessed one was used
	}
}

// turnTo turns the player to a look the client sent, as the server keeps it.
func (c *conn) turnTo(p *player.Player, yaw, pitch float64) {
	yaw, pitch = javaLook(yaw, pitch)
	y, pt := p.Rotation().Elem()
	p.Move(mgl64.Vec3{}, yaw-y, pitch-pt)
}

// finishEating completes food and drink the client is still using. A Java
// client sends nothing when it is done, and stops: the server finishes it.
// Bedrock clients ask again instead, which is what UseItem waits for.
func (c *conn) finishEating(p *player.Player) {
	if !p.UsingItem() {
		return
	}
	held, _ := p.HeldItems()
	food, ok := held.Item().(item.Consumable)
	if !ok || time.Since(c.usingSince) < food.ConsumeDuration() {
		return
	}
	p.UseItem()
	p.ReleaseItem()
}

// useBucket fills or empties a bucket where the player looks, as Java's bucket
// does: an empty one takes the first fluid source on the way, a full one empties
// against the first block. Fluids are passed through otherwise.
func (c *conn) useBucket(p *player.Player, b item.Bucket) {
	tx := p.Tx()
	reach := c.rules.BlockRange(p.GameMode().CreativeInventory())
	pos, face, hit, ok := c.ray(tx, entity.EyePosition(p), p.Rotation().Vec3(), reach, func(pos cube.Pos) bool {
		bl := tx.Block(pos)
		if _, air := bl.(block.Air); air {
			return false
		}
		if l, liquid := bl.(world.Liquid); liquid {
			return b.Empty() && l.LiquidDepth() == 8 && !l.LiquidFalling()
		}
		return true
	})
	if !ok {
		return
	}
	p.UseItemOnBlock(pos, face, hit.Sub(pos.Vec3()))
}

// ray walks from eye along dir for up to reach blocks, through blocks the
// client was sent, and returns the first block stop accepts, the face the ray
// came in by and the point it hit. Blocks count as whole cubes.
func (c *conn) ray(tx *world.Tx, eye, dir mgl64.Vec3, reach float64, stop func(cube.Pos) bool) (cube.Pos, cube.Face, mgl64.Vec3, bool) {
	const step = 0.01
	last := cube.PosFromVec3(eye)
	for d := 0.0; d <= reach; d += step {
		at := eye.Add(dir.Mul(d))
		pos := cube.PosFromVec3(at)
		if pos == last {
			continue
		}
		face, ok := entered(last, pos)
		last = pos
		if !ok || !c.wasSent(pos) || pos.OutOfBounds(tx.Range()) {
			return cube.Pos{}, 0, mgl64.Vec3{}, false
		}
		if stop(pos) {
			return pos, face, at, true
		}
	}
	return cube.Pos{}, 0, mgl64.Vec3{}, false
}

// entered is the face of to that a ray from the neighbouring block from came in
// by. A step to a block that is not a neighbour (through an edge) counts as its
// first changed axis.
func entered(from, to cube.Pos) (cube.Face, bool) {
	switch {
	case to[0] > from[0]:
		return cube.FaceWest, true
	case to[0] < from[0]:
		return cube.FaceEast, true
	case to[1] > from[1]:
		return cube.FaceDown, true
	case to[1] < from[1]:
		return cube.FaceUp, true
	case to[2] > from[2]:
		return cube.FaceNorth, true
	case to[2] < from[2]:
		return cube.FaceSouth, true
	}
	return 0, false
}

// wasSent reports whether the chunk holding pos was sent to the client.
func (c *conn) wasSent(pos cube.Pos) bool {
	return c.chunkSent(world.ChunkPos{int32(pos[0] >> 4), int32(pos[2] >> 4)})
}

// resendHand sends a hand's slot again, for a client that guessed it changed.
func (c *conn) resendHand(hand int32) {
	if c.invs.inv == nil {
		return
	}
	if hand == javaproto.HandOff {
		st, _ := c.invs.offHand.Item(0)
		c.send(javaproto.SetSlot(0, c.nextState(), slotOffHand, javaItem(st)))
		return
	}
	slot := int(*c.invs.held)
	st, _ := c.invs.inv.Item(slot)
	c.send(javaproto.SetSlot(0, c.nextState(), int16(javaInventorySlot(slot)), javaItem(st)))
}
