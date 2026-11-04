package worldgen

import (
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// The golden tests compare our layers with fixtures taken from the Java 26.1 generator, by raw bits
// where they are doubles. The fixtures are not in this repo: set OVERMIND_GOLDEN to their folder.

var goldenSeeds = []int64{100, 1, 12345, 987654321}

func goldenDir(t testing.TB) string {
	dir := os.Getenv("OVERMIND_GOLDEN")
	if dir == "" {
		t.Skip("OVERMIND_GOLDEN is not set: point it at the golden fixtures folder to run the exact-match tests")
	}
	if rows, err := filepath.Glob(filepath.Join(dir, "seed_*")); err != nil || len(rows) == 0 {
		t.Fatalf("OVERMIND_GOLDEN=%s holds no seed folders", dir)
	}
	return dir
}

// goldenRows reads a CSV into maps by column name.
func goldenRows(t testing.TB, parts ...string) []map[string]string {
	f, err := os.Open(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	all, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("%s holds no rows: the fixtures are empty", filepath.Join(parts...))
	}
	rows := make([]map[string]string, 0, len(all)-1)
	for _, line := range all[1:] {
		m := make(map[string]string, len(line))
		for i, name := range all[0] {
			if i < len(line) {
				m[name] = line[i]
			}
		}
		rows = append(rows, m)
	}
	return rows
}

func seedDir(dir string, seed int64) string { return filepath.Join(dir, fmt.Sprintf("seed_%d", seed)) }

func gi(t testing.TB, m map[string]string, name string) int {
	v, err := strconv.Atoi(m[name])
	if err != nil {
		f, err2 := strconv.ParseFloat(m[name], 64)
		if err2 != nil {
			t.Fatalf("%s: %q", name, m[name])
		}
		return int(f)
	}
	return v
}

func gf(t testing.TB, m map[string]string, name string) float64 {
	v, err := strconv.ParseFloat(m[name], 64)
	if err != nil {
		t.Fatalf("%s: %q", name, m[name])
	}
	return v
}

// gbits reads a hex double, which is "0" for zero.
func gbits(t testing.TB, m map[string]string, name string) uint64 {
	v, err := strconv.ParseUint(m[name+"_bits"], 16, 64)
	if err != nil {
		t.Fatalf("%s_bits: %q", name, m[name+"_bits"])
	}
	return v
}

// goldenCount tallies a comparison and reports the first few misses.
type goldenCount struct {
	t           *testing.T
	name        string
	total, miss int
}

func (c *goldenCount) same(ok bool, format string, args ...any) {
	c.total++
	if !ok {
		c.miss++
		if c.miss <= 8 {
			c.t.Errorf("%s: "+format, append([]any{c.name}, args...)...)
		}
	}
}

func (c *goldenCount) bits(got float64, want uint64, label string) {
	c.same(math.Float64bits(got) == want, "%s = %v (%016x), want %v (%016x)", label, got, math.Float64bits(got), math.Float64frombits(want), want)
}

func (c *goldenCount) report() { c.t.Logf("%s: %d of %d exact", c.name, c.total-c.miss, c.total) }

func TestGoldenRNG(t *testing.T) {
	dir := goldenDir(t)
	for _, seed := range goldenSeeds {
		c := goldenCount{t: t, name: fmt.Sprintf("rng %d", seed)}
		root := xoroFromSeed(seed)
		first := root
		w := root.forkFactory()
		h := w.named("minecraft:temperature")
		for _, row := range goldenRows(t, seedDir(dir, seed), "rng.csv") {
			what, value := row["what"], row["value"]
			switch {
			case strings.HasPrefix(what, "root_nextLong"):
				c.same(strconv.FormatInt(int64(first.next()), 10) == value, "%s want %s", what, value)
			case what == "root_factory":
				c.same(fmt.Sprintf("seedLo: %d; seedHi: %d", int64(w.lo), int64(w.hi)) == value, "%s %s", what, value)
			case strings.HasPrefix(what, "hash_temperature_nextDouble"):
				want, _ := strconv.ParseFloat(value, 64)
				c.same(h.nextDouble() == want, "%s want %s", what, value)
			case strings.HasPrefix(what, "hash_temperature_nextInt256"):
				c.same(strconv.Itoa(h.nextInt(256)) == value, "%s want %s", what, value)
			case what == "biome_zoom_seed":
				c.same(strconv.FormatInt(biomeZoomSeed(seed), 10) == value, "%s want %s", what, value)
			default:
				t.Errorf("unknown row %s", what)
			}
		}
		c.report()
	}
}

func TestGoldenNoiseParams(t *testing.T) {
	dir := goldenDir(t)
	rows := goldenRows(t, dir, "noise_params.csv")
	if len(rows) != len(noiseSpecs) {
		t.Errorf("%d noises in the list, %d in our table", len(rows), len(noiseSpecs))
	}
	for _, row := range rows {
		spec, ok := noiseSpecs[row["noise"]]
		if !ok {
			t.Errorf("%s missing", row["noise"])
			continue
		}
		var amps []string
		for _, a := range spec.amps {
			amps = append(amps, strconv.FormatFloat(a, 'f', -1, 64))
		}
		var want []string
		for _, a := range strings.Fields(row["amplitudes"]) {
			f, _ := strconv.ParseFloat(a, 64)
			want = append(want, strconv.FormatFloat(f, 'f', -1, 64))
		}
		if strconv.Itoa(spec.first) != row["first_octave"] || strings.Join(amps, " ") != strings.Join(want, " ") {
			t.Errorf("%s: %d %v, list %s %v", row["noise"], spec.first, amps, row["first_octave"], want)
		}
	}
}

func TestGoldenNoise(t *testing.T) {
	dir := goldenDir(t)
	for _, seed := range goldenSeeds {
		c := goldenCount{t: t, name: fmt.Sprintf("noise %d", seed)}
		root := xoroFromSeed(seed)
		w := root.forkFactory()
		built := map[string]*normalNoise{}
		for _, row := range goldenRows(t, seedDir(dir, seed), "noise.csv") {
			name := row["noise"]
			n := built[name]
			if n == nil {
				n = newNamedNoise(w, name)
				built[name] = n
			}
			x, y, z := gf(t, row, "x"), gf(t, row, "y"), gf(t, row, "z")
			c.bits(n.at(x, y, z), gbits(t, row, "value"), fmt.Sprintf("%s(%v,%v,%v)", name, x, y, z))
		}
		c.report()
	}
}

func TestGoldenClimate(t *testing.T) {
	dir := goldenDir(t)
	for _, seed := range goldenSeeds {
		g := New(seed)
		c := goldenCount{t: t, name: fmt.Sprintf("climate %d", seed)}
		for _, row := range goldenRows(t, seedDir(dir, seed), "climate.csv") {
			bx, by, bz := gi(t, row, "quart_x")*4, gi(t, row, "quart_y")*4, gi(t, row, "quart_z")*4
			cl := g.Climate(bx, bz)
			off := g.surfaceShape(bx, bz).offset
			depth := depthAt(by, off)
			c.bits(cl.Temperature, gbits(t, row, "temperature"), "temperature")
			c.bits(cl.Humidity, gbits(t, row, "humidity"), "humidity")
			c.bits(cl.Continentalness, gbits(t, row, "continentalness"), "continentalness")
			c.bits(cl.Erosion, gbits(t, row, "erosion"), "erosion")
			c.bits(depth, gbits(t, row, "depth"), "depth")
			c.bits(cl.Weirdness, gbits(t, row, "weirdness"), "weirdness")
			for name, v := range map[string]float64{"q_t": cl.Temperature, "q_h": cl.Humidity, "q_c": cl.Continentalness, "q_e": cl.Erosion, "q_d": depth, "q_w": cl.Weirdness} {
				c.same(quantize(v) == int64(gi(t, row, name)), "%s %d, want %s", name, quantize(v), row[name])
			}
		}
		c.report()
	}
}

func TestGoldenBiomeTable(t *testing.T) {
	dir := goldenDir(t)
	rows := goldenRows(t, dir, "biome_params.csv")
	if len(rows) != len(biomeRows) {
		t.Fatalf("%d rows in the list, %d in our table", len(rows), len(biomeRows))
	}
	for i, row := range rows {
		ours := biomeRows[i]
		if biomeTableNames[ours[0]] != row["biome"] {
			t.Fatalf("row %d: %s, list %s", i, biomeTableNames[ours[0]], row["biome"])
		}
		if _, ok := biomeDefs[row["biome"]]; !ok {
			t.Fatalf("no def for %s", row["biome"])
		}
		for d, name := range []string{"t", "h", "c", "e", "d", "w"} {
			iv := biomeIntervals[ours[d+1]]
			if int(iv[0]) != gi(t, row, name+"_min") || int(iv[1]) != gi(t, row, name+"_max") {
				t.Fatalf("row %d %s: %v against %s %s", i, name, iv, row[name+"_min"], row[name+"_max"])
			}
		}
	}
}

func TestGoldenBiomes(t *testing.T) {
	dir := goldenDir(t)
	tree := biomeTree()
	for _, seed := range goldenSeeds {
		g := New(seed)
		cold := goldenCount{t: t, name: fmt.Sprintf("biome tree cold %d", seed)}
		warm := goldenCount{t: t, name: fmt.Sprintf("biome tree warm %d", seed)}
		brute := goldenCount{t: t, name: fmt.Sprintf("biome brute force %d", seed)}
		zoom := goldenCount{t: t, name: fmt.Sprintf("biome block zoom %d", seed)}
		target := goldenCount{t: t, name: fmt.Sprintf("biome target %d", seed)}
		var last *biomeNode
		warmSearch := func(tg *biomeTarget) *biomeNode {
			d := int64(math.MaxInt64)
			if last != nil {
				d = last.dist(tg, math.MaxInt64)
			}
			last, _ = tree.search(tg, last, d)
			return last
		}
		targetAt := func(x, y, z int) biomeTarget {
			col := g.biomeColumnAt(x*4, z*4)
			tg := col.target
			tg[biomeTargetDepth] = quantize(depthAt(y*4, col.offset))
			return tg
		}
		for _, row := range goldenRows(t, seedDir(dir, seed), "biome.csv") {
			qx, qy, qz := gi(t, row, "quart_x"), gi(t, row, "quart_y"), gi(t, row, "quart_z")
			tg := targetAt(qx, qy, qz)
			for d, name := range []string{"q_t", "q_h", "q_c", "q_e", "q_d", "q_w"} {
				target.same(tg[d] == int64(gi(t, row, name)), "%s %d, want %s", name, tg[d], row[name])
			}
			name := func(n *biomeNode) string { return biomeTableNames[biomeRows[n.row][0]] }
			cn := nearestBiome(tree, &tg)
			var someLeaf *biomeNode
			forEachLeaf(tree, func(n *biomeNode) {
				if n.row == int32(qx&4095) {
					someLeaf = n
				}
			})
			if from := nearestBiomeFrom(tree, &tg, someLeaf); from != cn {
				t.Errorf("(%d,%d,%d): search from a known leaf found row %d, cold search row %d", qx, qy, qz, from.row, cn.row)
			}
			cold.same(name(cn) == row["biome_quart_rtree_cold"], "(%d,%d,%d) %s, want %s", qx, qy, qz, name(cn), row["biome_quart_rtree_cold"])
			wn := warmSearch(&tg)
			warm.same(name(wn) == row["biome_quart_rtree_warm"], "(%d,%d,%d) %s, want %s", qx, qy, qz, name(wn), row["biome_quart_rtree_warm"])
			var best *biomeNode
			bestD := int64(math.MaxInt64)
			forEachLeaf(tree, func(n *biomeNode) {
				d := n.dist(&tg, math.MaxInt64)
				if d < bestD || (d == bestD && n.row < best.row) {
					best, bestD = n, d
				}
			})
			brute.same(name(best) == row["biome_quart_bruteforce"], "(%d,%d,%d) %s, want %s", qx, qy, qz, name(best), row["biome_quart_bruteforce"])
			zx, zy, zz := g.zoomQuart(gi(t, row, "block_x"), gi(t, row, "block_y"), gi(t, row, "block_z"))
			zt := targetAt(zx, zy, zz)
			zn := warmSearch(&zt)
			zoom.same(name(zn) == row["biome_block_zoom"], "block %s,%s,%s %s, want %s", row["block_x"], row["block_y"], row["block_z"], name(zn), row["biome_block_zoom"])
		}
		for _, c := range []*goldenCount{&target, &cold, &warm, &brute, &zoom} {
			c.report()
		}
	}
}

func forEachLeaf(n *biomeNode, f func(*biomeNode)) {
	if n.kids == nil {
		f(n)
		return
	}
	for _, k := range n.kids {
		forEachLeaf(k, f)
	}
}

func TestGoldenDensityParts(t *testing.T) {
	dir := goldenDir(t)
	for _, seed := range goldenSeeds {
		g := New(seed)
		c := goldenCount{t: t, name: fmt.Sprintf("density parts %d", seed)}
		for _, row := range goldenRows(t, seedDir(dir, seed), "density_parts.csv") {
			x, y, z := gi(t, row, "x"), gi(t, row, "y"), gi(t, row, "z")
			bx, bz := float64(x), float64(z)
			qx, qz := g.warped(x, z)
			cc, e, w := g.shapeClimate(qx, qz)
			in := coordsOf(cc, e, w)
			shape := g.shapeAt(x, z)
			rough := g.roughnessAt(x, y, z)
			check := func(name string, v float64) {
				c.bits(v, gbits(t, row, name), fmt.Sprintf("%s at %d,%d,%d", name, x, y, z))
			}
			check("shift_x", g.climate.shiftX(bx, bz))
			check("shift_z", g.climate.shiftZ(bx, bz))
			check("continents", cc)
			check("erosion", e)
			check("ridges", w)
			check("ridges_folded", peaksAndValleys(w))
			check("offset", shape.offset)
			check("factor", shape.factor)
			check("jaggedness", float64(jaggedSpline.eval(&in)))
			check("depth", depthAt(y, shape.offset))
			check("sloped_cheese", g.slopedAt(&shape, x, y, z))
			check("base_3d_noise", g.caves.base3D.at(x, y, z))
			check("entrances", g.entrancesAt(x, y, z, rough))
			check("pillars", g.pillarsAt(x, y, z))
			check("spaghetti_2d", g.spaghetti2DAt(x, y, z))
			check("spaghetti_2d_thickness_modulator", g.thickness2D(x, y, z))
			check("spaghetti_roughness_function", rough)
			toggle := g.noodleToggleAt(x, y, z)
			thick, a, b := g.noodleParts(x, y, z)
			noodle := noodleValue(toggle, thick, a, b)
			check("noodle", noodle)
			check("final_direct", min(squeeze(float64(g.coreAt(&shape, x, y, z)*0.64)), noodle))
			// This column is direct, at the exact block; the snapped one is in column.csv.
			c.same(preliminaryLevel(g.surfaceShape(x, z)) == gi(t, row, "preliminary_surface_level"), "prelim at %d,%d, want %s", x, z, row["preliminary_surface_level"])
		}
		c.report()
	}
}

func TestGoldenDensityAndMaterial(t *testing.T) {
	dir := goldenDir(t)
	for _, seed := range goldenSeeds {
		g := New(seed)
		direct := goldenCount{t: t, name: fmt.Sprintf("final direct %d", seed)}
		inter := goldenCount{t: t, name: fmt.Sprintf("final interpolated %d", seed)}
		mat := goldenCount{t: t, name: fmt.Sprintf("material %d", seed)}
		s := new(chunkScratch)
		for _, row := range goldenRows(t, seedDir(dir, seed), "density.csv") {
			x, y, z := gi(t, row, "x"), gi(t, row, "y"), gi(t, row, "z")
			shape := g.shapeAt(x, z)
			thick, a, b := g.noodleParts(x, y, z)
			d := min(squeeze(float64(g.coreAt(&shape, x, y, z)*0.64)), noodleValue(g.noodleToggleAt(x, y, z), thick, a, b))
			direct.bits(d, gbits(t, row, "final_direct"), fmt.Sprintf("at %d,%d,%d", x, y, z))

			s.startColumnOnly(g)
			w := g.startWalk(s, x, z)
			layer := floorDiv(y+64, 8)
			dy := y - (minY + layer*cellY)
			blend := w.layerBlendAt(layer)
			f := blend.final(float64(dy) / 8.0)
			inter.bits(f, gbits(t, row, "final_interpolated"), fmt.Sprintf("at %d,%d,%d", x, y, z))
			k := w.rawKind(&blend, y, dy)
			name := map[fillKind]string{fillAir: "air", fillWater: "water", fillLava: "lava", fillSolid: "solid"}[k]
			mat.same(name == row["material_chunk"], "at %d,%d,%d: %s, want %s", x, y, z, name, row["material_chunk"])
		}
		for _, c := range []*goldenCount{&direct, &inter, &mat} {
			c.report()
		}
	}
}

func TestGoldenColumns(t *testing.T) {
	dir := goldenDir(t)
	for _, seed := range goldenSeeds {
		g := New(seed)
		c := goldenCount{t: t, name: fmt.Sprintf("columns %d", seed)}
		s := new(chunkScratch)
		for _, row := range goldenRows(t, seedDir(dir, seed), "column.csv") {
			x, z := gi(t, row, "x"), gi(t, row, "z")
			s.startColumnOnly(g)
			solid, nonAir := g.startWalk(s, x, z).rawTops()
			c.same(solid == gi(t, row, "top_solid_y"), "(%d,%d) top solid %d, want %s", x, z, solid, row["top_solid_y"])
			c.same(nonAir == gi(t, row, "top_nonair_y"), "(%d,%d) top non-air %d, want %s", x, z, nonAir, row["top_nonair_y"])
			c.same(g.preliminarySurface(&s.prelims, x, z) == gi(t, row, "preliminary_surface_level_quart"), "(%d,%d) prelim", x, z)
		}
		c.report()
	}
}

// TestGoldenFillNoiseMaterial compares the noise pass itself, block by block, with the study's
// material_chunk column: solid, air, water or lava.
func TestGoldenFillNoiseMaterial(t *testing.T) {
	dir := goldenDir(t)
	for _, seed := range goldenSeeds {
		g := NewWithProfile(seed, ProfileOverjava)
		c := goldenCount{t: t, name: fmt.Sprintf("fillNoise material %d", seed)}
		s := new(chunkScratch)
		for i, row := range goldenRows(t, seedDir(dir, seed), "density.csv") {
			if i%8 != 0 {
				continue
			}
			x, y, z := gi(t, row, "x"), gi(t, row, "y"), gi(t, row, "z")
			g.buildBase(&s.base, s, x>>4, z>>4, stageNoise, x&15, x&15, z&15, z&15)
			st := s.base.b[cellIndex(x&15, y, z&15)]
			name := "solid"
			switch st {
			case sAir:
				name = "air"
			case sWater:
				name = "water"
			case sLava:
				name = "lava"
			}
			c.same(name == row["material_chunk"], "at %d,%d,%d: %s, want %s", x, y, z, name, row["material_chunk"])
		}
		if c.total == 0 {
			t.Fatalf("seed %d: no rows compared", seed)
		}
		c.report()
	}
}
