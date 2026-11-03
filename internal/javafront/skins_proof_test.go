package javafront_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// Each edition sees the other's players with a skin it can draw. Bedrock gets
// a Java player's skin as 64x64 RGBA on Bedrock's built-in player model (an
// offline bot has no Mojang skin, so the stand-in, on the wide model). Java
// gets a Bedrock player's row with no textures property, so the Java client
// draws its own default skin: an unsigned property would not be valid to it.
func TestSkinsAcrossEditions(t *testing.T) {
	srv := testserver.Start(t)
	j := startBot(t, srv, "JSkinned")
	waitOnGround(t, srv, "JSkinned")
	b, err := bedrock.Join(srv.BedrockAddr, "BSkinned")
	require.NoError(t, err)
	defer b.Close()

	var s bedrock.SkinSeen
	require.Eventually(t, func() bool { var ok bool; s, ok = b.Skin("JSkinned"); return ok }, 10*time.Second, 50*time.Millisecond, "Bedrock never listed the Java player")
	require.Equal(t, 64, s.Width)
	require.Equal(t, 64, s.Height)
	require.Equal(t, 64*64*4, s.Data)
	var patch struct {
		Geometry struct {
			Default string `json:"default"`
		} `json:"geometry"`
	}
	require.NoError(t, json.Unmarshal([]byte(s.Patch), &patch), s.Patch)
	require.Equal(t, "geometry.humanoid.custom", patch.Geometry.Default)

	var prof struct {
		Known      bool              `json:"known"`
		Properties []json.RawMessage `json:"properties"`
	}
	require.Eventually(t, func() bool { j.call("profile", map[string]any{"name": "BSkinned"}, &prof); return prof.Known }, 10*time.Second, 50*time.Millisecond)
	for _, p := range prof.Properties {
		require.False(t, strings.Contains(string(p), "textures"), "a Bedrock player was sent textures: %s", p)
	}
}
