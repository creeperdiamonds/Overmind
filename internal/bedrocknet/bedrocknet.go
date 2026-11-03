// Package bedrocknet sets up how Bedrock players reach the server: RakNet over UDP, and
// NetherNet (HTTP signalling on TCP, then WebRTC over UDP), which Bedrock 26.50 and later
// clients use. The transports themselves are gophertunnel's and go-nethernet's, listened on
// through the Dragonfly fork; NOTES.md describes the protocol and what was checked against
// the vanilla server.
package bedrocknet

import (
	"fmt"
	"slices"

	"github.com/df-mc/dragonfly/server"

	"github.com/creeperdiamonds/overmind/internal/config"
)

// Transport names, as cfg.BedrockTransports spells them.
const (
	RakNet    = "raknet"
	NetherNet = "nethernet"
)

// Configure sets uc's Bedrock transports from cfg. RakNet listens on UDP at cfg.BedrockAddr;
// NetherNet's signalling listens on TCP at the same address, its game traffic on
// cfg.NetherNetUDPPorts. An empty list means RakNet alone, as before NetherNet existed.
func Configure(uc *server.UserConfig, cfg config.Config) error {
	uc.Network.Address = cfg.BedrockAddr
	transports := cfg.BedrockTransports
	if len(transports) == 0 {
		transports = []string{RakNet}
	}
	for _, t := range transports {
		if t != RakNet && t != NetherNet {
			return fmt.Errorf("unknown Bedrock transport %q: want %q or %q", t, RakNet, NetherNet)
		}
	}
	uc.Network.Transport = slices.Clone(transports)
	// The signalling shares BedrockAddr: TCP there is free, RakNet holds only UDP.
	uc.Network.NetherNet.Address = cfg.BedrockAddr
	uc.Network.NetherNet.UDPPorts = cfg.NetherNetUDPPorts
	uc.Network.NetherNet.KeyFile = cfg.NetherNetKeyFile
	// BDS answers with this domain too; the client shows it in the one-time trust prompt.
	uc.Network.NetherNet.Domain = "self"
	return nil
}
