package worldgen

import (
	"encoding/json"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world/biome"
)

// TestGradientBuilderReads checks the noise_gradient reader on a builder of our own making (made-up
// values, not the pack's): the specifiers' (min, max] ranges in order, the clamp, and the
// non-replaceable list.
func TestGradientBuilderReads(t *testing.T) {
	raw := `{"type":"minecraft:noise_gradient","noise_descriptor":{"name":"test:made_up_gradient","first_octave":-3,"amplitudes":[0.5,1.0]},
		"non_replaceable_blocks":["minecraft:granite","minecraft:tuff"],"noise_block_specifiers":[{"range":{"min":-0.8,"max":-0.25},"block":"minecraft:dirt"},
		{"range":{"min":0.1,"max":0.3},"block":"minecraft:gravel"},{"range":{"min":0.3,"max":0.9},"block":"minecraft:dirt"}]}`
	var b map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &b); err != nil {
		t.Fatal(err)
	}
	gd, err := parseGradient(b)
	if err != nil {
		t.Fatal(err)
	}
	if gd.name != "test:made_up_gradient" || gd.first != -3 || len(gd.amps) != 2 || len(gd.specs) != 3 {
		t.Fatalf("read %+v", gd)
	}
	granite, tuff := stateOf("granite"), stateOf("tuff")
	if !gd.keep.has(bname(granite)) || !gd.keep.has(bname(tuff)) || gd.keep.has(bname(sStone)) {
		t.Errorf("non-replaceable list: granite %v, tuff %v, stone %v", gd.keep.has(bname(granite)), gd.keep.has(bname(tuff)), gd.keep.has(bname(sStone)))
	}
	if cin, sul := stateOf("cinnabar"), stateOf("sulfur"); bnameString(cin) != "cinnabar" || bnameString(sul) != "sulfur" {
		t.Errorf("client names %s %s", bnameString(cin), bnameString(sul))
	}
	dirt, gravel := stateOf("dirt"), stateOf("gravel")
	for _, c := range []struct {
		v    float64
		want bs
		ok   bool
	}{{-0.8, 0, false}, {-0.79, dirt, true}, {-0.25, dirt, true}, {-0.2, 0, false}, {0.1, 0, false}, {0.2, gravel, true}, {0.3, gravel, true},
		{0.31, dirt, true}, {0.9, dirt, true}, {0.95, 0, false}, {7, 0, false}, {-7, 0, false}} {
		got, ok := gd.pickValue(c.v)
		if ok != c.ok || ok && got != c.want {
			t.Errorf("value %v: %v %v, want %v %v", c.v, got, ok, c.want, c.ok)
		}
	}
	// The clamp: with a specifier reaching 1, any value over 1 takes it.
	clamp := `{"noise_descriptor":{"name":"test:clamp","first_octave":-1,"amplitudes":[1]},"noise_block_specifiers":[{"range":{"min":0.5,"max":1.0},"block":"minecraft:gravel"}]}`
	var m map[string]json.RawMessage
	json.Unmarshal([]byte(clamp), &m)
	if g2, err := parseGradient(m); err != nil {
		t.Fatal(err)
	} else if got, ok := g2.pickValue(5); !ok || got != gravel {
		t.Errorf("value 5 with a specifier to 1: %v %v, want gravel", got, ok)
	}
	for _, bad := range []string{`{"noise_descriptor":{"name":"x","first_octave":5,"amplitudes":[1]}}`, `{"noise_descriptor":{"name":"","amplitudes":[1]}}`} {
		var m map[string]json.RawMessage
		json.Unmarshal([]byte(bad), &m)
		if _, err := parseGradient(m); err == nil {
			t.Errorf("%s: read without an error", bad)
		}
	}
}

// TestBedrockSulfurBuilderOnBDSBiomes runs the subsurface pass over our surface stage with BDS's own
// stored biomes, in every biome window that has sulfur caves, and compares with BDS, as the study
// checked it (T7, T12), so the builder is tested apart from our biomes. Every cinnabar block of BDS's sulfur caves is
// the builder's (features place sulfur, not cinnabar), so cinnabar is held to 99.9%; sulfur and the
// blocks BDS left alone are reported.
func TestBedrockSulfurBuilderOnBDSBiomes(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	files, _ := filepath.Glob(filepath.Join(dir, "whole", "windows", "*.bin.gz"))
	if len(files) == 0 {
		t.Skip("no windows exported in whole/windows")
	}
	sid := uint16(biome.SulfurCaves{}.EncodeBiome())
	ours := map[uint16]uint8{}
	for i, d := range biomeRowDefs {
		ours[uint16(d.EncodeBiome())] = uint8(i)
	}
	none := biomeIdx["plains"] // A biome with no subsurface builder, for ids we do not have.
	cin, sul := stateOf("cinnabar"), stateOf("sulfur")
	var cinN, cinOK, sulN, sulOK, keptN, keptOK int
	for _, f := range files {
		w := loadBDSFile(t, f)
		g := NewWithProfile(w.seed, ProfileBedrock)
		if g.bsurf == nil || g.bsurf.sub[sulfurBiome] == nil {
			t.Fatal("no sulfur caves builder read from the data")
		}
		s := newScratch()
		var b baseChunk
		for k, c := range w.chunks {
			has := false
			for _, id := range c.biomes {
				if id == sid {
					has = true
					break
				}
			}
			if !has {
				continue
			}
			g.buildBase(&b, s, k[0], k[1], stageSurface, 0, 15, 0, 15)
			before := b.b
			for lz := range 16 {
				for lx := range 16 {
					top := min(max(g.preliminarySurface(&s.prelims, k[0]*16+lx, k[1]*16+lz)-8, minY), maxY-1)
					g.subsurfaceColumn(&b, lx, lz, top-minY, func(i int) uint8 {
						if id, ok := ours[c.biomes[cellIndex(lx, minY+i, lz)]]; ok {
							return id
						}
						return none
					})
				}
			}
			for cell := range chunkCells - 256 {
				if c.biomes[cell] != sid || c.masked(cell) {
					continue
				}
				prev := before[cell]
				if prev.isAir() || prev.has(fWater) || prev.has(fLava) || prev == sBedrock {
					continue // Our caves or water differ there; the builder would not run.
				}
				real, got := w.names[c.ids[cell]], b.b[cell]
				switch {
				case real == "cinnabar":
					cinN++
					if got == cin {
						cinOK++
					}
				case real == "sulfur":
					sulN++
					if got == sul {
						sulOK++
					}
				case strings.Contains(real, "stone") || strings.Contains(real, "deepslate") || real == "tuff" || real == "granite" ||
					real == "diorite" || real == "andesite" || real == "dirt" || real == "gravel":
					keptN++
					if got == prev {
						keptOK++
					}
				}
			}
		}
	}
	t.Logf("cinnabar %d/%d, sulfur %d/%d, rock BDS left %d/%d left", cinOK, cinN, sulOK, sulN, keptOK, keptN)
	if cinN == 0 || float64(cinOK) < 0.999*float64(cinN) {
		t.Errorf("cinnabar: %d of %d match BDS", cinOK, cinN)
	}
}

// TestBedrockQuartBiomesMatchBDS reports how many of the quarts BDS stores as one biome get the same
// biome in the quart model (what the chunk stores for the client and Java's placements read), per test
// seed and biome window, with sulfur caves' recall. Nothing is held but sulfur caves appearing where
// BDS has them; the block model (TestBedrockBlockBiomesMatchBDS) is the exact one.
func TestBedrockQuartBiomesMatchBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	files := []string{}
	for _, s := range bedrockTestSeeds {
		files = append(files, filepath.Join(dir, "whole", "seed"+strconv.FormatInt(s, 10)+".bin.gz"))
	}
	w2, _ := filepath.Glob(filepath.Join(dir, "whole", "windows", "*.bin.gz"))
	files = append(files, w2...)
	sid := uint16(biome.SulfurCaves{}.EncodeBiome())
	enc := make([]uint16, len(biomeRowDefs))
	for i, d := range biomeRowDefs {
		enc[i] = uint16(d.EncodeBiome())
	}
	for _, f := range files {
		w := loadBDSFile(t, f)
		g := NewWithProfile(w.seed, ProfileBedrock)
		s := newScratch()
		var n, ok, sn, sok int
		for k, c := range w.chunks {
			for qz := range 4 {
				for qx := range 4 {
					q := s.quartColumnAt(g, k[0]*4+qx, k[1]*4+qz)
					for qy := range len(q.ids) - 1 {
						b, uniform := c.biomes[cellIndex(qx*4, minY+qy*4, qz*4)], true
						for i := 1; i < 64 && uniform; i++ {
							uniform = c.biomes[cellIndex(qx*4+i&3, minY+qy*4+i>>4, qz*4+i>>2&3)] == b
						}
						if !uniform {
							continue
						}
						n++
						if enc[q.ids[qy]] == b {
							ok++
						}
						if b == sid {
							sn++
							if enc[q.ids[qy]] == b {
								sok++
							}
						}
					}
				}
			}
		}
		name := strings.TrimSuffix(filepath.Base(f), ".bin.gz")
		t.Logf("%-28s quarts %7d same %.3f%%; sulfur caves %d of %d", name, n, 100*float64(ok)/float64(max(n, 1)), sok, sn)
		if sn > 500 && sok == 0 {
			t.Errorf("%s: no sulfur caves where BDS has %d quarts", name, sn)
		}
	}
}

// TestBedrockBlockBiomesMatchBDS compares the biome BDS stores at every block (T11) with ours, per test
// seed and biome window, and holds it to 99.9%. BDS's real sections reach the chunk's highest block
// once decorated (trees included), so the copy rule is applied here from BDS's own blocks; what
// decoration reads, with the carvers stage's top, is reported beside it.
func TestBedrockBlockBiomesMatchBDS(t *testing.T) {
	dir := goldenBedrockDir(t)
	testBedrockData(t)
	files := []string{}
	for _, s := range bedrockTestSeeds {
		files = append(files, filepath.Join(dir, "whole", "seed"+strconv.FormatInt(s, 10)+".bin.gz"))
	}
	w2, _ := filepath.Glob(filepath.Join(dir, "whole", "windows", "*.bin.gz"))
	files = append(files, w2...)
	enc := make([]uint16, len(biomeRowDefs))
	for i, d := range biomeRowDefs {
		enc[i] = uint16(d.EncodeBiome())
	}
	for _, f := range files {
		w := loadBDSFile(t, f)
		g := NewWithProfile(w.seed, ProfileBedrock)
		s := newScratch()
		var n, ok, baseOK int
		conf := map[[2]uint16]int{}
		for k, c := range w.chunks {
			hi := minY
			for cell := range chunkCells - 256 { // Y 319 holds the study's loading marker.
				if w.names[c.ids[cell]] != "air" {
					hi = max(hi, cell>>8+minY)
				}
			}
			for cell := range chunkCells {
				x, y, z := k[0]*16+cell&15, cell>>8+minY, k[1]*16+cell>>4&15
				n++
				got := enc[s.bedrockBiome(g, x, storedY(y, (hi-minY)>>4), z)]
				if got == c.biomes[cell] {
					ok++
				} else {
					conf[[2]uint16{c.biomes[cell], got}]++
				}
				if enc[s.bedrockStoredBiome(g, x, y, z)] == c.biomes[cell] {
					baseOK++
				}
			}
		}
		name := strings.TrimSuffix(filepath.Base(f), ".bin.gz")
		var worst [2]uint16
		for k, v := range conf {
			if v > conf[worst] {
				worst = k
			}
		}
		t.Logf("%-28s blocks %8d same %.4f%% (sky copies from the carvers stage's top: %.4f%%), most wrong (bds, ours) %v: %d", name, n,
			100*float64(ok)/float64(n), 100*float64(baseOK)/float64(n), worst, conf[worst])
		if float64(ok) < 0.999*float64(n) {
			t.Errorf("%s: %d of %d block biomes differ from BDS", name, n-ok, n)
		}
	}
}
