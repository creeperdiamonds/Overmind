package javafront_test

import (
	"encoding/base64"
	"image/color"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/skins/skintest"
	"github.com/overmind/overmind/internal/testserver"
)

// A Character Creator skin as a real client logs in with it: persona pieces,
// tints, skin colour, arm size, a blinking head animation, a cape and a
// geometry engine version of its own.
func personaClientData(t *testing.T) login.ClientData {
	p := skintest.Persona(t)
	cd := p.ClientData()
	enc := base64.StdEncoding.EncodeToString
	cd.SkinID = "c18e65aa-7b21-4637-9b63-8ad63622ef01.Custom9c2f"
	cd.SkinGeometryVersion = enc([]byte("1.14.0"))
	cd.SkinAnimationData = enc([]byte(`{"anim":"blink"}`))
	cd.SkinColour = "#b37b62"
	cd.ArmSize = "slim"
	cd.CapeID = "0b5e2b6c-7d1c-4b0b-8a7a-0b5e2b6c7d1c"
	cd.CapeData, cd.CapeImageWidth, cd.CapeImageHeight = enc(skintest.Image(64, 32, 9)), 64, 32
	cd.CapeOnClassicSkin = false
	cd.PremiumSkin = true
	cd.AnimatedImageData = []login.SkinAnimation{{Frames: 3, Image: enc(skintest.Image(32, 96, 5)), ImageWidth: 32, ImageHeight: 96,
		Type: protocol.SkinAnimationHead, AnimationExpression: protocol.ExpressionTypeBlinking}}
	cd.PersonaPieces = []login.PersonaPiece{
		{Default: true, PackID: "799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3", PieceID: "8f96d1f8-e9bb-40d2-acc8-eb79746c5d7c", PieceType: "persona_skeleton"},
		{Default: true, PackID: "799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3", PieceID: "1042557f-d1f9-44e3-ba78-f404e8fb7363", PieceType: "persona_body"},
		{Default: true, PackID: "799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3", PieceID: "a8ff3f4a-d4c7-4af7-ad19-1b06dd3e3a6b", PieceType: "persona_skin"},
		{Default: false, PackID: "8bf3bbb5-a2b3-4c33-b8b9-1c2b7c4e2a10", PieceID: "4b3a3f6c-2c3e-4d6e-9b1e-5e0b6a4d7c11", PieceType: "persona_hair", ProductID: "6e1a2b3c-4d5e-4f60-8a7b-9c0d1e2f3a4b"},
		{Default: true, PackID: "799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3", PieceID: "1c1a6e21-8a5e-4d0f-a7a2-6d0d5c8d1f22", PieceType: "persona_eyes"},
		{Default: true, PackID: "799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3", PieceID: "5d1b2c3e-0f6a-4e7b-9c8d-2a1b0c9d8e33", PieceType: "persona_mouth"},
		{Default: false, PackID: "8bf3bbb5-a2b3-4c33-b8b9-1c2b7c4e2a10", PieceID: "77c5b0a1-3e2d-4c1b-8a9f-0e1d2c3b4a44", PieceType: "persona_top", ProductID: "7f2b3c4d-5e6f-4a71-8b9c-0d1e2f3a4b5c"},
		{Default: true, PackID: "799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3", PieceID: "9e8d7c6b-5a49-4382-a1f0-e9d8c7b6a555", PieceType: "persona_bottom"},
		{Default: true, PackID: "799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3", PieceID: "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c66", PieceType: "persona_feet"},
	}
	cd.PieceTintColours = []login.PersonaPieceTintColour{
		{PieceType: "persona_eyes", Colours: [4]string{"#ffa12722", "#ff2f1f0f", "#ff3aafd9", "#0"}},
		{PieceType: "persona_hair", Colours: [4]string{"#ff2f1f0f", "#0", "#0", "#0"}},
		{PieceType: "persona_mouth", Colours: [4]string{"#ffe0a28a", "#0", "#0", "#0"}},
	}
	return cd
}

// personaProtocolSkin is personaClientData as the network form of a skin
// carries it, written out by hand: what every Bedrock viewer must get.
func personaProtocolSkin(t *testing.T) protocol.Skin {
	p := skintest.Persona(t)
	pack, product := uuid.MustParse("799bf6a6-6e1c-4b29-a2d4-0e8ba93ae6b3"), uuid.MustParse("8bf3bbb5-a2b3-4c33-b8b9-1c2b7c4e2a10")
	rgba := func(a, r, g, b uint8) color.RGBA { return color.RGBA{R: r, G: g, B: b, A: a} }
	none := color.RGBA{}
	return protocol.Skin{
		SkinID:            "c18e65aa-7b21-4637-9b63-8ad63622ef01.Custom9c2f",
		SkinResourcePatch: p.Patch,
		SkinImageWidth:    128, SkinImageHeight: 128, SkinData: p.Pix,
		Animations: []protocol.SkinAnimation{{ImageWidth: 32, ImageHeight: 96, ImageData: skintest.Image(32, 96, 5),
			AnimationType: protocol.SkinAnimationHead, FrameCount: 3, ExpressionType: protocol.ExpressionTypeBlinking}},
		CapeImageWidth: 64, CapeImageHeight: 32, CapeData: skintest.Image(64, 32, 9),
		SkinGeometry:              p.Geometry,
		AnimationData:             []byte(`{"anim":"blink"}`),
		GeometryDataEngineVersion: []byte("1.14.0"),
		PremiumSkin:               true,
		PersonaSkin:               true,
		CapeID:                    "0b5e2b6c-7d1c-4b0b-8a7a-0b5e2b6c7d1c",
		FullID:                    "c18e65aa-7b21-4637-9b63-8ad63622ef01.Custom9c2f",
		SkinColour:                rgba(0xff, 0xb3, 0x7b, 0x62),
		ArmSize:                   protocol.ArmSizeSlim,
		PersonaPieces: []protocol.PersonaPiece{
			{PieceID: "8f96d1f8-e9bb-40d2-acc8-eb79746c5d7c", PieceType: protocol.PieceTypeSkeleton, PackID: pack, Default: true},
			{PieceID: "1042557f-d1f9-44e3-ba78-f404e8fb7363", PieceType: protocol.PieceTypeBody, PackID: pack, Default: true},
			{PieceID: "a8ff3f4a-d4c7-4af7-ad19-1b06dd3e3a6b", PieceType: protocol.PieceTypeSkin, PackID: pack, Default: true},
			{PieceID: "4b3a3f6c-2c3e-4d6e-9b1e-5e0b6a4d7c11", PieceType: protocol.PieceTypeHair, PackID: product, ProductID: "6e1a2b3c-4d5e-4f60-8a7b-9c0d1e2f3a4b"},
			{PieceID: "1c1a6e21-8a5e-4d0f-a7a2-6d0d5c8d1f22", PieceType: protocol.PieceTypeEyes, PackID: pack, Default: true},
			{PieceID: "5d1b2c3e-0f6a-4e7b-9c8d-2a1b0c9d8e33", PieceType: protocol.PieceTypeMouth, PackID: pack, Default: true},
			{PieceID: "77c5b0a1-3e2d-4c1b-8a9f-0e1d2c3b4a44", PieceType: protocol.PieceTypeTop, PackID: product, ProductID: "7f2b3c4d-5e6f-4a71-8b9c-0d1e2f3a4b5c"},
			{PieceID: "9e8d7c6b-5a49-4382-a1f0-e9d8c7b6a555", PieceType: protocol.PieceTypeBottom, PackID: pack, Default: true},
			{PieceID: "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c66", PieceType: protocol.PieceTypeFeet, PackID: pack, Default: true},
		},
		PieceTintColours: []protocol.PersonaPieceTintColour{
			{PieceType: "persona_eyes", Colours: [4]color.RGBA{rgba(0xff, 0xa1, 0x27, 0x22), rgba(0xff, 0x2f, 0x1f, 0x0f), rgba(0xff, 0x3a, 0xaf, 0xd9), none}},
			{PieceType: "persona_hair", Colours: [4]color.RGBA{rgba(0xff, 0x2f, 0x1f, 0x0f), none, none, none}},
			{PieceType: "persona_mouth", Colours: [4]color.RGBA{rgba(0xff, 0xe0, 0xa2, 0x8a), none, none, none}},
		},
		Trusted:            true,
		OverrideAppearance: true,
	}
}

// waitSkin waits for a Bedrock bot's view of a player's skin to satisfy ok.
func waitSkin(t *testing.T, b *bedrock.Bot, name string, ok func(bedrock.SkinSeen) bool) bedrock.SkinSeen {
	t.Helper()
	var s bedrock.SkinSeen
	require.Eventually(t, func() bool { var seen bool; s, seen = b.Skin(name); return seen && ok(s) }, 15*time.Second, 50*time.Millisecond, "no skin for %s", name)
	return s
}

// A Bedrock player with a Character Creator skin is shown it, field for field
// as its client sent it, in its own player list and by another Bedrock player,
// both by the player list and by the skin sent when its entity is shown; a
// later skin change is relayed as sent too.
func TestBedrockPersonaSkinRelayedAsSent(t *testing.T) {
	srv := testserver.Start(t)
	watcher, err := bedrock.Join(srv.BedrockAddr, "BPWatcher")
	require.NoError(t, err)
	defer watcher.Close()
	p, err := bedrock.JoinWith(srv.BedrockAddr, "BPersonaFull", uuid.New(), personaClientData(t))
	require.NoError(t, err)
	defer p.Close()

	want := personaProtocolSkin(t)
	seen := func(bedrock.SkinSeen) bool { return true }
	require.Equal(t, want, waitSkin(t, p, "BPersonaFull", seen).Raw, "the player's own list row")
	require.Equal(t, want, waitSkin(t, watcher, "BPersonaFull", seen).Raw, "another Bedrock player's list row")
	// Once the entity is shown, a PlayerSkin follows: it must carry the same.
	changed := func(s bedrock.SkinSeen) bool { return s.Changes > 0 }
	require.Equal(t, want, waitSkin(t, watcher, "BPersonaFull", changed).Raw, "the skin sent with the entity")

	// A skin change, persona pieces and all, reaches the other player as sent.
	next := want
	next.SkinID = "c18e65aa-7b21-4637-9b63-8ad63622ef01.Custom1111"
	next.SkinData = skintest.Image(128, 128, 21)
	next.PersonaPieces = append([]protocol.PersonaPiece(nil), want.PersonaPieces[:4]...)
	before, _ := watcher.Skin("BPersonaFull")
	require.NoError(t, p.ChangeSkin(next))
	got := waitSkin(t, watcher, "BPersonaFull", func(s bedrock.SkinSeen) bool { return s.Changes > before.Changes && s.Raw.SkinID == next.SkinID })
	require.Equal(t, next, got.Raw, "the changed skin")
}
