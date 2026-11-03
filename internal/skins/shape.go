package skins

import (
	"math"

	"github.com/df-mc/dragonfly/server/player/skin"
)

// Odd shapes (the skins.blockOddShapes setting): a skin whose geometry goes
// beyond the standard player model. The bounds are the standard model's own
// (gophertunnel 1.63.0's skin_geometry.json, stdWide in geometry.go): every
// cube, its inflate included, lies in x -8.25 to 8.25, y -0.25 to 32.5 and z
// -4.5 to 4.5 model pixels (the sleeves, the trousers' feet and the hat). A
// skin is odd when, with each bone and cube turned by its rotation about its
// pivot (its parents' too):
//
//   - bigger: anything drawn reaches more than oddMargin pixels past those
//     bounds on any side;
//   - much smaller: all it draws fits in less than half the standard height
//     (16 pixels) or half the standard width (8.25 pixels);
//   - hidden parts: less than half of a standard base part (head, body, either
//     arm, either leg) is covered by anything drawn;
//   - floating parts: something drawn does not touch, within oddTouch pixels,
//     the rest of the model.
//
// A geometry that cannot be read or found is odd too: it cannot be checked.
// The standard models themselves, named without data, are never odd.

const (
	oddMargin = 1.0 // pixels past the standard bounds that still count as standard
	oddTouch  = 0.5 // pixels between two parts that still count as touching
)

// standardBounds is the box every part of the standard model is in.
var standardBounds = box{min: [3]float64{-8.25, -0.25, -4.5}, max: [3]float64{8.25, 32.5, 4.5}}

type box struct{ min, max [3]float64 }

func (b box) grow(d float64) box {
	for i := range 3 {
		b.min[i] -= d
		b.max[i] += d
	}
	return b
}

func (b box) inside(o box) bool {
	for i := range 3 {
		if b.min[i] < o.min[i] || b.max[i] > o.max[i] {
			return false
		}
	}
	return true
}

func (b box) touches(o box, d float64) bool {
	for i := range 3 {
		if b.min[i] > o.max[i]+d || o.min[i] > b.max[i]+d {
			return false
		}
	}
	return true
}

func (b box) contains(p [3]float64) bool {
	for i := range 3 {
		if p[i] < b.min[i] || p[i] > b.max[i] {
			return false
		}
	}
	return true
}

func union(bs []box) box {
	u := bs[0]
	for _, b := range bs[1:] {
		for i := range 3 {
			u.min[i] = math.Min(u.min[i], b.min[i])
			u.max[i] = math.Max(u.max[i], b.max[i])
		}
	}
	return u
}

// OddShape reports whether a skin's geometry goes beyond the standard player
// model, and why.
func OddShape(s skin.Skin) (odd bool, why string) {
	name := s.ModelConfig.Default
	all := parseGeometry(s.Model)
	if _, ok := all[name]; !ok {
		if std, _ := builtIn(name); std {
			return false, ""
		}
	}
	g, ok := resolve(all, name)
	if !ok {
		return true, "geometry not found"
	}
	return oddGeometry(g)
}

func oddGeometry(g *geometry) (bool, string) {
	boxes := drawn(g)
	if len(boxes) == 0 {
		return true, "draws nothing"
	}
	u := union(boxes)
	if !u.inside(standardBounds.grow(oddMargin)) {
		return true, "bigger than the player model"
	}
	if u.max[1]-u.min[1] < (standardBounds.max[1]-standardBounds.min[1])/2 || u.max[0]-u.min[0] < (standardBounds.max[0]-standardBounds.min[0])/2 {
		return true, "much smaller than the player model"
	}
	for _, c := range stdWide {
		if c.overlay {
			continue
		}
		part := box{min: c.origin, max: [3]float64{c.origin[0] + c.size[0], c.origin[1] + c.size[1], c.origin[2] + c.size[2]}}
		if covered(part, boxes) < 0.5 {
			return true, c.bone + " hidden"
		}
	}
	if !connected(boxes) {
		return true, "floating parts"
	}
	return false, ""
}

// covered is the share of a part, sampled every half pixel, inside any box.
func covered(part box, boxes []box) float64 {
	in, all := 0, 0
	for x := part.min[0] + 0.25; x < part.max[0]; x += 0.5 {
		for y := part.min[1] + 0.25; y < part.max[1]; y += 0.5 {
			for z := part.min[2] + 0.25; z < part.max[2]; z += 0.5 {
				all++
				for _, b := range boxes {
					if b.contains([3]float64{x, y, z}) {
						in++
						break
					}
				}
			}
		}
	}
	if all == 0 {
		return 1
	}
	return float64(in) / float64(all)
}

// connected reports whether every box is joined to every other through boxes
// that touch.
func connected(boxes []box) bool {
	seen := make([]bool, len(boxes))
	stack := []int{0}
	seen[0] = true
	n := 1
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for j := range boxes {
			if !seen[j] && boxes[i].touches(boxes[j], oddTouch) {
				seen[j] = true
				n++
				stack = append(stack, j)
			}
		}
	}
	return n == len(boxes)
}

// drawn is the box around each cube and poly mesh the geometry draws, in model
// space: each cube's corners turned by its own rotation, then by its bone's
// and each parent's, each about its pivot.
func drawn(g *geometry) []box {
	byName := map[string]*bone{}
	for _, b := range g.bones {
		byName[b.Name] = b
	}
	// chain is the transforms from a bone up to the root.
	var chain func(b *bone, depth int) []rot
	chain = func(b *bone, depth int) []rot {
		if b == nil || depth > 64 {
			return nil
		}
		out := []rot{newRot(b.Rotation, b.Pivot)}
		if p, ok := byName[b.Parent]; ok && p != b {
			out = append(out, chain(p, depth+1)...)
		}
		return out
	}
	var out []box
	for _, b := range g.bones {
		if !b.visible() {
			continue
		}
		tr := chain(b, 0)
		apply := func(p [3]float64) [3]float64 {
			for _, r := range tr {
				p = r.apply(p)
			}
			return p
		}
		for _, c := range b.Cubes {
			o, sz := vec3(c.Origin), vec3(c.Size)
			lo := [3]float64{o[0] - c.Inflate, o[1] - c.Inflate, o[2] - c.Inflate}
			hi := [3]float64{o[0] + sz[0] + c.Inflate, o[1] + sz[1] + c.Inflate, o[2] + sz[2] + c.Inflate}
			own := newRot(c.Rotation, c.Pivot)
			var pts [][3]float64
			for i := range 8 {
				p := lo
				if i&1 != 0 {
					p[0] = hi[0]
				}
				if i&2 != 0 {
					p[1] = hi[1]
				}
				if i&4 != 0 {
					p[2] = hi[2]
				}
				pts = append(pts, apply(own.apply(p)))
			}
			out = append(out, around(pts))
		}
		if b.PolyMesh != nil && len(b.PolyMesh.Positions) > 0 {
			var pts [][3]float64
			for _, p := range b.PolyMesh.Positions {
				pts = append(pts, apply(vec3(p)))
			}
			out = append(out, around(pts))
		}
	}
	return out
}

func around(pts [][3]float64) box {
	b := box{min: pts[0], max: pts[0]}
	for _, p := range pts[1:] {
		for i := range 3 {
			b.min[i] = math.Min(b.min[i], p[i])
			b.max[i] = math.Max(b.max[i], p[i])
		}
	}
	return b
}

func vec3(v []float64) (out [3]float64) {
	copy(out[:], v)
	return out
}

// rot turns a point about a pivot by Euler angles in degrees, X then Y then Z.
type rot struct {
	m     [3][3]float64
	pivot [3]float64
	none  bool
}

func newRot(deg, pivot []float64) rot {
	if !rotated(deg) {
		return rot{none: true}
	}
	a := vec3(deg)
	rx, ry, rz := a[0]*math.Pi/180, a[1]*math.Pi/180, a[2]*math.Pi/180
	x := [3][3]float64{{1, 0, 0}, {0, math.Cos(rx), -math.Sin(rx)}, {0, math.Sin(rx), math.Cos(rx)}}
	y := [3][3]float64{{math.Cos(ry), 0, math.Sin(ry)}, {0, 1, 0}, {-math.Sin(ry), 0, math.Cos(ry)}}
	z := [3][3]float64{{math.Cos(rz), -math.Sin(rz), 0}, {math.Sin(rz), math.Cos(rz), 0}, {0, 0, 1}}
	return rot{m: mul(z, mul(y, x)), pivot: vec3(pivot)}
}

func mul(a, b [3][3]float64) (out [3][3]float64) {
	for i := range 3 {
		for j := range 3 {
			for k := range 3 {
				out[i][j] += a[i][k] * b[k][j]
			}
		}
	}
	return out
}

func (r rot) apply(p [3]float64) [3]float64 {
	if r.none {
		return p
	}
	d := [3]float64{p[0] - r.pivot[0], p[1] - r.pivot[1], p[2] - r.pivot[2]}
	var out [3]float64
	for i := range 3 {
		out[i] = r.m[i][0]*d[0] + r.m[i][1]*d[1] + r.m[i][2]*d[2] + r.pivot[i]
	}
	return out
}
