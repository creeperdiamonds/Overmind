package javafront_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"sync/atomic"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/config"
	"github.com/overmind/overmind/internal/skins"
	"github.com/overmind/overmind/internal/testserver"
)

const javaSkinURL = "http://textures.minecraft.net/texture/0f1e2d3c4b5a69788796a5b4c3d2e1f0"

// javaSkin is a 64x64 skin PNG with every pixel opaque and its own, and the
// RGBA a Bedrock viewer must get for it.
func javaSkin(t *testing.T) (pngData, rgba []byte) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 4), G: uint8(y * 4), B: uint8(x ^ y), A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes(), img.Pix
}

// skinServer starts a server whose offline Java logins carry a textures
// property naming javaSkinURL, and whose skin fetches wait for release (nil
// does not wait) and return pngData.
func skinServer(t *testing.T, pngData []byte, release <-chan struct{}, fetches *atomic.Int32) *testserver.Server {
	props := base64.StdEncoding.EncodeToString(fmt.Appendf(nil, `{"textures":{"SKIN":{"url":%q}}}`, javaSkinURL))
	return testserver.StartWith(t, func(c *config.Config) {
		c.Skins.JavaOfflineTextures = func(string) string { return props }
		c.Skins.JavaFetch = func(ctx context.Context, url string) ([]byte, error) {
			if url != javaSkinURL {
				return nil, fmt.Errorf("fetched %s", url)
			}
			if release != nil {
				select {
				case <-release:
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			fetches.Add(1)
			return pngData, nil
		}
	})
}

// waitJavaWears waits until the server has put pix on a Java player.
func waitJavaWears(t *testing.T, srv *testserver.Server, name string, pix []byte) {
	t.Helper()
	require.Eventually(t, func() bool {
		h, ok := srv.Java.Player(name)
		if !ok {
			return false
		}
		got, err := player.Call(context.Background(), h, func(_ *world.Tx, p *player.Player) (bool, error) {
			return bytes.Equal(p.Skin().Pix, pix), nil
		})
		return err == nil && got
	}, 10*time.Second, 50*time.Millisecond, "the server never put the fetched skin on %s", name)
}

// requireBedrockSees waits until a Bedrock bot has the fetched skin for a Java
// player in both its list row and its latest skin, and checks the flags.
func requireBedrockSees(t *testing.T, b *bedrock.Bot, name string, pix []byte, why string) {
	t.Helper()
	var s bedrock.SkinSeen
	ok := assertEventually(func() bool { s, _ = b.Skin(name); return bytes.Equal(s.Pix, pix) && bytes.Equal(s.RowPix, pix) })
	require.True(t, ok, "%s: Bedrock never got the fetched skin (latest skin right: %v, list row right: %v, list row is the stand-in: %v, rows %d, changes %d)",
		why, bytes.Equal(s.Pix, pix), bytes.Equal(s.RowPix, pix), bytes.Equal(s.RowPix, skins.StandIn(false).Pix), s.Rows, s.Changes)
	require.True(t, s.Raw.Trusted, why)
	require.True(t, s.Raw.OverrideAppearance, why)
}

func assertEventually(cond func() bool) bool {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return cond()
}

// A Java player's fetched Mojang skin reaches Bedrock players in the player
// list row as well as by PlayerSkin, in either join order and when it arrives
// after a Bedrock player is already there. A list row carrying the stand-in,
// sent with OverrideAppearance, keeps a Bedrock client drawing the stand-in.
func TestJavaSkinReachesBedrock(t *testing.T) {
	pngData, pix := javaSkin(t)

	t.Run("java first", func(t *testing.T) {
		var fetches atomic.Int32
		srv := skinServer(t, pngData, nil, &fetches)
		startBot(t, srv, "JSkinA")
		waitJavaWears(t, srv, "JSkinA", pix)
		b, err := bedrock.Join(srv.BedrockAddr, "BSkinA")
		require.NoError(t, err)
		defer b.Close()
		var s bedrock.SkinSeen
		require.Eventually(t, func() bool { s, _ = b.Skin("JSkinA"); return s.Rows > 0 }, 10*time.Second, 50*time.Millisecond)
		require.True(t, bytes.Equal(s.RowPix, pix), "the first list row a later Bedrock player gets is not the fetched skin (stand-in: %v)", bytes.Equal(s.RowPix, skins.StandIn(false).Pix))
		requireBedrockSees(t, b, "JSkinA", pix, "java first")
		require.EqualValues(t, 1, fetches.Load())
	})

	t.Run("bedrock first", func(t *testing.T) {
		var fetches atomic.Int32
		srv := skinServer(t, pngData, nil, &fetches)
		b, err := bedrock.Join(srv.BedrockAddr, "BSkinB")
		require.NoError(t, err)
		defer b.Close()
		startBot(t, srv, "JSkinB")
		waitJavaWears(t, srv, "JSkinB", pix)
		requireBedrockSees(t, b, "JSkinB", pix, "bedrock first")
	})

	t.Run("skin after viewer", func(t *testing.T) {
		var fetches atomic.Int32
		release := make(chan struct{})
		srv := skinServer(t, pngData, release, &fetches)
		startBot(t, srv, "JSkinC")
		b, err := bedrock.Join(srv.BedrockAddr, "BSkinC")
		require.NoError(t, err)
		defer b.Close()
		require.Eventually(t, func() bool { s, _ := b.Skin("JSkinC"); return s.Rows > 0 && s.Changes > 0 }, 10*time.Second, 50*time.Millisecond, "the Java player was never shown")
		s, _ := b.Skin("JSkinC")
		require.True(t, bytes.Equal(s.Pix, skins.StandIn(false).Pix), "the stand-in was not shown before the fetch")
		close(release)
		waitJavaWears(t, srv, "JSkinC", pix)
		requireBedrockSees(t, b, "JSkinC", pix, "skin after viewer")

		late, err := bedrock.Join(srv.BedrockAddr, "BSkinC2")
		require.NoError(t, err)
		defer late.Close()
		require.Eventually(t, func() bool { s, _ = late.Skin("JSkinC"); return s.Rows > 0 }, 10*time.Second, 50*time.Millisecond)
		require.True(t, bytes.Equal(s.RowPix, pix), "a Bedrock player joining after the fetch got a stale list row")
		requireBedrockSees(t, late, "JSkinC", pix, "late viewer")
	})
}
