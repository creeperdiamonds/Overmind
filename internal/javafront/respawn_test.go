package javafront_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// overworldBed lays a bed in the Overworld and makes it the player's spawn, as sleeping in it does (the
// spawn is the head), and returns the head.
func overworldBed(t *testing.T, srv *testserver.Server, id uuid.UUID) cube.Pos {
	t.Helper()
	foot, head := cube.Pos{8, -60, 8}, cube.Pos{8, -60, 9}
	setBlock(t, srv, foot, block.Bed{Colour: item.ColourRed(), Facing: cube.South})
	setBlock(t, srv, head, block.Bed{Colour: item.ColourRed(), Facing: cube.South, Head: true})
	srv.World().SetPlayerSpawn(id, head)
	return head
}

// where is the world a player is in and its position.
func where(t *testing.T, on func(func(p *player.Player))) (w *world.World, pos mgl64.Vec3) {
	t.Helper()
	on(func(p *player.Player) { w, pos = p.Tx().World(), p.Position() })
	return w, pos
}

// A player who dies in the Nether or the End comes back at its Overworld bed, as in vanilla, on both
// editions. The Dragonfly fork looked for the bed in the world the player died in, found none there and
// sent it to the world spawn with "bed missing or obstructed".
func TestDeathElsewhereRespawnsAtOverworldBed(t *testing.T) {
	srv := testserver.Start(t)
	for _, dim := range []world.Dimension{world.Nether, world.End} {
		target := srv.Nether()
		if dim == world.End {
			target = srv.End()
		}
		t.Run("java "+fmt.Sprint(target.Dimension()), func(t *testing.T) {
			name := "JDies" + fmt.Sprint(target.Dimension())[:3]
			j := startBot(t, srv, name)
			waitOnGround(t, srv, name)
			on := func(f func(p *player.Player)) { onJava(t, srv, name, f) }
			var id uuid.UUID
			on(func(p *player.Player) { id = p.UUID() })
			head := overworldBed(t, srv, id)
			moveTo(t, srv, name, target, mgl64.Vec3{0.5, 70, 0.5})
			require.Eventually(t, func() bool { w, _ := where(t, on); return w == target }, 10*time.Second, 50*time.Millisecond)
			on(func(p *player.Player) { p.Hurt(1000, entity.VoidDamageSource{}) })
			require.Eventually(t, func() bool { h := j.state().Seen.Health; return h != nil && *h <= 0 }, 10*time.Second, 50*time.Millisecond, "the client never saw itself die")
			j.respawn()
			require.Eventually(t, func() bool {
				w, pos := where(t, on)
				return w == srv.World() && near(pos, head)
			}, 10*time.Second, 100*time.Millisecond, "the player did not come back at its bed")
			told := fmt.Sprintf("%+v", j.chat())
			for _, bad := range []string{"not_valid", "notValid", "missing"} {
				require.NotContains(t, told, bad, "the client was told its bed was missing")
			}
		})
		t.Run("bedrock "+fmt.Sprint(target.Dimension()), func(t *testing.T) {
			name := "BDies" + fmt.Sprint(target.Dimension())[:3]
			b, err := bedrock.Join(srv.BedrockAddr, name)
			require.NoError(t, err)
			defer b.Close()
			on := func(f func(p *player.Player)) { onBedrock(t, srv, name, f) }
			var id uuid.UUID
			on(func(p *player.Player) { id = p.UUID() })
			head := overworldBed(t, srv, id)
			on(func(p *player.Player) {
				h := p.Tx().RemoveEntity(p)
				target.Do(func(tx *world.Tx) { tx.AddEntity(h).(*player.Player).Teleport(mgl64.Vec3{0.5, 70, 0.5}) })
			})
			require.Eventually(t, func() bool { w, _ := where(t, on); return w == target }, 10*time.Second, 50*time.Millisecond)
			on(func(p *player.Player) { p.Hurt(1000, entity.VoidDamageSource{}) })
			require.Eventually(t, func() bool { return b.Health() <= 0 }, 10*time.Second, 50*time.Millisecond, "the client never saw itself die")
			require.NoError(t, b.Respawn())
			require.Eventually(t, func() bool {
				w, pos := where(t, on)
				return w == srv.World() && near(pos, head)
			}, 10*time.Second, 100*time.Millisecond, "the player did not come back at its bed")
			told := fmt.Sprintf("%+v", b.Texts())
			for _, bad := range []string{"notValid", "missing"} {
				require.NotContains(t, told, bad, "the client was told its bed was missing")
			}
		})
	}
}

// near says whether a player stands by a bed: Dragonfly's SafeSpawn picks a free block around it.
func near(pos mgl64.Vec3, bed cube.Pos) bool {
	return pos.Sub(bed.Vec3Middle()).Len() < 3
}
