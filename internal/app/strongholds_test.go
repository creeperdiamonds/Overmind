package app

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

type oneStronghold struct{ world.NopGenerator }

func (oneStronghold) NearestStronghold(cube.Pos) (cube.Pos, bool) { return cube.Pos{64, 30, -128}, true }

// The world asks its generator, through offTick, where an eye of ender leads. worldgen has no strongholds
// yet (Task 9), so it reports none.
func TestStrongholdsPassThroughOffTick(t *testing.T) {
	var _ world.StrongholdLocator = (*offTick)(nil)
	if at, ok := (&offTick{g: oneStronghold{}}).NearestStronghold(cube.Pos{}); !ok || at != (cube.Pos{64, 30, -128}) {
		t.Errorf("offTick gave %v, %v; want the generator's stronghold", at, ok)
	}
	if _, ok := (&offTick{g: world.NopGenerator{}}).NearestStronghold(cube.Pos{}); ok {
		t.Error("a generator with no strongholds gave one")
	}
	if _, ok := (&offTick{g: worldgen.New(1)}).NearestStronghold(cube.Pos{}); ok {
		t.Error("worldgen gave a stronghold before strongholds generate")
	}
}
