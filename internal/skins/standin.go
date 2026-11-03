package skins

import (
	"image/color"

	"github.com/df-mc/dragonfly/server/player/skin"
)

// StandIn is our own plain skin, drawn here (no Mojang image): on the
// 64x64 layout (minecraft.wiki "Skin" oldid=3874470) a skin-toned head, hands
// and face, a shirt, trousers and shoes on the base layer, and the second
// (overlay) layer left transparent, so Bedrock draws a plain person. It stands in for a skin we
// cannot show: Mojang's own default skins are Mojang's images, so none is
// shipped.
func StandIn(slim bool) skin.Skin {
	s := skin.New(64, 64)
	var (
		face  = color.RGBA{0xC6, 0x8E, 0x6A, 0xFF}
		hair  = color.RGBA{0x3B, 0x2A, 0x1E, 0xFF}
		shirt = color.RGBA{0x2F, 0x7D, 0x9B, 0xFF}
		pants = color.RGBA{0x3A, 0x3F, 0x6B, 0xFF}
		shoes = color.RGBA{0x4A, 0x4A, 0x4A, 0xFF}
		eye   = color.RGBA{0x24, 0x24, 0x24, 0xFF}
	)
	fill := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				i := (y*64 + x) * 4
				s.Pix[i], s.Pix[i+1], s.Pix[i+2], s.Pix[i+3] = c.R, c.G, c.B, c.A
			}
		}
	}
	// Head: its six faces at (0, 0) to (32, 16); hair on top and at the back.
	fill(0, 0, 32, 16, face)
	fill(8, 0, 16, 8, hair)
	fill(24, 8, 32, 16, hair)
	fill(8, 8, 16, 10, hair) // the fringe on the front
	fill(9, 12, 11, 13, eye)
	fill(13, 12, 15, 13, eye)
	// Body (16, 16) to (40, 32), right arm (40, 16) to (56, 32), left arm
	// (32, 48) to (48, 64): a shirt, with skin-toned hands at the bottom.
	fill(16, 16, 40, 32, shirt)
	fill(40, 16, 56, 32, shirt)
	fill(32, 48, 48, 64, shirt)
	fill(40, 28, 56, 32, face)
	fill(32, 60, 48, 64, face)
	// Right leg (0, 16) to (16, 32), left leg (16, 48) to (32, 64): trousers
	// and shoes.
	fill(0, 16, 16, 32, pants)
	fill(16, 48, 32, 64, pants)
	fill(0, 29, 16, 32, shoes)
	fill(16, 61, 32, 64, shoes)
	s.ModelConfig = skin.ModelConfig{Default: "geometry.humanoid.custom"}
	if slim {
		s.ModelConfig.Default = "geometry.humanoid.customSlim"
	}
	return s
}
