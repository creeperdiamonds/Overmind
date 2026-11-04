package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// Overmind fork: snow drops snowballs by edition (minecraft.wiki "Snow", oldid 3859371). Java gives 1 a
// layer; Bedrock 1 for 1 to 3 layers, 2 for 4 or 5, 3 for 6 or 7 and 4 for 8.
func TestSnowLayerDropsByEdition(t *testing.T) {
	for _, c := range []struct {
		java bool
		want [8]int
	}{
		{true, [8]int{1, 2, 3, 4, 5, 6, 7, 8}},
		{false, [8]int{1, 1, 1, 2, 2, 3, 3, 4}},
	} {
		w := world.Config{Synchronous: true, Entities: entity.DefaultRegistry,
			Mechanics: world.Mechanics{JavaWeather: c.java}}.New()
		t.Cleanup(func() { _ = w.Close() })
		pos := mgl64.Vec3{0.5, 100, 0.5}
		id := uuid.New()
		h := world.EntitySpawnOpts{Position: pos, ID: id}.New(Type, Config{Name: "digger", UUID: id, Position: pos})
		w.Do(func(tx *world.Tx) {
			p := tx.AddEntity(h).(*Player)
			p.SetGameMode(world.GameModeSurvival)
			shovel := item.NewStack(item.Shovel{Tier: item.ToolTierIron}, 1)
			for height, want := range c.want {
				n := 0
				for _, s := range p.drops(shovel, block.SnowLayer{Height: height}) {
					if _, ok := s.Item().(item.Snowball); !ok {
						t.Fatalf("java %v: %d layers dropped %v", c.java, height+1, s)
					}
					n += s.Count()
				}
				if n != want {
					t.Errorf("java %v: %d layers dropped %d snowballs, want %d", c.java, height+1, n, want)
				}
			}
		})
	}
}
