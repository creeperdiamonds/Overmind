package portal

import (
	"context"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
)

// endSpawnX, endSpawnY and endSpawnZ are the centre of the End arrival platform.
const (
	endSpawnX = 100
	endSpawnY = 49
	endSpawnZ = 0
)

// EndSpawnPosition returns the Bedrock End platform arrival position: y=49 for players and y=50 for other entities.
func EndSpawnPosition(player bool) mgl64.Vec3 {
	y := endSpawnY + 1
	if player {
		y = endSpawnY
	}
	return mgl64.Vec3{float64(endSpawnX) + 0.5, float64(y), float64(endSpawnZ) + 0.5}
}

// GenerateEndSpawnPlatform builds the 5x5 obsidian arrival platform at (100, 48, 0) and clears the 5x5x3 air column
// above it. It runs on every travel into the End, matching vanilla's unconditional regeneration.
func GenerateEndSpawnPlatform(tx *world.Tx) {
	ob := obsidian()
	for dx := -2; dx <= 2; dx++ {
		for dz := -2; dz <= 2; dz++ {
			tx.SetBlock(cube.Pos{endSpawnX + dx, endSpawnY - 1, endSpawnZ + dz}, ob, nil)
			for dy := 0; dy < 3; dy++ {
				tx.SetBlock(cube.Pos{endSpawnX + dx, endSpawnY + dy, endSpawnZ + dz}, nil, nil)
			}
		}
	}
}

// LoadEndSpawnPlatform loads the chunks under the arrival platform through a loader, so the chunk workers make
// them, and waits up to 30 s for them, so GenerateEndSpawnPlatform does not generate them on the End's tick. It must
// not run in one of w's transactions. The returned func lets the chunks go; call it once the entity has arrived.
// Overmind fork.
func LoadEndSpawnPlatform(w *world.World) (release func()) {
	l := world.NewLoader(1, w, world.NopViewer{})
	release = func() { w.Do(func(tx *world.Tx) { l.Close(tx) }) }
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	// The platform spans x 98 to 102 and z -2 to 2: chunk 6 at z -1 and 0, both within radius 1 of (100, 0).
	need := []world.ChunkPos{{endSpawnX >> 4, (endSpawnZ - 2) >> 4}, {endSpawnX >> 4, (endSpawnZ + 2) >> 4}}
	for {
		if err := w.Do(func(tx *world.Tx) {
			l.Move(tx, mgl64.Vec3{endSpawnX + 0.5, endSpawnY, endSpawnZ + 0.5})
			l.ChangeRadius(tx, 1) // Fills the load queue even when the loader is already there.
			l.Load(tx, 16)
		}).Wait(ctx); err != nil {
			return release
		}
		loaded := true
		for _, pos := range need {
			if _, ok := l.Chunk(pos); !ok {
				loaded = false
			}
		}
		if loaded {
			return release
		}
		select {
		case <-ctx.Done():
			return release
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// endRingFrame is one of the twelve canonical ring positions and the Facing each frame must have.
type endRingFrame struct {
	pos    cube.Pos
	facing cube.Direction
}

// endFrameBlock is implemented by block.EndPortalFrame, which cannot be imported here directly.
type endFrameBlock interface {
	world.Block
	EndPortalFrameState() (eye bool, facing cube.Direction)
}

// ActivateEndPortal fills the 3x3 interior with end_portal blocks if a complete twelve-frame ring exists around the
// frame at the position passed. All twelve frames must hold an eye and face toward the centre, as in vanilla.
func ActivateEndPortal(tx *world.Tx, framePos cube.Pos) bool {
	f, ok := tx.Block(framePos).(endFrameBlock)
	if !ok {
		return false
	}
	_, facing := f.EndPortalFrameState()

	// The frame may be the left, middle or right of its side: walk inward twice, then try the three candidate centres.
	inward, tangent := facing.Face(), facing.RotateRight().Face()
	base := framePos.Side(inward).Side(inward)
	for _, center := range []cube.Pos{base.Side(tangent.Opposite()), base, base.Side(tangent)} {
		interior, ok := matchEndRing(tx, center)
		if !ok {
			continue
		}
		ep := endPortal()
		active := true
		for _, pos := range interior {
			if tx.Block(pos) != ep {
				active = false
				break
			}
		}
		if active {
			return false
		}
		ctx := tx.Event()
		positions := append([]cube.Pos(nil), interior...)
		if tx.World().Handler().HandlePortalActivate(ctx, world.End, positions); ctx.Cancelled() {
			return false
		}
		for _, pos := range interior {
			if tx.Block(pos) != ep {
				tx.SetBlock(pos, ep, nil)
			}
		}
		return true
	}
	return false
}

// EndPortalRingIntact reports whether an intact twelve-frame ring still surrounds the end_portal block at the
// position passed, which may be any of the nine interior positions.
func EndPortalRingIntact(tx *world.Tx, portalPos cube.Pos) bool {
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			if _, ok := matchEndRing(tx, portalPos.Add(cube.Pos{dx, 0, dz})); ok {
				return true
			}
		}
	}
	return false
}

// DeactivateEndPortal clears every end_portal block reachable from the position passed through cardinal neighbours
// on the same y plane, removing a single portal's interior without touching unrelated portals.
func DeactivateEndPortal(tx *world.Tx, portalPos cube.Pos) {
	ep := endPortal()
	if tx.Block(portalPos) != ep {
		return
	}
	var positions []cube.Pos
	queue := []cube.Pos{portalPos}
	seen := map[cube.Pos]struct{}{portalPos: {}}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if tx.Block(p) != ep {
			continue
		}
		positions = append(positions, p)
		for _, face := range cube.HorizontalFaces() {
			n := p.Side(face)
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			queue = append(queue, n)
		}
	}
	deactivate(tx, positions)
}

// matchEndRing returns the 3x3 interior positions if the twelve canonical ring positions around centre all hold
// matching frames.
func matchEndRing(tx *world.Tx, center cube.Pos) ([]cube.Pos, bool) {
	for _, want := range expectedEndRingFrames(center) {
		b, ok := tx.Block(want.pos).(endFrameBlock)
		if !ok {
			return nil, false
		}
		eye, facing := b.EndPortalFrameState()
		if !eye || facing != want.facing {
			return nil, false
		}
	}
	return endRingInterior(center), true
}

// expectedEndRingFrames returns the twelve (position, facing) pairs a complete ring around the centre must have, with
// every frame facing toward the centre.
func expectedEndRingFrames(center cube.Pos) []endRingFrame {
	frames := make([]endRingFrame, 0, 12)
	for _, side := range cube.Directions() {
		base := center.Side(side.Face()).Side(side.Face())
		tangent := side.RotateRight().Face()
		inward := side.Opposite()
		for _, pos := range []cube.Pos{base.Side(tangent.Opposite()), base, base.Side(tangent)} {
			frames = append(frames, endRingFrame{pos: pos, facing: inward})
		}
	}
	return frames
}

// endRingInterior returns the nine 3x3 interior positions on the y plane of centre.
func endRingInterior(center cube.Pos) []cube.Pos {
	out := make([]cube.Pos, 0, 9)
	for dx := -1; dx <= 1; dx++ {
		for dz := -1; dz <= 1; dz++ {
			out = append(out, center.Add(cube.Pos{dx, 0, dz}))
		}
	}
	return out
}

// endPortal returns the end_portal block.
func endPortal() world.Block {
	p, ok := world.BlockByName("minecraft:end_portal", nil)
	if !ok {
		panic("could not find end_portal block")
	}
	return p
}
