package javafront

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/internal/tables/java261"
)

// The client works out its global palette width from its registries, so ours
// has to agree: ceil(log2(entries)). The block side is checked by
// TestEveryPaletteKindDecodes, whose decoder works out the width itself.
func TestDirectPaletteWidths(t *testing.T) {
	biomes := 0
	for _, r := range java261.Registries() {
		if r.ID == "minecraft:worldgen/biome" {
			biomes = len(r.Entries)
		}
	}
	require.Equal(t, 65, biomes, "biome count changed; check directBiomeBits")
	require.Less(t, biomes, 1<<directBiomeBits)
	require.Greater(t, biomes, 1<<(directBiomeBits-1))
}

func TestPackedLongs(t *testing.T) {
	require.Equal(t, 37, packedLongs(256, heightmapBits)) // 7 a long
	require.Equal(t, 256, packedLongs(4096, 4))           // 16 a long
	require.Equal(t, 1024, packedLongs(4096, directBlockBits))
}
