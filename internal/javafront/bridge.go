package javafront

import (
	"math"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/block/cube/trace"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// Bedrock bridging for Java players (Options.BedrockBridging). Not vanilla for
// Java: a Java client places only on a block face its crosshair is on. A
// Bedrock client also places when the crosshair is on nothing, against the
// block it stands on, and sends that as an ordinary use-item-on-block (the
// block under its feet, a side face); minecraft.wiki's Tutorial:Bridging
// (oldid=3871251, "Simple bridging") and GeyserMC's
// BedrockInventoryTransactionTranslator (which flags exactly that face: the
// one pointing the way the player faces, by 90Â° yaw sectors) describe it. A
// Java client still sends use_item when it uses a block on nothing, so the
// server does what the Bedrock client would have asked for.
//
// The rule, as the sources give it:
//   - the look hits no block within reach (else the client places normally);
//   - the player stands on a block, which is the block clicked;
//   - the face is the player's cardinal facing, by yaw alone;
//   - the block goes next to it, at the level under the feet, and only where
//     the player looks at that cell within reach (the wiki's "look at the
//     position"), so looking up or far away places nothing.
//
// The client did not predict the block, so it appears a round trip late.

// bridgeCooldown is Java's own use cooldown (Minecraft.rightClickDelay, 4
// ticks): a held button repeats use_item that often, and no faster here.
const bridgeCooldown = 4

// supportDepth is how far under the feet the block stood on is looked for:
// Java's Entity.getOnPos looks 0.2 down, past a slab's or carpet's top.
const supportDepth = 0.2

// bridgeSpot is where a Bedrock client bridging would place: the block clicked,
// the face, and the click point on it. ok is false where it would not.
func (c *conn) bridgeSpot(tx *world.Tx, p *player.Player) (on cube.Pos, face cube.Face, click mgl64.Vec3, ok bool) {
	if !p.OnGround() {
		return
	}
	creative := p.GameMode().CreativeInventory()
	reach := c.rules.BlockRange(creative)
	eye, dir := entity.EyePosition(p), p.Rotation().Vec3()
	if _, _, _, hit := c.ray(tx, eye, dir, reach, func(pos cube.Pos) bool {
		switch tx.Block(pos).(type) {
		case block.Air, world.Liquid:
			return false
		}
		return true
	}); hit {
		return // aimed at a block: a client would have placed on it
	}
	on = cube.PosFromVec3(p.Position().Sub(mgl64.Vec3{0, supportDepth, 0}))
	face = p.Rotation().Direction().Face()
	target := on.Side(face)
	if !c.wasSent(on) || !c.wasSent(target) {
		return
	}
	if _, air := tx.Block(on).(block.Air); air {
		return
	}
	if _, opens := tx.Block(on).(block.Activatable); opens && !p.Sneaking() {
		return // bridging off a chest must not open it
	}
	if !c.rules.CanReachBlock(eye, on, creative) || !c.rules.CanReachBlock(eye, target, creative) {
		return
	}
	cell := cube.Box(0, 0, 0, 1, 1, 1).Translate(target.Vec3())
	res, looks := trace.BBoxIntercept(cell, eye, eye.Add(dir.Mul(reach)))
	if !looks {
		return
	}
	// The point where the look meets the cell, as a click on the face of the
	// block stood on that the cell lies against.
	click = res.Position().Sub(on.Vec3())
	for i := range click {
		click[i] = math.Min(math.Max(click[i], 0), 1)
	}
	switch face {
	case cube.FaceNorth:
		click[2] = 0
	case cube.FaceSouth:
		click[2] = 1
	case cube.FaceWest:
		click[0] = 0
	case cube.FaceEast:
		click[0] = 1
	}
	return on, face, click, true
}

// bridge places the block held in hand as a Bedrock client bridging would, and
// reports whether it tried: the block used, nothing else is.
func (c *conn) bridge(p *player.Player, hand int32) bool {
	if !c.f.opts.BedrockBridging {
		return false
	}
	held := handItem(p, hand)
	if _, isBlock := held.Item().(world.Block); !isBlock || held.Empty() {
		return false
	}
	tx := p.Tx()
	now := tx.CurrentTick()
	// Ticks are a world's own: after a change of world the difference may be
	// negative, which is no cooldown.
	if d := now - c.bridgedAt; c.bridged && d >= 0 && d < bridgeCooldown {
		return true // a repeat sooner than Java's: used up, as the client expects
	}
	on, face, click, ok := c.bridgeSpot(tx, p)
	if !ok {
		return false
	}
	c.bridged, c.bridgedAt = true, now
	c.withHand(p, hand, func() { p.UseItemOnBlock(on, face, click) })
	if handItem(p, hand).Equal(held) {
		c.resendHand(hand)
	}
	return true
}
