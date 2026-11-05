package structures

import (
	"path/filepath"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestIglooMatchesBDS scores seed 10's igloo (start (3, -14); P11.6: rotation 2, a basement, n = 8)
// against the window export, in the box of its top, ladder shaft and lab.
func TestIglooMatchesBDS(t *testing.T) {
	w := loadWorld(t, filepath.Join(goldenDir(t), "whole", "structures", "igloo_10.bin.gz"))
	g, _ := worldFor(t, 10)
	bd, _ := installData(t)
	bare := worldgen.NewWithData(10, worldgen.ProfileBedrock, bd)
	var cells, same, sameBare int
	conf := map[[2]string]int{}
	// The top (x 42..48, z -231..-224 for rotation 2) and the shaft and lab under it.
	b := box{36, 30, -236, 54, 80, -218}
	for _, c := range [][2]int{{2, -15}, {3, -15}, {2, -14}, {3, -14}} {
		col := generate(g, c[0], c[1])
		sm, tt, cf := boxScore(w, col.Chunk, c[0], c[1], []box{b})
		plain := chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())
		bare.GenerateChunk(world.ChunkPos{int32(c[0]), int32(c[1])}, plain)
		smb, _, _ := boxScore(w, plain, c[0], c[1], []box{b})
		cells, same, sameBare = cells+tt, same+sm, sameBare+smb
		for k, v := range cf {
			conf[k] += v
		}
		for _, be := range col.BlockEntities {
			t.Logf("block entity %v %v", be.Pos, be.Data)
		}
	}
	t.Logf("igloo box: %d/%d = %.2f%% same (without structures %.2f%%); %s", same, cells, 100*float64(same)/float64(max(cells, 1)),
		100*float64(sameBare)/float64(max(cells, 1)), topConfusions(conf, 12))
}
