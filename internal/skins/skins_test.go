package skins_test

import (
	"encoding/json"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/skins"
	"github.com/creeperdiamonds/overmind/internal/skins/skintest"
)

// bedrockSkin is a skin as Dragonfly decodes it from client data.
func bedrockSkin(s skintest.Skin) skin.Skin {
	out := skin.New(s.W, s.H)
	out.Pix = s.Pix
	out.Model = s.Geometry
	out.Persona = s.Persona
	cfg, _ := skin.DecodeModelConfig(s.Patch)
	out.ModelConfig = cfg
	if s.CapeW > 0 {
		out.Cape = skin.NewCape(s.CapeW, s.CapeH)
		out.Cape.Pix = skintest.Image(s.CapeW, s.CapeH, 9)
	}
	return out
}

func classic(t *testing.T, slim bool) skintest.Skin {
	def := "geometry.humanoid.custom"
	if slim {
		def = "geometry.humanoid.customSlim"
	}
	return skintest.Skin{Pix: skintest.Image(64, 64, 1), W: 64, H: 64, Geometry: skintest.ClassicGeometry(t), Patch: skintest.Patch(def, "")}
}

// The standard model in geometry.go is gophertunnel's skin_geometry.json, cube
// for cube: a classic skin on that data is Classic, wide and slim.
func TestClassicSkins(t *testing.T) {
	for _, slim := range []bool{false, true} {
		s := classic(t, slim)
		l := skins.Classify(bedrockSkin(s))
		require.Equal(t, skins.Classic, l.Kind, "slim %v", slim)
		require.Equal(t, slim, l.Slim)
		require.Equal(t, s.Pix, l.Image, "the image changed")
		require.Equal(t, skins.Hash(s.Pix), l.Hash)
	}
	// Named without data: the built-in model.
	s := classic(t, true)
	s.Geometry = []byte("{}")
	l := skins.Classify(bedrockSkin(s))
	require.Equal(t, skins.Classic, l.Kind)
	require.True(t, l.Slim)
}

// A legacy 64x32 image is made 64x64, the left limbs from the right ones.
func TestLegacySkinMade64(t *testing.T) {
	s := classic(t, false)
	s.Pix, s.H = skintest.Image(64, 32, 2), 32
	l := skins.Classify(bedrockSkin(s))
	require.Equal(t, skins.Classic, l.Kind)
	require.Len(t, l.Image, 64*64*4)
	px := func(x, y int) []byte { i := (y*64 + x) * 4; return l.Image[i : i+4] }
	src := func(x, y int) []byte { i := (y*64 + x) * 4; return s.Pix[i : i+4] }
	require.Equal(t, src(2, 20), px(18, 52), "left leg")
	require.Equal(t, src(42, 18), px(34, 50), "left arm")
}

// Extras are dropped for Java: the base image on the standard model.
func TestClassicWithExtras(t *testing.T) {
	cases := map[string]func(s *skintest.Skin){
		"extra bone": func(s *skintest.Skin) { s.Geometry = skintest.WithExtras(t, s.Geometry, "geometry.humanoid.custom") },
		"animated face": func(s *skintest.Skin) {
			s.Patch = skintest.Patch("geometry.humanoid.custom", "geometry.animated_face_persona_x")
		},
		"cape": func(s *skintest.Skin) { s.CapeW, s.CapeH = 64, 32 },
		"extra cube on the head": func(s *skintest.Skin) {
			s.Geometry = skintest.Edit(t, s.Geometry, "geometry.humanoid.custom", func(b []map[string]any) []map[string]any {
				h := skintest.Bone(b, "head")
				h["cubes"] = append(h["cubes"].([]any), skintest.Cube([3]float64{-1, 32, -1}, [3]float64{2, 2, 2}, [2]float64{56, 0}))
				return b
			})
		},
	}
	for name, change := range cases {
		s := classic(t, false)
		change(&s)
		l := skins.Classify(bedrockSkin(s))
		require.Equal(t, skins.ClassicExtras, l.Kind, name)
		require.Equal(t, s.Pix, l.Image, name)
		require.True(t, l.JavaFormat())
	}
}

// A geometry without the hat clears the hat in Java's image, as Bedrock did
// not draw it.
func TestMissingOverlayCleared(t *testing.T) {
	s := classic(t, false)
	s.Geometry = skintest.Edit(t, s.Geometry, "geometry.humanoid.custom", func(b []map[string]any) []map[string]any {
		out := b[:0]
		for _, bone := range b {
			if bone["name"] != "hat" {
				out = append(out, bone)
			}
		}
		return out
	})
	l := skins.Classify(bedrockSkin(s))
	require.Equal(t, skins.Classic, l.Kind)
	i := (4*64 + 40) * 4
	require.Equal(t, []byte{0, 0, 0, 0}, l.Image[i:i+4], "the hat is still drawn")
	i = (4*64 + 8) * 4
	require.Equal(t, s.Pix[i:i+4], l.Image[i:i+4], "the head was cleared")
}

func TestPersonaAndCustom(t *testing.T) {
	l := skins.Classify(bedrockSkin(skintest.Persona(t)))
	require.Equal(t, skins.Persona, l.Kind)
	require.False(t, l.JavaFormat())
	require.Empty(t, l.Image)

	l = skins.Classify(bedrockSkin(skintest.Custom(t)))
	require.Equal(t, skins.Custom, l.Kind)
	require.False(t, l.JavaFormat())

	// Standard geometry, but a 128x128 image: not Java's format.
	s := classic(t, false)
	s.Pix, s.W, s.H = skintest.Image(128, 128, 3), 128, 128
	require.Equal(t, skins.Custom, skins.Classify(bedrockSkin(s)).Kind)
	// A geometry the data does not have.
	s = classic(t, false)
	s.Patch = skintest.Patch("geometry.nowhere", "")
	require.Equal(t, skins.Custom, skins.Classify(bedrockSkin(s)).Kind)
	// Rotated arms.
	s = classic(t, false)
	s.Geometry = skintest.Edit(t, s.Geometry, "geometry.humanoid.custom", func(b []map[string]any) []map[string]any {
		skintest.Bone(b, "leftArm")["rotation"] = []float64{0, 0, 45}
		return b
	})
	require.Equal(t, skins.Custom, skins.Classify(bedrockSkin(s)).Kind)
}

// The old (1.8) geometry format, inheriting from the built-in model.
func TestOldFormatGeometry(t *testing.T) {
	s := classic(t, false)
	s.Geometry = []byte(`{"format_version":"1.8.0","geometry.mine:geometry.humanoid.custom":{"bones":[{"name":"horn","parent":"head","pivot":[0,24,0],"cubes":[{"origin":[-1,31,-1],"size":[2,2,2],"uv":[56,0]}]}]}}`)
	s.Patch = skintest.Patch("geometry.mine", "")
	require.Equal(t, skins.ClassicExtras, skins.Classify(bedrockSkin(s)).Kind)
	odd, why := skins.OddShape(bedrockSkin(s))
	require.False(t, odd, why)
}

// Java's DefaultPlayerSkin.get: values computed by a JDK (25.0.4) with
// Math.floorMod(UUID.fromString(s).hashCode(), 18).
func TestJavaDefaultMatchesJava(t *testing.T) {
	for _, c := range []struct {
		id   string
		want int
	}{
		{"069a79f4-44e9-4726-a5be-fca90e38aaf5", 0},
		{"853c80ef-3c37-49fd-aa49-938b674adae6", 1},
		{"c06f8906-4c8a-4911-9c29-ea1dbd1aab82", 6},
		{"bb0f1798-a377-3418-bfc9-945a02770b39", 15},
		{"592ad309-7d98-4a5d-e5b3-f440be1a4792", 6},
		{"b54b1904-91b3-ca5d-5a17-15fff3e147a1", 11},
		{"1ee88ee9-40ae-7fdf-d993-e7beb252edfc", 12},
		{"154de301-afca-c4e5-edc2-6f161bfba4ce", 16},
		{"96c697fb-b258-3069-7914-dd2d973295f1", 0},
		{"4121c8e0-996d-a4a6-61f4-4c359fc917b3", 8},
		{"c54c61ed-9f02-4ba7-384e-818eb69539d9", 1},
		{"ffffffff-ffff-ffff-ffff-fffffffffffe", 1},
	} {
		require.Equal(t, c.want, skins.JavaDefault(uuid.MustParse(c.id)), c.id)
	}
}

// A view UUID picks the wanted default, is the same every time for the same
// account and skin, differs between accounts, and is version 8.
func TestViewUUID(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	seen := map[uuid.UUID]bool{}
	for want := range 18 {
		id := skins.ViewUUID(a, want, nil)
		require.Equal(t, want, skins.JavaDefault(id))
		require.Equal(t, id, skins.ViewUUID(a, want, nil), "not stable")
		require.NotEqual(t, id, skins.ViewUUID(b, want, nil))
		require.EqualValues(t, 8, id.Version())
		require.Equal(t, uuid.RFC4122, id.Variant())
		require.False(t, seen[id])
		seen[id] = true
	}
	first := skins.ViewUUID(a, 3, nil)
	other := skins.ViewUUID(a, 3, func(id uuid.UUID) bool { return id == first })
	require.NotEqual(t, first, other)
	require.Equal(t, 3, skins.JavaDefault(other))
}

func TestFallback(t *testing.T) {
	require.Equal(t, "wide steve", skins.DefaultName(skins.Fallback(false)))
	require.Equal(t, "slim alex", skins.DefaultName(skins.Fallback(true)))
	_, ok := skins.DefaultIndex(skins.Hash(skintest.Image(64, 64, 1)), false)
	require.False(t, ok)
}

// The hashes stored are the default skins' own: run with
// OVERMIND_SKIN_FIXTURES holding java/{slim,wide}/<name>.png from the 26.1.2
// client jar and bedrock/<name>.png from Bedrock's vanilla skin pack.
func TestDefaultHashesFromImages(t *testing.T) {
	dir := skintest.Fixtures(t)
	names := []string{"alex", "ari", "efe", "kai", "makena", "noor", "steve", "sunny", "zuri"}
	for _, model := range []string{"slim", "wide"} {
		for _, n := range names {
			pix := readPNG(t, filepath.Join(dir, "java", model, n+".png"))
			i, ok := skins.DefaultIndex(skins.Hash(pix), model == "slim")
			require.True(t, ok, "%s %s", model, n)
			require.Equal(t, model+" "+n, skins.DefaultName(i))
		}
	}
	bedrockModel := map[string]bool{"steve": false, "alex": true, "ari": false, "efe": true, "kai": false, "makena": true, "noor": true, "sunny": true, "zuri": false}
	for n, slim := range bedrockModel {
		pix := readPNG(t, filepath.Join(dir, "bedrock", n+".png"))
		i, ok := skins.DefaultIndex(skins.Hash(pix), slim)
		require.True(t, ok, "bedrock %s", n)
		require.Contains(t, skins.DefaultName(i), n)
	}
}

func TestOddShapes(t *testing.T) {
	notOdd := func(s skintest.Skin) {
		t.Helper()
		odd, why := skins.OddShape(bedrockSkin(s))
		require.False(t, odd, why)
	}
	isOdd := func(s skintest.Skin, reason string) {
		t.Helper()
		odd, why := skins.OddShape(bedrockSkin(s))
		require.True(t, odd)
		require.Contains(t, why, reason)
	}
	notOdd(classic(t, false))
	notOdd(classic(t, true))
	s := classic(t, false)
	s.Geometry = []byte("{}")
	notOdd(s) // the built-in model
	s = classic(t, false)
	s.Geometry = skintest.WithExtras(t, s.Geometry, "geometry.humanoid.custom")
	notOdd(s) // a hood half a pixel past the hat
	notOdd(skintest.Persona(t))

	edit := func(slim bool, f func(b []map[string]any) []map[string]any) skintest.Skin {
		s := classic(t, slim)
		id := "geometry.humanoid.custom"
		if slim {
			id += "Slim"
		}
		s.Geometry = skintest.Edit(t, s.Geometry, id, f)
		return s
	}
	scale := func(k float64) func(b []map[string]any) []map[string]any {
		return func(b []map[string]any) []map[string]any {
			for _, bone := range b {
				cubes, _ := bone["cubes"].([]any)
				for _, c := range cubes {
					cm := c.(map[string]any)
					for _, key := range []string{"origin", "size"} {
						v := cm[key].([]any)
						for i := range v {
							v[i] = v[i].(float64) * k
						}
					}
				}
			}
			return b
		}
	}
	isOdd(edit(false, scale(2)), "bigger")
	isOdd(edit(false, scale(0.3)), "smaller")
	isOdd(edit(false, func(b []map[string]any) []map[string]any {
		// Wings: a wide plane on the body.
		return append(b, map[string]any{"name": "wings", "parent": "body", "pivot": []float64{0, 24, 0},
			"cubes": []any{skintest.Cube([3]float64{-16, 14, 2}, [3]float64{32, 12, 0}, [2]float64{0, 0})}})
	}), "bigger")
	isOdd(edit(false, func(b []map[string]any) []map[string]any {
		// The head and hat gone: a headless player.
		skintest.Bone(b, "head")["cubes"] = []any{}
		skintest.Bone(b, "hat")["cubes"] = []any{}
		return b
	}), "head hidden")
	isOdd(edit(true, func(b []map[string]any) []map[string]any {
		// A halo floating over the head.
		return append(b, map[string]any{"name": "halo", "parent": "head", "pivot": []float64{0, 24, 0},
			"cubes": []any{skintest.Cube([3]float64{-3, 33.4, -3}, [3]float64{6, 0, 6}, [2]float64{0, 0})}})
	}), "floating")
	isOdd(edit(false, func(b []map[string]any) []map[string]any {
		// The left arm held out sideways.
		skintest.Bone(b, "leftArm")["rotation"] = []float64{0, 0, 90}
		return b
	}), "bigger")
	notOdd(skintest.Custom(t)) // custom, but within the standard shape
	s = classic(t, false)
	s.Patch = skintest.Patch("geometry.nowhere", "")
	isOdd(s, "not found")
}

// Real geometry from other projects and the Bedrock client, kept out of the
// repo: OVERMIND_SKIN_FIXTURES/geometry/<file>.json, each with the identifier
// to test and whether it is odd in cases.json.
func TestOddShapesOnFixtures(t *testing.T) {
	dir := skintest.Fixtures(t)
	data, err := os.ReadFile(filepath.Join(dir, "geometry", "cases.json"))
	require.NoError(t, err)
	var cases []struct {
		File, ID string
		Odd      bool
		Kind     string
	}
	require.NoError(t, json.Unmarshal(data, &cases))
	for _, c := range cases {
		g, err := os.ReadFile(filepath.Join(dir, "geometry", c.File))
		require.NoError(t, err)
		s := skin.New(64, 64)
		s.Pix = skintest.Image(64, 64, 5)
		s.Model = g
		s.ModelConfig = skin.ModelConfig{Default: c.ID}
		odd, why := skins.OddShape(s)
		require.Equal(t, c.Odd, odd, "%s %s: %s", c.File, c.ID, why)
		if c.Kind != "" {
			require.Equal(t, c.Kind, skins.Classify(s).Kind.String(), "%s %s", c.File, c.ID)
		}
		t.Logf("%s %s: odd %v %s, %s", c.File, c.ID, odd, why, skins.Classify(s).Kind)
	}
}

func readPNG(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	require.NoError(t, err)
	defer f.Close()
	img, err := png.Decode(f)
	require.NoError(t, err)
	b := img.Bounds()
	out := make([]byte, 0, b.Dx()*b.Dy()*4)
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			out = append(out, c.R, c.G, c.B, c.A)
		}
	}
	return out
}
