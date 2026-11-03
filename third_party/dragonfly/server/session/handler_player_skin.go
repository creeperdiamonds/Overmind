package session

import (
	"fmt"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// PlayerSkinHandler handles the PlayerSkin packet.
type PlayerSkinHandler struct{}

// Handle ...
func (PlayerSkinHandler) Handle(p packet.Packet, s *Session, _ *world.Tx, c Controllable) error {
	pk := p.(*packet.PlayerSkin)

	playerSkin, err := protocolToSkin(pk.Skin)
	if err != nil {
		return fmt.Errorf("error decoding skin: %w", err)
	}

	// Overmind fork: viewers get the new skin as sent, unless a handler
	// replaces it (overmind_skin.go).
	sent := pk.Skin
	sent.Trusted, sent.OverrideAppearance = true, true
	s.setClientSkins(s.clientSkinsNow(sent)...)
	c.SetSkin(playerSkin)
	s.keepClientSkin(c.Skin())

	return nil
}
