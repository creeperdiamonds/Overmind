package javafront

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/player/skin"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/javawire"
	"github.com/creeperdiamonds/overmind/internal/skins"
)

// textures is a Mojang textures property naming a skin.
func textures(url string, slim bool) string {
	meta := ""
	if slim {
		meta = `,"metadata":{"model":"slim"}`
	}
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(
		`{"timestamp":1,"profileId":"0","profileName":"T","textures":{"SKIN":{"url":%q%s}}}`, url, meta)))
}

// skinPNG is a w by h skin with one marked pixel.
func skinPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(8, 9, color.NRGBA{R: 200, G: 10, B: 30, A: 255})
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, img))
	return b.Bytes()
}

func TestSkinOf(t *testing.T) {
	s, ok := skinOf(textures("http://textures.minecraft.net/texture/abc", true))
	require.True(t, ok)
	require.Equal(t, "http://textures.minecraft.net/texture/abc", s.URL)
	require.True(t, s.Slim)
	s, ok = skinOf(textures("http://textures.minecraft.net/texture/abc", false))
	require.True(t, ok)
	require.False(t, s.Slim)
	for _, bad := range []string{"", "not base64!", base64.StdEncoding.EncodeToString([]byte(`{"textures":{}}`))} {
		_, ok := skinOf(bad)
		require.False(t, ok, bad)
	}
}

// Only Mojang's texture host is asked, whatever a property says.
func TestSkinsComeFromMojangOnly(t *testing.T) {
	for _, u := range []string{
		"http://evil.example/texture/x",
		"http://textures.minecraft.net.evil.example/x",
		"http://user@textures.minecraft.net/x",
		"http://textures.minecraft.net:8080/x",
		"file:///etc/passwd",
	} {
		_, err := fetchMojangSkin(context.Background(), u)
		require.ErrorIs(t, err, errSkinHost, u)
	}
}

func TestDecodeSkin(t *testing.T) {
	s, err := decodeSkin(skinPNG(t, 64, 64), true)
	require.NoError(t, err)
	require.Equal(t, geometrySlim, s.ModelConfig.Default)
	i := (9*64 + 8) * 4
	require.Equal(t, []uint8{200, 10, 30, 255}, s.Pix[i:i+4])
	require.Equal(t, 64*64*4, len(s.Pix))

	// A legacy 64x32 skin becomes 64x64 on the same model, its left leg and
	// arm the right ones.
	legacy := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	legacy.Set(2, 20, color.NRGBA{R: 1, G: 2, B: 3, A: 255})  // right leg
	legacy.Set(42, 18, color.NRGBA{R: 4, G: 5, B: 6, A: 255}) // right arm
	var lb bytes.Buffer
	require.NoError(t, png.Encode(&lb, legacy))
	s, err = decodeSkin(lb.Bytes(), false)
	require.NoError(t, err, "the legacy size is a skin too")
	require.Equal(t, geometryWide, s.ModelConfig.Default)
	require.Equal(t, 64*64*4, len(s.Pix))
	px := func(x, y int) []uint8 { i := (y*64 + x) * 4; return s.Pix[i : i+4] }
	require.Equal(t, []uint8{1, 2, 3, 255}, px(18, 52), "the left leg is not the right one")
	require.Equal(t, []uint8{4, 5, 6, 255}, px(34, 50), "the left arm is not the right one")

	_, err = decodeSkin(skinPNG(t, 32, 32), false)
	require.Error(t, err)
	_, err = decodeSkin(make([]byte, maxSkinPNG+1), false)
	require.Error(t, err)
	_, err = decodeSkin([]byte("not a png"), false)
	require.Error(t, err)
}

// One fetch per skin however many ask at once; a failed fetch is not kept;
// the cache drops its oldest past its size.
func TestSkinCache(t *testing.T) {
	var fetches atomic.Int32
	good := skinPNG(t, 64, 64)
	sc := newSkinCache(func(_ context.Context, u string) ([]byte, error) {
		fetches.Add(1)
		time.Sleep(20 * time.Millisecond)
		if u == "bad" {
			return nil, fmt.Errorf("offline")
		}
		return good, nil
	})
	var wg sync.WaitGroup
	var oks atomic.Int32
	for range 10 {
		wg.Add(1)
		sc.get(profileSkin{URL: "a"}, func(_ skin.Skin, ok bool) {
			if ok {
				oks.Add(1)
			}
			wg.Done()
		})
	}
	wg.Wait()
	require.EqualValues(t, 1, fetches.Load())
	require.EqualValues(t, 10, oks.Load())

	done := make(chan bool, 1)
	sc.get(profileSkin{URL: "bad"}, func(_ skin.Skin, ok bool) { done <- ok })
	require.False(t, <-done)
	require.Equal(t, 1, sc.len())

	for i := range skinCacheSize + 5 {
		ch := make(chan struct{})
		sc.get(profileSkin{URL: fmt.Sprint("u", i)}, func(skin.Skin, bool) { close(ch) })
		<-ch
	}
	require.Equal(t, skinCacheSize, sc.len())
}

// A Java player joins with the stand-in on its profile's model and gets its
// Mojang skin once it is fetched, which Dragonfly shows to Bedrock viewers.
func TestJavaPlayerGetsItsSkin(t *testing.T) {
	f := testFront(t, proofWorld(t), 2)
	release := make(chan struct{})
	f.skins = newSkinCache(func(context.Context, string) ([]byte, error) {
		<-release
		return skinPNG(t, 64, 64), nil
	})
	a, b := net.Pipe()
	defer b.Close()
	id := testIdentity()
	id.Textures = textures("http://textures.minecraft.net/texture/abc", true)
	c := newConn(f, javawire.NewConn(a), a, id)
	go c.run()
	defer c.CloseConnection()
	go func() {
		client := javawire.NewConn(b)
		for {
			if _, err := client.ReadFrame(); err != nil {
				return
			}
		}
	}()
	skinNow := func() (s skin.Skin) {
		require.Eventually(t, func() bool { return c.visible.Load() }, 5*time.Second, 10*time.Millisecond)
		_, err := player.Call(context.Background(), c.handle, func(_ *world.Tx, p *player.Player) (struct{}, error) {
			s = p.Skin()
			return struct{}{}, nil
		})
		require.NoError(t, err)
		return s
	}
	first := skinNow()
	require.Equal(t, geometrySlim, first.ModelConfig.Default, "the stand-in is not on the profile's model")
	close(release)
	require.Eventually(t, func() bool { s := skinNow(); return s.Pix[(9*64+8)*4] == 200 }, 5*time.Second, 20*time.Millisecond, "the skin never came")
}

// Our own stand-in: a plain person, its overlay layer transparent.
func TestStandInSkin(t *testing.T) {
	s := skins.StandIn(false)
	px := func(x, y int) []uint8 { i := (y*64 + x) * 4; return s.Pix[i : i+4] }
	for _, at := range [][2]int{{4, 12}, {20, 20}, {44, 20}, {4, 20}, {20, 52}, {36, 52}} {
		require.Equal(t, uint8(255), px(at[0], at[1])[3], "base layer at %v is not drawn", at)
	}
	for _, at := range [][2]int{{40, 8}, {20, 36}, {4, 52}, {52, 52}} {
		require.Equal(t, uint8(0), px(at[0], at[1])[3], "overlay at %v is not transparent", at)
	}
	require.NotEqual(t, px(4, 12), px(20, 20), "head and shirt alike")
	require.Equal(t, geometryWide, s.ModelConfig.Default)
}

// A skin fetch follows redirects only to Mojang's texture host over HTTPS.
func TestSkinRedirectsStayOnMojang(t *testing.T) {
	check := func(raw string) error {
		u, err := url.Parse(raw)
		require.NoError(t, err)
		return skinClient.CheckRedirect(&http.Request{URL: u}, []*http.Request{{}})
	}
	require.NoError(t, check("https://textures.minecraft.net/texture/abc"))
	for _, bad := range []string{"http://textures.minecraft.net/texture/abc", "https://evil.example/x", "https://textures.minecraft.net:444/x", "https://u@textures.minecraft.net/x"} {
		require.ErrorIs(t, check(bad), errSkinHost, bad)
	}
}
