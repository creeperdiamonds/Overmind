package session

import (
	"github.com/df-mc/dragonfly/server/world"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// RequestChunkRadiusHandler handles the RequestChunkRadius packet.
type RequestChunkRadiusHandler struct{}

// Handle ...
func (*RequestChunkRadiusHandler) Handle(p packet.Packet, s *Session, tx *world.Tx, _ Controllable) error {
	pk := p.(*packet.RequestChunkRadius)

	pk.ChunkRadius = clampChunkRadius(pk.ChunkRadius, s.maxChunkRadius) // Overmind fork: a lower bound too
	s.chunkRadius = pk.ChunkRadius

	s.chunkLoader.ChangeRadius(tx, int(pk.ChunkRadius))

	s.writePacket(&packet.ChunkRadiusUpdated{ChunkRadius: s.chunkRadius})
	return nil
}

// minChunkRadius is the smallest radius a client gets (Overmind fork). A player's tick reach is the
// world's tick range held two chunks under its radius, and never under its own chunk, whose blocks
// read the chunks beside it. Radius 2 is that reach of 0 plus 2. Bedrock's own client minimum was not
// checked; any client minimum of 2 or more is unaffected.
const minChunkRadius = 2

// clampChunkRadius holds a client's asked radius between minChunkRadius and the server's maximum.
func clampChunkRadius(asked, most int32) int32 {
	return max(min(asked, most), min(minChunkRadius, most))
}
