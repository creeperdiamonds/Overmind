package javafront

import (
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/google/uuid"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
	"github.com/creeperdiamonds/overmind/internal/skins"
)

// Bedrock players' skins, as Java viewers see them (internal/skins says why).
//
// A Java client draws another player in a skin from a textures property Mojang
// signed, else in the default skin its UUID picks. So Java viewers know a
// Bedrock player by a UUID of its own (skins.ViewUUID: the same for the same
// account and skin every session), chosen by tier:
//
//   - (a) a Bedrock skin whose image is one of the default skins both editions
//     share, on the same model: the UUID makes the Java client pick that very
//     skin, nothing uploaded.
//   - (b) another Java-format skin (skins.Classic, or ClassicExtras with the
//     extras dropped), when an uploader is set up: once uploaded, the signed
//     textures go in the player's row, and the player is shown again with
//     them.
//   - (c) everything else, and (b) while waiting or after a failure: Steve for
//     the wide model, Alex for the slim one, by the UUID.
//
// When a Bedrock player's look changes (a skin change, an upload done), each
// Java client's row is replaced and the player's entity spawned again, since a
// Java client reads a player's skin from its row when the entity appears.
// Bedrock viewers are not touched: Dragonfly sends them the skin as the client
// sent it.

// viewLook is how Java viewers are shown a player: its UUID and textures.
type viewLook struct {
	uuid                uuid.UUID
	textures, signature string
}

// bedrockLook is what the Front worked out for a Bedrock player's skin.
type bedrockLook struct {
	key    string // the skin it was worked out for: kind, model and hash
	ref    skinRef
	look   viewLook
	handle *world.EntityHandle
	// index is the default skin the UUID picks.
	index int
}

// skinRef tells one skin value from another without reading it: Dragonfly
// hands out the same slices until the skin changes. It saves sorting and
// hashing the skin again each time a Java client is shown the player.
type skinRef struct {
	pix, model   *byte
	npix, nmodel int
	def, face    string
}

func refOf(s skin.Skin) skinRef {
	r := skinRef{npix: len(s.Pix), nmodel: len(s.Model), def: s.ModelConfig.Default, face: s.ModelConfig.AnimatedFace}
	if len(s.Pix) > 0 {
		r.pix = &s.Pix[0]
	}
	if len(s.Model) > 0 {
		r.model = &s.Model[0]
	}
	return r
}

// standInHash is the hash of our stand-in skin, which Java viewers are shown
// as Steve or Alex rather than uploaded.
var standInHash = skins.Hash(skins.StandIn(false).Pix)

// lookOf is how Java viewers are shown a player: a Java player by its own
// account and Mojang textures, a Bedrock one by its skin.
func (f *Front) lookOf(p *player.Player) viewLook {
	if c := f.javaConn(p.UUID()); c != nil {
		return viewLook{uuid: c.id.UUID, textures: c.id.Textures, signature: c.id.TexturesSignature}
	}
	return f.bedrockView(p.UUID(), p.H(), p.Skin())
}

// javaConn is the Java client of an account, nil if it has none.
func (f *Front) javaConn(id uuid.UUID) *conn {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.names {
		if c.id.UUID == id {
			return c
		}
	}
	return nil
}

// bedrockView works out a Bedrock player's look from its skin, keeps it, and
// replaces the player's row in every Java client's list if it changed.
func (f *Front) bedrockView(account uuid.UUID, h *world.EntityHandle, s skin.Skin) viewLook {
	ref := refOf(s)
	f.bmu.Lock()
	if b := f.blooks[account]; b != nil && b.ref == ref && ref.pix != nil {
		if h != nil {
			b.handle = h
		}
		look := b.look
		f.bmu.Unlock()
		return look
	}
	f.bmu.Unlock()

	l := skins.Classify(s)
	key := l.Kind.String()
	if l.Slim {
		key += "/slim"
	}
	key += "/" + l.Hash

	f.bmu.Lock()
	b := f.blooks[account]
	if b != nil && b.key == key {
		b.ref = ref
		if h != nil {
			b.handle = h
		}
		look := b.look
		f.bmu.Unlock()
		return look
	}
	f.bmu.Unlock()

	index := skins.Fallback(l.Slim)
	var signed skins.Signed
	upload := false
	if l.JavaFormat() && l.Hash != standInHash {
		if i, ok := skins.DefaultIndex(l.Hash, l.Slim); ok {
			index = i
		} else if f.opts.Uploader != nil {
			if sg, ok := f.opts.Uploader.Signed(l.Hash, l.Slim); ok {
				signed = sg
			} else {
				upload = true
			}
		}
	}
	look := viewLook{uuid: skins.ViewUUID(account, index, f.accountTaken), textures: signed.Value, signature: signed.Signature}

	f.bmu.Lock()
	b = f.blooks[account]
	if b == nil {
		b = &bedrockLook{}
		f.blooks[account] = b
	}
	b.key, b.ref, b.look, b.index = key, ref, look, index
	if h != nil {
		b.handle = h
	}
	f.bmu.Unlock()
	f.opts.Log.Debug("bedrock skin for java viewers", "account", account, "kind", l.Kind, "default", skins.DefaultName(index), "signed", signed.Value != "", "upload", upload)

	f.replaceRow(account, look)
	if upload {
		hash, slim := l.Hash, l.Slim
		f.opts.Uploader.Request(l.Hash, l.Image, l.Slim, func(sg skins.Signed) { f.signedArrived(account, key, hash, slim, sg) })
	}
	return look
}

// accountTaken reports whether a Java player online has this account UUID, so
// a view UUID never matches one.
func (f *Front) accountTaken(id uuid.UUID) bool { return f.javaConn(id) != nil }

// signedArrived puts an uploaded skin's signed textures on the Bedrock player
// still wearing it, and shows the player again to Java clients that see it.
func (f *Front) signedArrived(account uuid.UUID, key, hash string, slim bool, sg skins.Signed) {
	f.bmu.Lock()
	b := f.blooks[account]
	if b == nil || b.key != key {
		f.bmu.Unlock()
		return // the skin has changed since, or the player left
	}
	b.look.textures, b.look.signature = sg.Value, sg.Signature
	look, h := b.look, b.handle
	f.bmu.Unlock()
	f.replaceRow(account, look)
	if h == nil {
		return
	}
	player.Do(h, func(tx *world.Tx, p *player.Player) {
		for _, v := range tx.Viewers(p.Position()) {
			if c, ok := v.(*conn); ok {
				c.ViewSkin(p)
			}
		}
	})
}

// forgetBedrock drops what the Front kept of a Bedrock player that left.
func (f *Front) forgetBedrock(account uuid.UUID) {
	f.bmu.Lock()
	delete(f.blooks, account)
	f.bmu.Unlock()
}

// replaceRow puts a new look in a listed player's row, in the shared list and
// in each Java client's.
func (f *Front) replaceRow(account uuid.UUID, look viewLook) {
	f.mu.Lock()
	defer f.mu.Unlock()
	info, ok := f.tab[account]
	if !ok {
		return
	}
	old := info.UUID
	info.UUID, info.Textures, info.TexturesSignature = look.uuid, look.textures, look.signature
	if info == f.tab[account] {
		return
	}
	f.tab[account] = info
	for _, c := range f.names {
		c.swapInfo(old, info)
	}
}

// swapInfo replaces a row in the client's list: the old one goes, the new one
// comes, unless the client has it already as it is.
func (c *conn) swapInfo(old uuid.UUID, e javaproto.PlayerInfoEntry) {
	c.lmu.Lock()
	defer c.lmu.Unlock()
	if !c.tabOpen {
		return
	}
	if have, ok := c.rows[e.UUID]; ok && have == e {
		return
	}
	if _, ok := c.rows[old]; ok {
		delete(c.rows, old)
		c.send(javaproto.PlayerRemove(old))
	}
	if _, ok := c.rows[e.UUID]; ok {
		c.send(javaproto.PlayerRemove(e.UUID))
	}
	c.rows[e.UUID] = e
	c.send(javaproto.PlayerInfoAdd(e))
}

// ViewSkin follows a player's skin change. Bedrock players' looks for Java
// change with their skins (and when an upload is done); a player shown with
// another look is shown again. A Java player's own skin reaches Java viewers
// through its Mojang textures, which do not change.
func (c *conn) ViewSkin(e world.Entity) {
	p, ok := e.(*player.Player)
	if !ok || e.H() == c.handle {
		return
	}
	look := c.f.lookOf(p)
	s, ok := c.ents[e.H()]
	if !ok || s.look == look {
		return
	}
	// Its row first, as the client reads the skin from the row when the
	// entity spawns. A listed player's row has been replaced already.
	c.swapInfo(s.uuid, c.infoFor(p, look))
	c.send(javaproto.EntitiesRemoved(s.id))
	s.uuid, s.look = look.uuid, look
	pos, rot := p.Position(), p.Rotation()
	s.x, s.y, s.z, s.yaw = fixed(pos[0]), fixed(pos[1]), fixed(pos[2]), rot.Yaw()
	c.send(javaproto.SpawnEntity(s.id, look.uuid, playerEntity, pos[0], pos[1], pos[2], rot.Yaw(), rot.Pitch(), [3]float64{}))
	c.send(javaproto.EntityMetadata(s.id, c.playerMeta(p)))
	c.ViewEntityItems(p)
	c.ViewEntityArmour(p)
}

// infoFor is a player's row: the shared list's if it is listed, else one made
// for the entity.
func (c *conn) infoFor(p *player.Player, look viewLook) javaproto.PlayerInfoEntry {
	c.f.mu.Lock()
	info, ok := c.f.tab[p.UUID()]
	c.f.mu.Unlock()
	if ok {
		return info
	}
	return javaproto.PlayerInfoEntry{UUID: look.uuid, Name: javaName(p.Name()), GameMode: gameModeID(p.GameMode()),
		Textures: look.textures, TexturesSignature: look.signature}
}
