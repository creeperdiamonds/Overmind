package worldgen

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// TestCoralMatchesBDS scores Bedrock's coral_feature and coral_hang_feature (bedrock_coral.go) on the
// windows holding warm ocean, without them (Java's warm_ocean_vegetation standing in, no hang) and with
// them: the window's stable cells, and over every cell the live coral families with their colour
// (BDS's count, ours, both at the cell with the same block name; recall and precision).
func TestCoralMatchesBDS(t *testing.T) {
	bd := testBedrockData(t)
	before := dataWithout(t, bd, "minecraft:coral_feature", "minecraft:coral_hang_feature")
	fams := []string{"coral block", "coral fan", "coral wall fan"}
	names := blockNames()
	for _, win := range []string{"warm_ocean_37", "lukewarm_ocean_31", "mushroom_island_11"} {
		file := filepath.Join(goldenBedrockDir(t), "whole", "windows", win+".bin.gz")
		w := loadBDSFile(t, file)
		gens := [2]*Generator{NewWithData(w.seed, ProfileBedrock, before), NewWithData(w.seed, ProfileBedrock, bd)}
		type tally struct{ bds, ours, both int }
		var per [2]map[string]*tally
		for i := range per {
			per[i] = map[string]*tally{}
			for _, f := range fams {
				per[i][f] = &tally{}
			}
		}
		var same [2]int
		var cells int
		for k, c := range w.chunks {
			for i, g := range gens {
				ch := generate(g, int32(k[0]), int32(k[1]))
				for y := minY; y < maxY; y++ {
					for z := range 16 {
						for x := range 16 {
							cell := cellIndex(x, y, z)
							ours, real := names[ch.Block(uint8(x), int16(y), uint8(z), 0)].name, w.names[c.ids[cell]]
							if !c.masked(cell) {
								if i == 0 {
									cells++
								}
								if ours == real {
									same[i]++
								}
							}
							fo, fr := crustFamily(ours), crustFamily(real)
							if fr != "" && per[i][fr] != nil {
								per[i][fr].bds++
							}
							if fo != "" && per[i][fo] != nil {
								per[i][fo].ours++
								if ours == real {
									per[i][fo].both++
								}
							}
						}
					}
				}
			}
		}
		var lines []string
		for _, f := range fams {
			a, b := per[0][f], per[1][f]
			lines = append(lines, fmt.Sprintf("  %-15s BDS %6d; before: ours %6d both %6d (recall %.2f%%, precision %.2f%%); after: ours %6d both %6d (recall %.2f%%, precision %.2f%%)",
				f, b.bds, a.ours, a.both, pct(a.both, a.bds), pct(a.both, a.ours), b.ours, b.both, pct(b.both, b.bds), pct(b.both, b.ours)))
		}
		t.Logf("%s (%d chunks): %.4f%% -> %.4f%% of %d stable cells\n%s", win, len(w.chunks),
			100*float64(same[0])/float64(max(cells, 1)), 100*float64(same[1])/float64(max(cells, 1)), cells, strings.Join(lines, "\n"))
		bdsWorlds.Delete(file)
	}
}

func pct(a, b int) float64 { return 100 * float64(a) / float64(max(b, 1)) }
