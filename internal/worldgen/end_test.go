package worldgen

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"
)

// endChunk generates an End chunk the way the server does.
func endChunk(t *testing.T, g *Generator, cx, cz int) *chunk.Chunk {
	t.Helper()
	c := chunk.New(world.DefaultBlockRegistry, world.End.Range())
	g.GenerateChunk(world.ChunkPos{int32(cx), int32(cz)}, c)
	return c
}

func endBlockAt(c *chunk.Chunk, x, y, z int) world.Block {
	b, _ := world.BlockByRuntimeID(c.Block(uint8(x&15), int16(y), uint8(z&15), 0))
	return b
}

// TestEndMainIsland checks the main island's shape on seed 100: end stone under the exit portal, the
// portal's bedrock pillar and rim, its torches, and every spike's obsidian down to Y 0 with a cage on
// the two guarded ones.
func TestEndMainIsland(t *testing.T) {
	g := NewWithProfile(100, ProfileBedrock).End()
	sc := newScratch()
	top := g.endPodiumTop(sc)
	if top < 40 || top > 80 {
		t.Fatalf("the island's top at (0, 0) is Y %d", top)
	}
	c := endChunk(t, g, 0, 0)
	for y := top + 1; y < top+5; y++ {
		if b, ok := endBlockAt(c, 0, y, 0).(block.Bedrock); !ok || !b.InfiniteBurning {
			t.Errorf("no burning bedrock pillar at Y %d: %#v", y, endBlockAt(c, 0, y, 0))
		}
	}
	if _, ok := endBlockAt(c, 3, top+1, 0).(block.Bedrock); !ok {
		t.Errorf("no bedrock rim at (3, %d, 0)", top+1)
	}
	if _, ok := endBlockAt(c, 2, top+1, 0).(block.Air); !ok {
		t.Errorf("the inactive portal is not open at (2, %d, 0): %#v", top+1, endBlockAt(c, 2, top+1, 0))
	}
	if tb, ok := endBlockAt(c, 1, top+3, 0).(block.Torch); !ok || tb.Facing.String() != "west" {
		t.Errorf("no torch on the pillar's east side: %#v", endBlockAt(c, 1, top+3, 0))
	}
	if _, ok := endBlockAt(c, 5, top-3, 5).(block.EndStone); !ok {
		t.Errorf("no end stone under the island's top: %#v", endBlockAt(c, 5, top-3, 5))
	}
	pc := endChunk(t, g, 6, 0)
	if _, ok := endBlockAt(pc, 100, 48, 0).(block.Obsidian); !ok {
		t.Errorf("no obsidian platform under the End's spawn: %#v", endBlockAt(pc, 100, 48, 0))
	}
	if sp := g.DefaultSpawn(world.End); sp != (cube.Pos{100, 49, 0}) {
		t.Errorf("End spawn %v", sp)
	}
	guarded := 0
	for _, sp := range g.endN.spikes {
		sc := endChunk(t, g, sp.x>>4, sp.z>>4)
		for _, y := range []int{0, sp.h / 2, sp.h - 1} {
			if _, ok := endBlockAt(sc, sp.x, y, sp.z).(block.Obsidian); !ok {
				t.Errorf("spike at %d, %d: no obsidian at Y %d", sp.x, sp.z, y)
			}
		}
		if _, ok := endBlockAt(sc, sp.x, sp.h, sp.z).(block.Air); !ok {
			t.Errorf("spike at %d, %d: its top at Y %d is not open (no cap or crystal at worldgen)", sp.x, sp.z, sp.h)
		}
		if sp.guarded {
			guarded++
			if _, ok := endBlockAt(sc, sp.x, sp.h+3, sp.z).(block.IronBars); !ok {
				t.Errorf("guarded spike at %d, %d: no cage lid", sp.x, sp.z)
			}
		}
	}
	if guarded != 2 {
		t.Errorf("%d guarded spikes, want 2", guarded)
	}
}

// TestEndOuterGateway checks the outer islands at seed 100's one natural gateway in the BDS world,
// (911, 64, 941): the gateway block, with burning bedrock above and below it, and chorus around.
func TestEndOuterGateway(t *testing.T) {
	g := NewWithProfile(100, ProfileBedrock).End()
	c := endChunk(t, g, 911>>4, 941>>4)
	if b := endBlockAt(c, 911, 64, 941); b == nil {
		t.Fatal("no block at the gateway")
	} else if n, _ := b.EncodeBlock(); n != "minecraft:end_gateway" {
		t.Errorf("at the gateway: %s", n)
	}
	for _, y := range []int{62, 63, 65, 66} {
		if _, ok := endBlockAt(c, 911, y, 941).(block.Bedrock); !ok {
			t.Errorf("no bedrock at Y %d over or under the gateway", y)
		}
	}
	chorus := 0
	for cx := 54; cx <= 58; cx++ {
		for cz := 56; cz <= 60; cz++ {
			cc := endChunk(t, g, cx, cz)
			for y := 50; y < 128; y++ {
				for i := range 256 {
					if n, _ := endBlockAt(cc, i&15, y, i>>4).EncodeBlock(); n == "minecraft:chorus_flower" {
						chorus++
					}
				}
			}
		}
	}
	if chorus == 0 {
		t.Error("no chorus flowers on the outer islands around the gateway")
	}
}

// BenchmarkEndChunk generates fresh outer-island chunks, a new generator (and cache) per 64.
func BenchmarkEndChunk(b *testing.B) {
	var g *Generator
	for i := range b.N {
		if i%64 == 0 {
			g = NewWithProfile(100, ProfileBedrock).End()
		}
		c := chunk.New(world.DefaultBlockRegistry, world.End.Range())
		g.GenerateChunk(world.ChunkPos{int32(56 + i%8), int32(56 + i/8%8)}, c)
	}
}

// BenchmarkNetherChunk generates fresh decorated Nether chunks (with the data when the tests have it),
// a new generator (and cache) per 64.
func BenchmarkNetherChunk(b *testing.B) {
	var g *Generator
	for i := range b.N {
		if i%64 == 0 {
			g = NewWithProfile(100, ProfileBedrock).Nether()
		}
		c := chunk.New(world.DefaultBlockRegistry, world.Nether.Range())
		g.GenerateChunk(world.ChunkPos{int32(i % 8), int32(i / 8 % 8)}, c)
	}
}
