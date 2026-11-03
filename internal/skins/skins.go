// Package skins decides how each edition is shown a player's skin.
//
// Bedrock viewers always get a Bedrock skin as the client sent it (Dragonfly
// passes it on). Java viewers can only be shown a skin hosted on
// textures.minecraft.net under a textures property Mojang signed, or one of
// the Java client's own default skins, which it picks from the player's UUID.
// This package sorts a Bedrock skin into what Java can draw (Classify), finds
// the Java-format 64x64 image (JavaImage), knows the default skins both
// editions share by the hash of their pixels (DefaultIndex), picks a UUID a Java
// client draws a given default skin for (ViewUUID), uploads other skins to a
// Mojang account to have them signed (Uploader), and spots skins whose shape
// goes beyond the standard player model (OddShape).
package skins

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"

	"github.com/df-mc/dragonfly/server/player/skin"
)

// Kind is what Java can make of a Bedrock skin.
type Kind int

const (
	// Classic is the standard player model, wide or slim, with a 64x64 image or
	// a legacy 64x32 one: Java's own format.
	Classic Kind = iota
	// ClassicExtras is Classic with more on top: extra bones or cubes
	// (Marketplace hats, hair), an animated face, a cape. Java gets the base
	// image on the standard model; the extras are dropped.
	ClassicExtras
	// Persona is a Character Creator skin: its image is composed for the
	// persona model, not laid out as a Java skin. Java shows a default skin.
	Persona
	// Custom is any other geometry, or an image that is not 64x64 or 64x32.
	// Java shows a default skin.
	Custom
)

func (k Kind) String() string {
	return [...]string{"classic", "classic with extras", "persona", "custom geometry"}[k]
}

// Look is what Java can be shown of a Bedrock skin.
type Look struct {
	Kind Kind
	// Slim is the arm width: the standard model's, else the default name's.
	Slim bool
	// Image is the Java-format 64x64 RGBA (a legacy 64x32 skin made 64x64,
	// second-layer parts the geometry lacks cleared), and Hash the hex SHA-256 of
	// its pixels with every fully transparent pixel zeroed. Both are empty unless
	// Kind is Classic or ClassicExtras.
	Image []byte
	Hash  string
}

// JavaFormat reports whether Java can be shown the skin's own image.
func (l Look) JavaFormat() bool { return l.Kind == Classic || l.Kind == ClassicExtras }

// Classify sorts a Bedrock skin by what Java can draw of it.
func Classify(s skin.Skin) Look {
	name := s.ModelConfig.Default
	_, slimName := builtIn(name)
	all := parseGeometry(s.Model)
	g, ok := resolve(all, name)
	if !slimName {
		// Dragonfly keeps no arm size, so a model of its own is slim when its
		// arms are 3 pixels wide, else when its name says so.
		if arms, found := armWidth(g); found {
			slimName = arms < 3.5
		} else {
			slimName = containsSlim(name)
		}
	}
	if s.Persona {
		return Look{Kind: Persona, Slim: slimName}
	}
	w, h := s.Bounds().Dx(), s.Bounds().Dy()
	if w != 64 || h != 64 && h != 32 || len(s.Pix) != w*h*4 {
		return Look{Kind: Custom, Slim: slimName}
	}
	if !ok {
		return Look{Kind: Custom, Slim: slimName}
	}
	shape := compare(g)
	if !shape.standard {
		return Look{Kind: Custom, Slim: slimName}
	}
	kind := Classic
	if shape.extras || s.ModelConfig.AnimatedFace != "" || len(s.Animations) > 0 || len(s.Cape.Pix) > 0 {
		kind = ClassicExtras
	}
	img := to64(s.Pix, h)
	for bone := range shape.missing {
		clearBone(img, bone, shape.slim)
	}
	return Look{Kind: kind, Slim: shape.slim, Image: img, Hash: Hash(img)}
}

// armWidth is the width of a geometry's first arm cube, if it has one.
func armWidth(g *geometry) (float64, bool) {
	if g == nil {
		return 0, false
	}
	for _, b := range g.bones {
		if (b.Name == "leftArm" || b.Name == "rightArm") && len(b.Cubes) > 0 && len(b.Cubes[0].Size) == 3 {
			return b.Cubes[0].Size[0], true
		}
	}
	return 0, false
}

func containsSlim(name string) bool {
	return bytes.Contains(bytes.ToLower([]byte(name)), []byte("slim"))
}

// Hash is the hex SHA-256 of 64x64 RGBA pixels, every fully transparent pixel
// counted as zero (clients differ in the colour they leave there).
func Hash(pix []byte) string {
	n := append([]byte(nil), pix...)
	for i := 0; i+3 < len(n); i += 4 {
		if n[i+3] == 0 {
			n[i], n[i+1], n[i+2] = 0, 0, 0
		}
	}
	sum := sha256.Sum256(n)
	return hex.EncodeToString(sum[:])
}

// to64 makes a skin's pixels 64x64. A legacy 64x32 skin gets its left leg and
// arm from the right ones, which is all the legacy layout has (minecraft.wiki
// "Skin" oldid=3874470).
func to64(pix []byte, h int) []byte {
	out := make([]byte, 64*64*4)
	copy(out, pix)
	if h == 32 {
		copyRect(out, 0, 16, 16, 48)  // right leg to left leg
		copyRect(out, 40, 16, 32, 48) // right arm to left arm
	}
	return out
}

// ToJava64 is to64 for a decoded skin image: a 64x64 or legacy 64x32 RGBA.
func ToJava64(pix []byte, h int) []byte { return to64(pix, h) }

// copyRect copies a 16 by 16 square of a 64-wide RGBA image.
func copyRect(pix []uint8, fromX, fromY, toX, toY int) {
	for y := range 16 {
		from := ((fromY+y)*64 + fromX) * 4
		to := ((toY+y)*64 + toX) * 4
		copy(pix[to:to+64], pix[from:from+64])
	}
}

// clearBone makes the texture of a standard second-layer bone transparent: a
// Java client draws that layer from the image, and the Bedrock model did not.
// A cube of size (w, h, d) at box UV (u, v) covers u to u+2(w+d), v to v+d+h.
func clearBone(img []byte, name string, slim bool) {
	for _, c := range standardCubes(slim) {
		if c.bone != name {
			continue
		}
		u, v := int(c.uv[0]), int(c.uv[1])
		w, h, d := int(c.size[0]), int(c.size[1]), int(c.size[2])
		for y := v; y < v+d+h && y < 64; y++ {
			for x := u; x < u+2*(w+d) && x < 64; x++ {
				i := (y*64 + x) * 4
				copy(img[i:i+4], []byte{0, 0, 0, 0})
			}
		}
	}
}

// PNG encodes 64x64 RGBA pixels as a PNG.
func PNG(pix []byte) ([]byte, error) {
	img := &image.NRGBA{Pix: pix, Stride: 64 * 4, Rect: image.Rect(0, 0, 64, 64)}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}
