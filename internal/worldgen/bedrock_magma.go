package worldgen

import (
	"encoding/json"
	"math"
)

// Bedrock's underwater magma (NOTES-leftovers L20), for the Bedrock profile: the rule
// overworld_underwater_magma_feature places surface_relative_threshold_feature, which places
// snap_to_surface_feature (bedrock_plants.go), which places partially_exposed_blob_feature. Glow lichen's
// rule uses the same threshold but a multiface feature whose directions BDS shuffles with its worker
// thread's Random (T18), so it is not reproducible and Java's placement stays (javaReplaced).

// thresholdFeat is SurfaceRelativeThresholdFeature::place (0xd8f2730): the child goes in only where the
// position is more than minimum_distance_below_surface under the surface height of its quart column
// (x >> 2, z >> 2). ASSUMED: that height is the preliminary surface level (the estimate the aquifer
// reads, cached per quart column), as the study takes it; the provider object is not read further.
type thresholdFeat struct {
	f       bfeat
	minimum int
}

func (f *thresholdFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	h := d.g.preliminarySurface(&d.s.prelims, p.x, p.z)
	if p.y >= h-f.minimum {
		return p, false
	}
	return f.f.place(d, r, p)
}

func (fc *featCompiler) threshold(v map[string]json.RawMessage) (bfeat, error) {
	f := &thresholdFeat{}
	json.Unmarshal(v["minimum_distance_below_surface"], &f.minimum)
	var err error
	if f.f, err = fc.child(v["feature_to_place"]); err != nil {
		return nil, err
	}
	return f, nil
}

// blobFeat is PartiallyExposedBlobFeature::place (0x5409b60): around the cell under the position, the
// cells within placement_radius_around_floor in BlockPos.withinManhattan order (0xdd94f80), each
// drawing nextFloat first; at most the probability, the cell and its 5 faces other than exposed_face
// must hold neither water (block or liquid layer; ASSUMED material 5 is water) nor air, and the block
// is set. Success when one block was set.
type blobFeat struct {
	block   bs
	radius  int
	chance  float32
	exposed dir
}

func (f *blobFeat) place(d *decoration, r *bmt, p pos) (pos, bool) {
	c := p.step(dDown)
	ok := false
	solid := func(q pos) bool { s := d.at(q); return !s.isAir() && !s.has(fWater) }
	withinManhattan(f.radius, f.radius, f.radius, func(dx, dy, dz int) {
		if r.nextFloat() > f.chance {
			return
		}
		q := c.add(dx, dy, dz)
		if q.y < minY || q.y > maxY || !solid(q) {
			return
		}
		// The faces in the code's order: down, up, north, south, west, east, less the exposed one.
		for _, a := range allDirs {
			if a != f.exposed && !solid(q.step(a)) {
				return
			}
		}
		if d.bput(q, f.block) {
			ok = true
		}
	})
	return p, ok
}

// withinManhattan calls fn for every offset within the reaches, by Manhattan depth from 0: x from -min(rx,
// depth) up, y inner, z = +k then its mirror -k (the order of Java's BlockPos.withinManhattan, which
// 0xdd94f80 follows).
func withinManhattan(rx, ry, rz int, fn func(x, y, z int)) {
	for depth := 0; depth <= rx+ry+rz; depth++ {
		mx := min(rx, depth)
		for x := -mx; x <= mx; x++ {
			my := min(ry, depth-manhattanAbs(x))
			for y := -my; y <= my; y++ {
				z := depth - manhattanAbs(x) - manhattanAbs(y)
				if z > rz {
					continue
				}
				fn(x, y, z)
				if z != 0 {
					fn(x, y, -z)
				}
			}
		}
	}
}

func (fc *featCompiler) blob(v map[string]json.RawMessage) (bfeat, error) {
	b, err := parseBDesc(v["places_block"])
	if err != nil {
		return nil, err
	}
	s, err := javaStateFor(b)
	if err != nil {
		return nil, unimpl("%v", err)
	}
	f := &blobFeat{block: s}
	json.Unmarshal(v["placement_radius_around_floor"], &f.radius)
	json.Unmarshal(v["placement_probability_per_valid_position"], &f.chance)
	if f.radius < 0 || f.radius > 8 || math.IsNaN(float64(f.chance)) {
		return nil, unimpl("blob radius %d", f.radius)
	}
	var face string
	json.Unmarshal(v["exposed_face"], &face)
	i, ok := map[string]dir{"down": dDown, "up": dUp, "north": dNorth, "south": dSouth, "west": dWest, "east": dEast}[face]
	if !ok {
		return nil, unimpl("blob exposed_face %s", face)
	}
	f.exposed = i
	return f, nil
}

func manhattanAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
