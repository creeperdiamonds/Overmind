package app

import (
	"net"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/stretchr/testify/require"
)

// At one below the limit, a Bedrock player who has passed Allow but is not yet
// online holds the last slot, and a Java join must see that.
func TestSimultaneousJoinsAtTheLimit(t *testing.T) {
	java := 1
	n := &javaNames{maxPlayers: 2, reserved: map[string]reservation{},
		bedrockOnline: func() int { return 0 }, javaCount: func() int { return java }}

	_, ok := n.Allow(&net.UDPAddr{}, login.IdentityData{DisplayName: "First", Identity: "a"}, login.ClientData{})
	require.True(t, ok, "the last slot was refused")
	require.Equal(t, 1, n.otherPlayers(), "a Bedrock player on the way does not count against the limit")

	_, ok = n.Allow(&net.UDPAddr{}, login.IdentityData{DisplayName: "Second", Identity: "b"}, login.ClientData{})
	require.False(t, ok, "two players got the last slot")

	n.arrived("First")
	require.Equal(t, 0, n.otherPlayers(), "a Bedrock player who arrived counts twice (online and on the way)")
}
