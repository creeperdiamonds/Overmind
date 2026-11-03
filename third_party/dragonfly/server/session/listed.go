package session

// Overmind fork: this whole file is new.

import (
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// listedPlayer is a player with no Session that Bedrock players list anyway.
type listedPlayer struct {
	name string
	skin skin.Skin
}

// listed holds the players added by List. It is guarded by sessions.mu, so a
// session joining and a player being listed cannot miss each other.
var listed = map[*world.EntityHandle]listedPlayer{}

// List puts a player that plays through another front end, and so has no
// Session, in the player list of every Bedrock player, now and for those who
// join later, as a player with a Session is. Unlist takes it out again, and
// must be called after the player's entity has left its world: it forgets the
// runtime IDs the sessions gave it.
func List(h *world.EntityHandle, name string, sk skin.Skin) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	lp := listedPlayer{name: name, skin: sk}
	listed[h] = lp
	for _, s := range sessions.s {
		sendListedTo(h, lp, s)
	}
}

// SetListedSkin gives a listed player a new skin in the list rows of every
// Bedrock player, now and for those who join later; it does nothing for a
// player List did not add. Call it as the player's skin changes. A row carries
// a skin with OverrideAppearance, and a Bedrock client draws the player from
// its row, so a row left with an older skin keeps it drawn in that skin
// whatever PlayerSkin then says. The row is taken out and put back, as
// PocketMine did for a skin change, since adding a UUID already listed is not
// sure to replace its skin.
func SetListedSkin(h *world.EntityHandle, sk skin.Skin) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	lp, ok := listed[h]
	if !ok {
		return
	}
	lp.skin = sk
	listed[h] = lp
	for _, s := range sessions.s {
		s.writePacket(&packet.PlayerList{Entries: []protocol.PlayerListEntry{{
			ActionType: protocol.PlayerListActionRemove,
			UUID:       h.UUID(),
		}}})
		sendListedTo(h, lp, s)
	}
}

// Unlist reverses List.
func Unlist(h *world.EntityHandle) {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	if _, ok := listed[h]; !ok {
		return
	}
	delete(listed, h)
	for _, s := range sessions.s {
		s.entityMutex.Lock()
		if id, ok := s.entityRuntimeIDs[h]; ok {
			delete(s.entities, id)
			delete(s.entityRuntimeIDs, h)
		}
		s.entityMutex.Unlock()
		s.writePacket(&packet.PlayerList{Entries: []protocol.PlayerListEntry{{
			ActionType: protocol.PlayerListActionRemove,
			UUID:       h.UUID(),
		}}})
	}
}

// isListed reports whether List added this player.
func isListed(h *world.EntityHandle) bool {
	sessions.mu.Lock()
	defer sessions.mu.Unlock()
	_, ok := listed[h]
	return ok
}

// sendListedTo shows one listed player to a session. The caller holds
// sessions.mu.
func sendListedTo(h *world.EntityHandle, lp listedPlayer, to *Session) {
	to.entityMutex.Lock()
	runtimeID, ok := to.entityRuntimeIDs[h]
	if !ok {
		to.currentEntityRuntimeID += 1
		runtimeID = to.currentEntityRuntimeID
		to.entityRuntimeIDs[h] = runtimeID
		to.entities[runtimeID] = h
	}
	to.entityMutex.Unlock()
	to.writePacket(&packet.PlayerList{Entries: []protocol.PlayerListEntry{{
		ActionType:     protocol.PlayerListActionAdd,
		UUID:           h.UUID(),
		EntityUniqueID: int64(runtimeID),
		Username:       lp.name,
		BuildPlatform:  int32(protocol.DeviceUnknown),
		Skin:           skinToProtocol(lp.skin),
	}}})
}
