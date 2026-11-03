package skins

import (
	"encoding/json"
	"math"
	"strings"
)

// Bedrock skin geometry, as a client sends it in SkinGeometryData: JSON in the
// format of 1.12 and later ({"format_version", "minecraft:geometry": [{
// "description": {"identifier", "texture_width", "texture_height"}, "bones":
// [...]}]}) or the older 1.8 one ({"format_version", "geometry.name": {
// "texturewidth", "textureheight", "bones": [...]}}, where a key
// "geometry.child:geometry.parent" inherits the parent's bones). A bone has a
// name, a parent, a pivot, a rotation in degrees and cubes (origin, size, uv,
// inflate, and a rotation of their own about a pivot) or a poly_mesh. The
// ModelConfig's default names the geometry the player is drawn with.

// geometry is one geometry, its bones by name.
type geometry struct {
	name           string
	texW, texH     int
	bones          []*bone
	parent         string // the old format's inherited geometry, "" if none
	hasDescription bool
}

type bone struct {
	Name        string    `json:"name"`
	Parent      string    `json:"parent"`
	Pivot       []float64 `json:"pivot"`
	Rotation    []float64 `json:"rotation"`
	Cubes       []cube    `json:"cubes"`
	NeverRender bool      `json:"neverRender"`
	PolyMesh    *struct {
		Positions [][]float64 `json:"positions"`
	} `json:"poly_mesh"`
}

type cube struct {
	Origin   []float64       `json:"origin"`
	Size     []float64       `json:"size"`
	UV       json.RawMessage `json:"uv"`
	Inflate  float64         `json:"inflate"`
	Rotation []float64       `json:"rotation"`
	Pivot    []float64       `json:"pivot"`
}

// visible reports whether a bone draws anything.
func (b *bone) visible() bool {
	return !b.NeverRender && (len(b.Cubes) > 0 || b.PolyMesh != nil && len(b.PolyMesh.Positions) > 0)
}

// parseGeometry reads every geometry in a SkinGeometryData, by identifier. A
// geometry of the old format keeps its parent's name to inherit from.
func parseGeometry(data []byte) map[string]*geometry {
	out := map[string]*geometry{}
	var top map[string]json.RawMessage
	if len(data) == 0 || json.Unmarshal(data, &top) != nil {
		return out
	}
	if raw, ok := top["minecraft:geometry"]; ok {
		var list []json.RawMessage
		_ = json.Unmarshal(raw, &list)
		for _, one := range list {
			var g struct {
				Description struct {
					Identifier string  `json:"identifier"`
					TexW       float64 `json:"texture_width"`
					TexH       float64 `json:"texture_height"`
				} `json:"description"`
				Bones []*bone `json:"bones"`
			}
			if json.Unmarshal(one, &g) != nil {
				continue // a geometry that cannot be read is not there
			}
			out[g.Description.Identifier] = &geometry{name: g.Description.Identifier, texW: int(g.Description.TexW), texH: int(g.Description.TexH), bones: g.Bones, hasDescription: true}
		}
	}
	for key, raw := range top {
		if !strings.HasPrefix(key, "geometry.") {
			continue
		}
		var g struct {
			TexW  float64 `json:"texturewidth"`
			TexH  float64 `json:"textureheight"`
			Bones []*bone `json:"bones"`
		}
		if json.Unmarshal(raw, &g) != nil {
			continue
		}
		name, parent, _ := strings.Cut(key, ":")
		out[name] = &geometry{name: name, texW: int(g.TexW), texH: int(g.TexH), bones: g.Bones, parent: parent}
	}
	return out
}

// resolve gives the bones of the geometry called name, its inherited bones
// first (a child's bone replaces the parent's of the same name), and whether
// the geometry could be found at all: in the data, or built in (the standard
// humanoid models need no data).
func resolve(all map[string]*geometry, name string) (g *geometry, ok bool) {
	return resolveDepth(all, name, 0)
}

func resolveDepth(all map[string]*geometry, name string, depth int) (*geometry, bool) {
	if depth > 8 {
		return nil, false
	}
	g, ok := all[name]
	if !ok {
		if std, slim := builtIn(name); std {
			return standardGeometry(slim), true
		}
		return nil, false
	}
	if g.parent == "" {
		return g, true
	}
	p, ok := resolveDepth(all, g.parent, depth+1)
	if !ok {
		return nil, false
	}
	merged := &geometry{name: g.name, texW: g.texW, texH: g.texH}
	if merged.texW == 0 {
		merged.texW, merged.texH = p.texW, p.texH
	}
	mine := map[string]bool{}
	for _, b := range g.bones {
		mine[b.Name] = true
	}
	for _, b := range p.bones {
		if !mine[b.Name] {
			merged.bones = append(merged.bones, b)
		}
	}
	merged.bones = append(merged.bones, g.bones...)
	return merged, true
}

// builtIn reports whether a geometry name is one of Bedrock's built-in player
// models, which a client may name without sending, and whether it is slim.
func builtIn(name string) (std, slim bool) {
	switch name {
	case "geometry.humanoid.custom", "geometry.humanoid", "geometry.humanoid.custom:geometry.humanoid":
		return true, false
	case "geometry.humanoid.customSlim", "geometry.humanoid.customSlim:geometry.humanoid":
		return true, true
	}
	return false, false
}

// The standard player model: gophertunnel 1.63.0's skin_geometry.json
// (geometry.humanoid.custom and geometry.humanoid.customSlim, the models a
// Bedrock client sends with a classic skin), its textured bones only. The
// locator-only bones (root, waist, cape, leftItem, rightItem) draw nothing.
// Origins and sizes are in model pixels; uv is the box UV's corner on the
// 64x64 texture.
type stdCube struct {
	bone, parent string
	origin, size [3]float64
	uv           [2]float64
	inflate      float64
	pivot        [3]float64
	overlay      bool // a second-layer bone
}

var stdWide = []stdCube{
	{bone: "body", parent: "waist", origin: [3]float64{-4, 12, -2}, size: [3]float64{8, 12, 4}, uv: [2]float64{16, 16}, pivot: [3]float64{0, 24, 0}},
	{bone: "jacket", parent: "body", origin: [3]float64{-4, 12, -2}, size: [3]float64{8, 12, 4}, uv: [2]float64{16, 32}, inflate: 0.25, pivot: [3]float64{0, 24, 0}, overlay: true},
	{bone: "head", parent: "body", origin: [3]float64{-4, 24, -4}, size: [3]float64{8, 8, 8}, uv: [2]float64{0, 0}, pivot: [3]float64{0, 24, 0}},
	{bone: "hat", parent: "head", origin: [3]float64{-4, 24, -4}, size: [3]float64{8, 8, 8}, uv: [2]float64{32, 0}, inflate: 0.5, pivot: [3]float64{0, 24, 0}, overlay: true},
	{bone: "leftArm", parent: "body", origin: [3]float64{4, 12, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{32, 48}, pivot: [3]float64{5, 22, 0}},
	{bone: "leftSleeve", parent: "leftArm", origin: [3]float64{4, 12, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{48, 48}, inflate: 0.25, pivot: [3]float64{5, 22, 0}, overlay: true},
	{bone: "rightArm", parent: "body", origin: [3]float64{-8, 12, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{40, 16}, pivot: [3]float64{-5, 22, 0}},
	{bone: "rightSleeve", parent: "rightArm", origin: [3]float64{-8, 12, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{40, 32}, inflate: 0.25, pivot: [3]float64{-5, 22, 0}, overlay: true},
	{bone: "leftLeg", parent: "root", origin: [3]float64{-0.1, 0, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{16, 48}, pivot: [3]float64{1.9, 12, 0}},
	{bone: "leftPants", parent: "leftLeg", origin: [3]float64{-0.1, 0, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{0, 48}, inflate: 0.25, pivot: [3]float64{1.9, 12, 0}, overlay: true},
	{bone: "rightLeg", parent: "root", origin: [3]float64{-3.9, 0, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{0, 16}, pivot: [3]float64{-1.9, 12, 0}},
	{bone: "rightPants", parent: "rightLeg", origin: [3]float64{-3.9, 0, -2}, size: [3]float64{4, 12, 4}, uv: [2]float64{0, 32}, inflate: 0.25, pivot: [3]float64{-1.9, 12, 0}, overlay: true},
}

// stdSlim is stdWide with the slim model's 3-pixel arms, half a pixel lower.
var stdSlim = func() []stdCube {
	out := append([]stdCube(nil), stdWide...)
	for i, c := range out {
		switch c.bone {
		case "leftArm", "leftSleeve":
			c.origin, c.size, c.pivot = [3]float64{4, 11.5, -2}, [3]float64{3, 12, 4}, [3]float64{5, 21.5, 0}
		case "rightArm", "rightSleeve":
			c.origin, c.size, c.pivot = [3]float64{-7, 11.5, -2}, [3]float64{3, 12, 4}, [3]float64{-5, 21.5, 0}
		}
		out[i] = c
	}
	return out
}()

// locatorBones are the standard model's bones that draw nothing.
var locatorBones = map[string]bool{"root": true, "waist": true, "cape": true, "leftItem": true, "rightItem": true}

func standardCubes(slim bool) []stdCube {
	if slim {
		return stdSlim
	}
	return stdWide
}

// standardGeometry is the standard model as a geometry.
func standardGeometry(slim bool) *geometry {
	g := &geometry{name: "geometry.humanoid.custom", texW: 64, texH: 64}
	if slim {
		g.name = "geometry.humanoid.customSlim"
	}
	for _, c := range standardCubes(slim) {
		uv, _ := json.Marshal([]float64{c.uv[0], c.uv[1]})
		g.bones = append(g.bones, &bone{Name: c.bone, Parent: c.parent, Pivot: c.pivot[:], Cubes: []cube{{
			Origin: c.origin[:], Size: c.size[:], UV: uv, Inflate: c.inflate,
		}}})
	}
	return g
}

// shapeOf is how a skin's geometry compares with the standard model.
type shapeOf struct {
	found    bool            // the geometry was in the data or built in
	standard bool            // the base parts are exactly the standard model's
	slim     bool            // ... the slim one's
	extras   bool            // there is more than the standard model draws
	missing  map[string]bool // standard second-layer bones it does not have
}

// compare matches a geometry against the standard model: every base part
// (head, body, arms, legs) must be there, unrotated, with exactly the standard
// cube; second-layer bones must be standard or absent. Anything else drawn
// (another bone with cubes, another cube on a standard bone) is an extra.
func compare(g *geometry) shapeOf {
	s := shapeOf{found: g != nil}
	if g == nil {
		return s
	}
	if g.texW != 0 && (g.texW != 64 || g.texH != 64 && g.texH != 32) {
		return s
	}
	byName := map[string]*bone{}
	for _, b := range g.bones {
		byName[b.Name] = b
	}
	for _, slim := range []bool{false, true} {
		if ok, extras, missing := matches(byName, g.bones, standardCubes(slim)); ok {
			s.standard, s.slim, s.extras, s.missing = true, slim, extras, missing
			return s
		}
	}
	return s
}

func matches(byName map[string]*bone, bones []*bone, std []stdCube) (ok, extras bool, missing map[string]bool) {
	missing = map[string]bool{}
	known := map[string]bool{}
	for _, c := range std {
		known[c.bone] = true
		b := byName[c.bone]
		if b == nil || !b.visible() {
			if c.overlay {
				missing[c.bone] = true
				continue
			}
			return false, false, nil
		}
		if rotated(b.Rotation) || b.PolyMesh != nil && len(b.PolyMesh.Positions) > 0 {
			return false, false, nil
		}
		found := false
		for _, cu := range b.Cubes {
			if sameCube(cu, c) {
				found = true
			} else {
				extras = true
			}
		}
		if !found {
			return false, false, nil
		}
	}
	for _, b := range bones {
		if !known[b.Name] && !locatorBones[b.Name] && b.visible() {
			extras = true
		}
	}
	return true, extras, missing
}

func rotated(r []float64) bool {
	for _, v := range r {
		if math.Abs(v) > 1e-6 {
			return true
		}
	}
	return false
}

func sameCube(cu cube, c stdCube) bool {
	if !vecIs(cu.Origin, c.origin) || !vecIs(cu.Size, c.size) || math.Abs(cu.Inflate-c.inflate) > 1e-6 || rotated(cu.Rotation) {
		return false
	}
	var uv []float64
	if json.Unmarshal(cu.UV, &uv) != nil || len(uv) != 2 {
		return false
	}
	return math.Abs(uv[0]-c.uv[0]) < 1e-6 && math.Abs(uv[1]-c.uv[1]) < 1e-6
}

func vecIs(v []float64, want [3]float64) bool {
	if len(v) != 3 {
		return false
	}
	for i := range v {
		if math.Abs(v[i]-want[i]) > 1e-3 {
			return false
		}
	}
	return true
}
