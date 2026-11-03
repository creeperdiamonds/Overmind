package session

// Overmind fork: a Bedrock player's skin reaches Bedrock viewers, the player
// included, as its client sent it. Upstream sends skin.Skin through
// skinToProtocol, which drops persona pieces, tints, skin colour, arm size,
// animation data, the cape ID, the premium flag and the geometry engine
// version, and makes up a new skin ID each time. A Character Creator skin
// without its pieces is drawn as a default skin, by other players and, as the
// list row for its own UUID overrides its appearance, by the player itself.

import (
	"bytes"
	"encoding/base64"
	"image/color"
	"strconv"
	"strings"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// clientSkin is the skin a client logged in with, in its network form.
// Trusted and OverrideAppearance are the server's to set and are true, as
// upstream sends them; FullID is the skin ID, as upstream sends it.
func clientSkin(d login.ClientData) protocol.Skin {
	dec := func(s string) []byte { b, _ := base64.StdEncoding.DecodeString(s); return b }
	sk := protocol.Skin{
		SkinID:                    d.SkinID,
		SkinResourcePatch:         dec(d.SkinResourcePatch),
		SkinImageWidth:            uint32(d.SkinImageWidth),
		SkinImageHeight:           uint32(d.SkinImageHeight),
		SkinData:                  dec(d.SkinData),
		CapeImageWidth:            uint32(d.CapeImageWidth),
		CapeImageHeight:           uint32(d.CapeImageHeight),
		CapeData:                  dec(d.CapeData),
		SkinGeometry:              dec(d.SkinGeometry),
		AnimationData:             dec(d.SkinAnimationData),
		GeometryDataEngineVersion: dec(d.SkinGeometryVersion),
		PremiumSkin:               d.PremiumSkin,
		PersonaSkin:               d.PersonaSkin,
		PersonaCapeOnClassicSkin:  d.CapeOnClassicSkin,
		CapeID:                    d.CapeID,
		FullID:                    d.SkinID,
		SkinColour:                hexColour(d.SkinColour),
		ArmSize:                   protocol.ArmSizeWide,
		Trusted:                   true,
		OverrideAppearance:        true,
	}
	if d.ArmSize == "slim" {
		sk.ArmSize = protocol.ArmSizeSlim
	}
	if len(sk.SkinGeometry) == 0 {
		sk.SkinGeometry = []byte("{}")
	}
	for _, a := range d.AnimatedImageData {
		sk.Animations = append(sk.Animations, protocol.SkinAnimation{
			ImageWidth: uint32(a.ImageWidth), ImageHeight: uint32(a.ImageHeight), ImageData: dec(a.Image),
			AnimationType: uint32(a.Type), FrameCount: float32(a.Frames), ExpressionType: uint32(a.AnimationExpression),
		})
	}
	for _, p := range d.PersonaPieces {
		pack, _ := uuid.Parse(p.PackID)
		sk.PersonaPieces = append(sk.PersonaPieces, protocol.PersonaPiece{
			PieceID: p.PieceID, PieceType: pieceType(p.PieceType), PackID: pack, Default: p.Default, ProductID: p.ProductID,
		})
	}
	for _, t := range d.PieceTintColours {
		tint := protocol.PersonaPieceTintColour{PieceType: t.PieceType}
		for i, c := range t.Colours {
			tint.Colours[i] = hexColour(c)
		}
		sk.PieceTintColours = append(sk.PieceTintColours, tint)
	}
	return sk
}

// pieceTypes are the login names of persona piece types, in the order of
// gophertunnel's PieceType constants (from PieceTypeSkeleton).
var pieceTypes = []string{"skeleton", "body", "skin", "bottom", "feet", "dress", "top", "high_pants", "hand",
	"outerwear", "facial_hair", "mouth", "eyes", "hair", "hood", "back", "face_accessory", "head", "legs",
	"left_leg", "right_leg", "arms", "left_arm", "right_arm", "capes", "classic_skin", "emote"}

// pieceType is the network type of a persona piece type named as in login
// data ("persona_hair"). An unknown name is PieceTypeUnsupported.
func pieceType(name string) uint32 {
	n := strings.TrimPrefix(name, "persona_")
	if n == "hands" {
		n = "hand"
	}
	for i, t := range pieceTypes {
		if t == n {
			return uint32(protocol.PieceTypeSkeleton + i)
		}
	}
	if name == "" {
		return protocol.PieceTypeUnknown
	}
	return protocol.PieceTypeUnsupported
}

// hexColour reads a login colour: "#rrggbb" (opaque) or "#aarrggbb"; "#0" and
// anything unreadable are transparent black.
func hexColour(s string) color.RGBA {
	h := strings.TrimPrefix(s, "#")
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.RGBA{}
	}
	switch len(h) {
	case 6:
		return color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
	case 8:
		return color.RGBA{A: uint8(v >> 24), R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v)}
	}
	return color.RGBA{}
}

// skinMatches reports whether a skin is the one a network skin decodes to, so
// that nothing (a JoinSkin hook, a skin change handler) has replaced it.
func skinMatches(s skin.Skin, p protocol.Skin) bool {
	b, cb := s.Bounds().Max, s.Cape.Bounds().Max
	if uint32(b.X) != p.SkinImageWidth || uint32(b.Y) != p.SkinImageHeight || !bytes.Equal(s.Pix, p.SkinData) ||
		uint32(cb.X) != p.CapeImageWidth || uint32(cb.Y) != p.CapeImageHeight || !bytes.Equal(s.Cape.Pix, p.CapeData) ||
		s.Persona != p.PersonaSkin || len(s.Animations) != len(p.Animations) {
		return false
	}
	if !bytes.Equal(s.Model, p.SkinGeometry) && !(len(s.Model) == 0 && string(p.SkinGeometry) == "{}") {
		return false
	}
	if cfg, err := skin.DecodeModelConfig(p.SkinResourcePatch); err != nil || cfg != s.ModelConfig {
		return false
	}
	for i, a := range s.Animations {
		if !bytes.Equal(a.Pix, p.Animations[i].ImageData) || a.FrameCount != int(p.Animations[i].FrameCount) {
			return false
		}
	}
	return true
}

// setClientSkins sets the skins the session's client sent that its player may
// still wear, oldest first.
func (s *Session) setClientSkins(skins ...protocol.Skin) {
	s.clientSkinMu.Lock()
	s.clientSkins = skins
	s.clientSkinMu.Unlock()
}

// keepClientSkin forgets the client's skins other than the one the player
// wears, if any.
func (s *Session) keepClientSkin(worn skin.Skin) {
	s.clientSkinMu.Lock()
	defer s.clientSkinMu.Unlock()
	for _, p := range s.clientSkins {
		if skinMatches(worn, p) {
			s.clientSkins = []protocol.Skin{p}
			return
		}
	}
	s.clientSkins = nil
}

// protocolSkin is the network form of a skin of this session's player: the
// skin its client sent when the player still wears it, or upstream's
// conversion when something replaced it.
func (s *Session) protocolSkin(sk skin.Skin) protocol.Skin {
	s.clientSkinMu.Lock()
	defer s.clientSkinMu.Unlock()
	for i := len(s.clientSkins) - 1; i >= 0; i-- {
		if skinMatches(sk, s.clientSkins[i]) {
			return s.clientSkins[i]
		}
	}
	return skinToProtocol(sk)
}

// skinOf is the network form of a Controllable's skin for Bedrock viewers.
func skinOf(c Controllable) protocol.Skin {
	if s, ok := sessions.Lookup(c.UUID()); ok {
		return s.protocolSkin(c.Skin())
	}
	return skinToProtocol(c.Skin())
}

// clientSkinsNow is the client's skins the player may wear, with one more.
func (s *Session) clientSkinsNow(next protocol.Skin) []protocol.Skin {
	s.clientSkinMu.Lock()
	defer s.clientSkinMu.Unlock()
	return append(append([]protocol.Skin(nil), s.clientSkins...), next)
}
