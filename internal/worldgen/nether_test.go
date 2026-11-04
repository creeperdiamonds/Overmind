package worldgen

import "testing"

// TestNetherChunkShape checks a Nether chunk without any data: the bedrock floor and roof rows as the
// code lays them (2 to 5 rows of floor, 1 to 5 of roof), netherrack, the lava sea below Y 32 only, and
// the same chunk twice.
func TestNetherChunkShape(t *testing.T) {
	n := NewNetherWithData(100, nil)
	var a, b [netherHeight * 256]bs
	lava, rack := 0, 0
	for cx := -2; cx < 2; cx++ {
		for cz := -2; cz < 2; cz++ {
			n.cells(cx, cz, &a)
			n.cells(cx, cz, &b)
			if a != b {
				t.Fatalf("chunk %d,%d differs between two builds", cx, cz)
			}
			for x := range 16 {
				for z := range 16 {
					floor, roof := 0, 0
					for y := 0; y < 6 && a[y<<8|z<<4|x] == sBedrock; y++ {
						floor++
					}
					for y := netherHeight - 1; y > 121 && a[y<<8|z<<4|x] == sBedrock; y-- {
						roof++
					}
					if floor < 2 || floor > 5 || roof < 1 || roof > 5 {
						t.Fatalf("column %d,%d of chunk %d,%d: %d floor and %d roof rows", x, z, cx, cz, floor, roof)
					}
					for y := range netherHeight {
						switch a[y<<8|z<<4|x] {
						case sLava:
							lava++
							if y >= netherLava {
								t.Fatalf("lava at Y %d, above the sea", y)
							}
						case sNetherrack:
							rack++
						}
					}
				}
			}
		}
	}
	if lava == 0 || rack == 0 {
		t.Errorf("%d lava and %d netherrack blocks in 16 chunks", lava, rack)
	}
}

// TestNetherVineStates: weeping and twisting vines resolve to Bedrock blocks with their age.
func TestNetherVineStates(t *testing.T) {
	for _, n := range []string{"weeping_vines[age=0]", "weeping_vines[age=25]", "twisting_vines[age=7]", "weeping_vines_plant", "twisting_vines_plant"} {
		s, err := safeState(n)
		if err != nil {
			t.Fatalf("%s: %v", n, err)
		}
		name, props := dragonflyBlock(s.name()).EncodeBlock()
		if name != "minecraft:weeping_vines" && name != "minecraft:twisting_vines" || len(props) != 1 {
			t.Errorf("%s: %s %v", n, name, props)
		}
	}
}
