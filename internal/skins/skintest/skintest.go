// Package skintest makes Bedrock skin data for tests. The geometry starts from
// real client data: gophertunnel 1.63.0's skin_geometry.json, the geometry a
// Bedrock client sends with a classic skin, read from the module cache (it is
// not copied into this repo). Images are drawn here; no Mojang image is used.
// Fixtures that cannot be shipped (Mojang's default skins, other projects'
// geometry files) are read from the folder OVERMIND_SKIN_FIXTURES names.
package skintest

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

var (
	geomOnce sync.Once
	geom     []byte
	geomErr  error
)

// ClassicGeometry is gophertunnel's skin_geometry.json: geometry.cape,
// geometry.humanoid.custom and geometry.humanoid.customSlim.
func ClassicGeometry(t testing.TB) []byte {
	t.Helper()
	geomOnce.Do(func() {
		out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/sandertv/gophertunnel").Output()
		if err != nil {
			geomErr = err
			return
		}
		geom, geomErr = os.ReadFile(filepath.Join(strings.TrimSpace(string(out)), "minecraft", "skin_geometry.json"))
	})
	if geomErr != nil {
		t.Fatalf("read gophertunnel's skin geometry: %v", geomErr)
	}
	return append([]byte(nil), geom...)
}

// Image is a w by h RGBA skin image, opaque, its pixels from seed.
func Image(w, h int, seed byte) []byte {
	pix := make([]byte, w*h*4)
	for i := 0; i < len(pix); i += 4 {
		v := byte(i/4)*31 + seed*17
		pix[i], pix[i+1], pix[i+2], pix[i+3] = v, v^0x5a, seed, 255
	}
	return pix
}

// Edit changes one geometry of a geometry file, by identifier: edit gets its
// bones as decoded JSON and returns them.
func Edit(t testing.TB, data []byte, id string, edit func(bones []map[string]any) []map[string]any) []byte {
	t.Helper()
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	list, _ := top["minecraft:geometry"].([]any)
	found := false
	for _, g := range list {
		gm := g.(map[string]any)
		if gm["description"].(map[string]any)["identifier"] != id {
			continue
		}
		var bones []map[string]any
		for _, b := range gm["bones"].([]any) {
			bones = append(bones, b.(map[string]any))
		}
		bones = edit(bones)
		out := make([]any, len(bones))
		for i, b := range bones {
			out[i] = b
		}
		gm["bones"] = out
		found = true
	}
	if !found {
		t.Fatalf("no geometry %s", id)
	}
	b, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// Rename gives a geometry a new identifier and texture size.
func Rename(t testing.TB, data []byte, id, to string, texW, texH int) []byte {
	t.Helper()
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	for _, g := range top["minecraft:geometry"].([]any) {
		d := g.(map[string]any)["description"].(map[string]any)
		if d["identifier"] == id {
			d["identifier"], d["texture_width"], d["texture_height"] = to, texW, texH
		}
	}
	b, _ := json.Marshal(top)
	return b
}

// Bone finds a bone by name.
func Bone(bones []map[string]any, name string) map[string]any {
	for _, b := range bones {
		if b["name"] == name {
			return b
		}
	}
	return nil
}

// Cube is a cube's JSON.
func Cube(origin, size [3]float64, uv [2]float64) map[string]any {
	return map[string]any{"origin": origin[:], "size": size[:], "uv": uv[:]}
}

// WithExtras is the classic geometry with what a Marketplace skin adds on
// top: a "hood" bone on the head, a little bigger than it and touching it.
func WithExtras(t testing.TB, data []byte, id string) []byte {
	return Edit(t, data, id, func(bones []map[string]any) []map[string]any {
		return append(bones, map[string]any{"name": "hood", "parent": "head", "pivot": []float64{0, 24, 0},
			"cubes": []any{Cube([3]float64{-4.5, 23.5, -4.5}, [3]float64{9, 9, 9}, [2]float64{0, 0})}})
	})
}

// Patch is a resource patch naming a default geometry, and an animated face if
// face is not empty.
func Patch(def, face string) []byte {
	m := map[string]string{"default": def}
	if face != "" {
		m["animated_face"] = face
	}
	b, _ := json.Marshal(map[string]any{"geometry": m})
	return b
}

// Skin is what a skin's client data is made of.
type Skin struct {
	Pix          []byte
	W, H         int
	Geometry     []byte
	Patch        []byte
	Persona      bool
	ArmSize      string
	CapeW, CapeH int
}

// ClientData is a Bedrock client's login data with this skin.
func (s Skin) ClientData() login.ClientData {
	enc := base64.StdEncoding.EncodeToString
	cd := login.ClientData{
		SkinData: enc(s.Pix), SkinImageWidth: s.W, SkinImageHeight: s.H,
		SkinGeometry: enc(s.Geometry), SkinResourcePatch: enc(s.Patch),
		PersonaSkin: s.Persona, ArmSize: s.ArmSize,
	}
	if s.CapeW > 0 {
		cd.CapeData, cd.CapeImageWidth, cd.CapeImageHeight = enc(Image(s.CapeW, s.CapeH, 9)), s.CapeW, s.CapeH
	}
	return cd
}

// Persona is a Character Creator skin as a client sends one: the persona flag,
// an image composed for the persona model (here 128x128, drawn), and a
// geometry of its own ("geometry.persona_" and an ID) with the humanoid bones
// on a 128x128 texture.
func Persona(t testing.TB) Skin {
	g := Rename(t, ClassicGeometry(t), "geometry.humanoid.customSlim", "geometry.persona_7b3c0e2a_0", 128, 128)
	return Skin{Pix: Image(128, 128, 4), W: 128, H: 128, Geometry: g, Patch: Patch("geometry.persona_7b3c0e2a_0", ""), Persona: true, ArmSize: "slim"}
}

// Custom is a skin on geometry of its own: the classic model with long,
// thin arms, under a name of its own.
func Custom(t testing.TB) Skin {
	g := Edit(t, ClassicGeometry(t), "geometry.humanoid.custom", func(bones []map[string]any) []map[string]any {
		for _, name := range []string{"leftArm", "rightArm"} {
			b := Bone(bones, name)
			x := 4.0
			if name == "rightArm" {
				x = -6
			}
			b["cubes"] = []any{Cube([3]float64{x, 6, -1}, [3]float64{2, 18, 2}, [2]float64{40, 16})}
		}
		return bones
	})
	g = Rename(t, g, "geometry.humanoid.custom", "geometry.custom.longarms", 64, 64)
	return Skin{Pix: Image(64, 64, 6), W: 64, H: 64, Geometry: g, Patch: Patch("geometry.custom.longarms", ""), ArmSize: "wide"}
}

// Fixtures is the folder of fixtures that cannot be shipped; the test is
// skipped without it.
func Fixtures(t testing.TB) string {
	t.Helper()
	dir := os.Getenv("OVERMIND_SKIN_FIXTURES")
	if dir == "" {
		t.Skip("OVERMIND_SKIN_FIXTURES is not set")
	}
	return dir
}

// ProtocolSkin is the skin as a client sends it in a skin change.
func (s Skin) ProtocolSkin() protocol.Skin {
	p := protocol.Skin{SkinID: "skintest-" + strconv.Itoa(len(s.Pix)) + "-" + strconv.Itoa(int(s.Pix[0])), SkinResourcePatch: s.Patch,
		SkinImageWidth: uint32(s.W), SkinImageHeight: uint32(s.H), SkinData: s.Pix, SkinGeometry: s.Geometry,
		PersonaSkin: s.Persona, FullID: "skintest", GeometryDataEngineVersion: []byte("1.21.0"), Trusted: true}
	if s.CapeW > 0 {
		p.CapeImageWidth, p.CapeImageHeight, p.CapeData = uint32(s.CapeW), uint32(s.CapeH), Image(s.CapeW, s.CapeH, 9)
	}
	return p
}
