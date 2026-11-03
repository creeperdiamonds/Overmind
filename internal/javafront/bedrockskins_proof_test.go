package javafront_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/skins"
	"github.com/overmind/overmind/internal/skins/skintest"
	"github.com/overmind/overmind/internal/testserver"
)

// Bedrock skins as each edition is shown them, with Bedrock bots sending real
// skin data (gophertunnel's classic geometry, edited for the other kinds) and a
// Java bot reading its player list and entities.

type javaRow struct {
	Known      bool   `json:"known"`
	UUID       string `json:"uuid"`
	Properties []struct {
		Name      string  `json:"name"`
		Value     string  `json:"value"`
		Signature *string `json:"signature"`
	} `json:"properties"`
}

// row is the Java client's player list row for a name, once it has one.
func row(t *testing.T, j *javaBot, name string) javaRow {
	t.Helper()
	var r javaRow
	require.Eventually(t, func() bool { r = javaRow{}; j.call("profile", map[string]any{"name": name}, &r); return r.Known }, 15*time.Second, 50*time.Millisecond, "no row for %s", name)
	return r
}

// textures is a row's textures property, "" if none.
func (r javaRow) textures() (value, signature string) {
	for _, p := range r.Properties {
		if p.Name == "textures" {
			if p.Signature != nil {
				signature = *p.Signature
			}
			return p.Value, signature
		}
	}
	return "", ""
}

// entityUUID is the UUID the Java client's entity for a name carries.
func entityUUID(t *testing.T, j *javaBot, name string) string {
	t.Helper()
	var id string
	require.Eventually(t, func() bool {
		var r struct {
			Players map[string]struct {
				UUID string `json:"uuid"`
			} `json:"players"`
		}
		j.call("players", nil, &r)
		p, ok := r.Players[name]
		id = p.UUID
		return ok
	}, 15*time.Second, 50*time.Millisecond, "Java never saw %s", name)
	return id
}

// javaDefault is the default skin a Java client draws for a row.
func javaDefault(t *testing.T, r javaRow) string {
	t.Helper()
	return skins.DefaultName(skins.JavaDefault(uuid.MustParse(r.UUID)))
}

// bedrockSees waits for the watcher's view of a player's skin.
func bedrockSees(t *testing.T, w *bedrock.Bot, name string) bedrock.SkinSeen {
	t.Helper()
	var s bedrock.SkinSeen
	require.Eventually(t, func() bool { var ok bool; s, ok = w.Skin(name); return ok }, 15*time.Second, 50*time.Millisecond, "Bedrock never listed %s", name)
	return s
}

// requireUnchanged checks a Bedrock viewer got the skin's own bytes.
func requireUnchanged(t *testing.T, got bedrock.SkinSeen, sent skintest.Skin, name string) {
	t.Helper()
	require.Equal(t, sent.W, got.Width, name)
	require.Equal(t, sent.H, got.Height, name)
	require.True(t, bytes.Equal(sent.Pix, got.Pix), "%s: Bedrock got other pixels", name)
	require.JSONEq(t, string(sent.Geometry), got.Geometry, "%s: Bedrock got other geometry", name)
	gotCfg, err := skin.DecodeModelConfig([]byte(got.Patch))
	require.NoError(t, err)
	wantCfg, _ := skin.DecodeModelConfig(sent.Patch)
	require.Equal(t, wantCfg, gotCfg, name)
	require.Equal(t, sent.Persona, got.Persona, name)
}

func classicSkin(t *testing.T, seed byte, slim bool) skintest.Skin {
	def := "geometry.humanoid.custom"
	if slim {
		def = "geometry.humanoid.customSlim"
	}
	return skintest.Skin{Pix: skintest.Image(64, 64, seed), W: 64, H: 64, Geometry: skintest.ClassicGeometry(t), Patch: skintest.Patch(def, "")}
}

func extrasSkin(t *testing.T, seed byte) skintest.Skin {
	s := classicSkin(t, seed, false)
	s.Geometry = skintest.WithExtras(t, s.Geometry, "geometry.humanoid.custom")
	s.Patch = skintest.Patch("geometry.humanoid.custom", "geometry.animated_face_persona_test")
	s.CapeW, s.CapeH = 64, 32
	return s
}

func oddSkin(t *testing.T) skintest.Skin {
	s := classicSkin(t, 90, false)
	// Wings: a plane twice the model's width on its back.
	s.Geometry = skintest.Edit(t, s.Geometry, "geometry.humanoid.custom", func(b []map[string]any) []map[string]any {
		return append(b, map[string]any{"name": "wings", "parent": "body", "pivot": []float64{0, 24, 0},
			"cubes": []any{skintest.Cube([3]float64{-16, 14, 2}, [3]float64{32, 12, 0}, [2]float64{0, 0})}})
	})
	return s
}

func joinSkinned(t *testing.T, srv *testserver.Server, name string, s skintest.Skin) *bedrock.Bot {
	t.Helper()
	b, err := bedrock.JoinWith(srv.BedrockAddr, name, uuid.New(), s.ClientData())
	require.NoError(t, err, name)
	t.Cleanup(func() { _ = b.Close() })
	return b
}

// Without an uploader: a classic skin, one with Marketplace extras, a persona
// skin, a custom geometry and an odd shape (blockOddShapes off) all reach
// Bedrock viewers byte for byte, and Java viewers as Steve or Alex by arm
// width, by the UUID their row and entity carry, with no textures.
func TestBedrockSkinsWithoutUploader(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JViewer")
	waitOnGround(t, srv, "JViewer")
	watcher, err := bedrock.Join(srv.BedrockAddr, "BWatcher")
	require.NoError(t, err)
	defer watcher.Close()

	cases := []struct {
		name string
		skin skintest.Skin
		java string
	}{
		{"BClassic", classicSkin(t, 1, false), "wide steve"},
		{"BClassicSlim", classicSkin(t, 2, true), "slim alex"},
		{"BExtras", extrasSkin(t, 3), "wide steve"},
		{"BPersona", skintest.Persona(t), "slim alex"},
		{"BCustom", skintest.Custom(t), "slim alex"}, // its arms are 2 pixels wide
		{"BOdd", oddSkin(t), "wide steve"},
	}
	for _, c := range cases {
		joinSkinned(t, srv, c.name, c.skin)
	}
	for _, c := range cases {
		requireUnchanged(t, bedrockSees(t, watcher, c.name), c.skin, c.name)
		r := row(t, j, c.name)
		require.Equal(t, c.java, javaDefault(t, r), c.name)
		v, _ := r.textures()
		require.Empty(t, v, "%s: textures with no uploader", c.name)
		require.Equal(t, r.UUID, entityUUID(t, j, c.name), "%s: the entity and the row disagree", c.name)
	}
	// The same account and skin get the same UUID next session.
	acc := uuid.New()
	b, err := bedrock.JoinWith(srv.BedrockAddr, "BAgain", acc, classicSkin(t, 7, false).ClientData())
	require.NoError(t, err)
	first := row(t, j, "BAgain").UUID
	require.NoError(t, b.Close())
	require.Eventually(t, func() bool { var r javaRow; j.call("profile", map[string]any{"name": "BAgain"}, &r); return !r.Known }, 10*time.Second, 50*time.Millisecond)
	b, err = bedrock.JoinWith(srv.BedrockAddr, "BAgain", acc, classicSkin(t, 7, false).ClientData())
	require.NoError(t, err)
	defer b.Close()
	require.Equal(t, first, row(t, j, "BAgain").UUID, "a new UUID next session")

	// A skin change: the persona one, slim, makes the row and entity Alex.
	cl := cases[0]
	bot := joinSkinned(t, srv, "BChanger", cl.skin)
	before := row(t, j, "BChanger")
	require.Equal(t, "wide steve", javaDefault(t, before))
	entityUUID(t, j, "BChanger")
	persona := skintest.Persona(t)
	require.NoError(t, bot.ChangeSkin(persona.ProtocolSkin()))
	var after javaRow
	require.Eventually(t, func() bool { after = row(t, j, "BChanger"); return after.UUID != before.UUID }, 10*time.Second, 50*time.Millisecond, "the row did not follow the skin")
	require.Equal(t, "slim alex", javaDefault(t, after))
	require.Eventually(t, func() bool { return entityUUID(t, j, "BChanger") == after.UUID }, 10*time.Second, 50*time.Millisecond, "the entity was not shown again")
	require.Eventually(t, func() bool { s, _ := watcher.Skin("BChanger"); return s.Changes > 0 && bytes.Equal(s.Pix, persona.Pix) }, 10*time.Second, 50*time.Millisecond, "Bedrock did not get the new skin")
}

// With an uploader (against a fake Mojang): Java-format skins are uploaded,
// base image only and once each, and Java rows get the signed textures;
// persona and custom skins are not uploaded; Bedrock still sees the bytes the
// client sent. A Bedrock default skin needs no upload: Java draws its own copy.
func TestBedrockSkinsUploaded(t *testing.T) {
	m := skintest.NewFakeMojang(t)
	up, err := skins.NewUploader(skins.UploaderConfig{Tokens: &staticTokens{}, CacheDir: t.TempDir(), Gap: 10 * time.Millisecond,
		ServicesURL: m.URL(), SessionURL: m.URL(), PollEvery: 10 * time.Millisecond})
	require.NoError(t, err)
	srv := testserver.StartWith(t, func(c *config.Config) { c.Skins.Uploader = up })
	j := startBot(t, srv, "JViewer2")
	waitOnGround(t, srv, "JViewer2")
	watcher, err := bedrock.Join(srv.BedrockAddr, "BWatcher2")
	require.NoError(t, err)
	defer watcher.Close()

	classic, extras := classicSkin(t, 11, true), extrasSkin(t, 12)
	joinSkinned(t, srv, "BUpClassic", classic)
	joinSkinned(t, srv, "BUpExtras", extras)
	joinSkinned(t, srv, "BUpPersona", skintest.Persona(t))
	joinSkinned(t, srv, "BUpCustom", skintest.Custom(t))

	for _, name := range []string{"BUpClassic", "BUpExtras"} {
		var r javaRow
		require.Eventually(t, func() bool { r = row(t, j, name); v, _ := r.textures(); return v != "" }, 20*time.Second, 50*time.Millisecond, "%s: no signed textures", name)
		_, sig := r.textures()
		require.NotEmpty(t, sig, name)
		require.Equal(t, r.UUID, entityUUID(t, j, name))
	}
	// The other bots join with gophertunnel's own black 64x32 classic skin,
	// a Java-format skin too: one upload for all of them.
	require.Equal(t, 3, m.UploadCount(), "persona or custom uploaded, or a skin twice")
	m.Mu.Lock()
	uploaded := map[string]bool{}
	for i, data := range m.Uploads {
		uploaded[skins.Hash(decodePNG(t, data))+"/"+m.Variants[i]] = true
	}
	m.Mu.Unlock()
	require.True(t, uploaded[skins.Hash(classic.Pix)+"/slim"], "the classic skin was not uploaded as itself, slim")
	require.True(t, uploaded[skins.Hash(extras.Pix)+"/classic"], "the extras skin was not uploaded as its base image")
	for _, name := range []string{"BUpPersona", "BUpCustom"} {
		v, _ := row(t, j, name).textures()
		require.Empty(t, v, name)
	}
	// After the uploads too (Dragonfly also sends a skin when it shows a player).
	requireUnchanged(t, bedrockSees(t, watcher, "BUpClassic"), classic, "BUpClassic")
	requireUnchanged(t, bedrockSees(t, watcher, "BUpExtras"), extras, "BUpExtras")

	t.Run("default skins", func(t *testing.T) {
		dir := skintest.Fixtures(t)
		before := m.UploadCount()
		for _, c := range []struct {
			file string
			slim bool
			java string
		}{{"kai", false, "wide kai"}, {"noor", true, "slim noor"}, {"steve", false, "wide steve"}, {"alex", true, "slim alex"}} {
			s := classicSkin(t, 0, c.slim)
			s.Pix = readPNGFile(t, filepath.Join(dir, "bedrock", c.file+".png"))
			name := "BDef" + c.file
			joinSkinned(t, srv, name, s)
			r := row(t, j, name)
			require.Equal(t, c.java, javaDefault(t, r), name)
			v, _ := r.textures()
			require.Empty(t, v, name)
			requireUnchanged(t, bedrockSees(t, watcher, name), s, name)
		}
		time.Sleep(200 * time.Millisecond)
		require.Equal(t, before, m.UploadCount(), "a default skin was uploaded")
	})
}

// blockOddShapes on: an odd shape is our stand-in for Bedrock viewers too, and
// Steve for Java; a standard skin is untouched; a later odd skin is replaced
// as well.
func TestBlockOddShapes(t *testing.T) {
	srv := testserver.StartWith(t, func(c *config.Config) { c.Skins.BlockOddShapes = true })
	j := startBot(t, srv, "JViewer3")
	waitOnGround(t, srv, "JViewer3")
	watcher, err := bedrock.Join(srv.BedrockAddr, "BWatcher3")
	require.NoError(t, err)
	defer watcher.Close()

	standIn := skins.StandIn(false)
	joinSkinned(t, srv, "BWinged", oddSkin(t))
	s := bedrockSees(t, watcher, "BWinged")
	require.True(t, bytes.Equal(standIn.Pix, s.Pix), "Bedrock was shown the odd skin")
	require.Contains(t, s.Patch, "geometry.humanoid.custom")
	require.Equal(t, "wide steve", javaDefault(t, row(t, j, "BWinged")))

	classic := classicSkin(t, 21, false)
	joinSkinned(t, srv, "BPlain", classic)
	requireUnchanged(t, bedrockSees(t, watcher, "BPlain"), classic, "BPlain")

	persona := skintest.Persona(t)
	joinSkinned(t, srv, "BPersona3", persona)
	requireUnchanged(t, bedrockSees(t, watcher, "BPersona3"), persona, "BPersona3")

	changer := joinSkinned(t, srv, "BLater", classicSkin(t, 22, false))
	bedrockSees(t, watcher, "BLater")
	require.NoError(t, changer.ChangeSkin(oddSkin(t).ProtocolSkin()))
	require.Eventually(t, func() bool { s, _ := watcher.Skin("BLater"); return bytes.Equal(standIn.Pix, s.Pix) }, 10*time.Second, 50*time.Millisecond, "a later odd skin was shown")
}

type staticTokens struct{}

func (staticTokens) Token(context.Context) (string, error) { return "test-token", nil }
func (staticTokens) Invalidate()                           {}

func decodePNG(t *testing.T, data []byte) []byte {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(data))
	require.NoError(t, err)
	return rgba(img)
}

func readPNGFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return decodePNG(t, data)
}

func rgba(img image.Image) []byte {
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
