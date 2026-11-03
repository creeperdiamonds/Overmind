package javafront_test

import (
	"slices"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// damageTypeID is a damage type's place in the registry Java was sent.
func damageTypeID(t *testing.T, name string) int32 {
	t.Helper()
	for _, r := range java261.Registries() {
		if r.ID == "minecraft:damage_type" {
			if i := slices.Index(r.Entries, "minecraft:"+name); i >= 0 {
				return int32(i)
			}
		}
	}
	t.Fatalf("no damage type %s", name)
	return -1
}

func healthOf(t *testing.T, srv *testserver.Server, name string, java bool) float64 {
	t.Helper()
	var hp float64
	onServer(t, srv, name, java, func(p *player.Player) { hp = p.Health() })
	return hp
}

// hungry stops natural regeneration, which would change health under a test.
func hungry(t *testing.T, srv *testserver.Server, name string, java bool) {
	t.Helper()
	onServer(t, srv, name, java, func(p *player.Player) { p.SetFood(10) })
}

// combatants has a Bedrock bot at (bx, 0.5) and a Java bot at the spawn, each
// seeing the other.
func combatants(t *testing.T, srv *testserver.Server, bedrockName, javaName string, bx float32) (*bedrock.Bot, *javaBot) {
	t.Helper()
	b, err := bedrock.Join(srv.BedrockAddr, bedrockName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = b.Close() })
	j := startBot(t, srv, javaName)
	waitOnGround(t, srv, javaName)
	b.GoTo(bx, 0.5)
	require.Eventually(t, func() bool { p, ok := srv.PlayerPosition(bedrockName); return ok && p.X() > float64(bx)-0.1 }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { _, ok := b.RuntimeIDOf(javaName); return ok }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { return j.entities().idOf(bedrockName) >= 0 }, 10*time.Second, 50*time.Millisecond)
	hungry(t, srv, bedrockName, false)
	hungry(t, srv, javaName, true)
	return b, j
}

// A Java player hits a Bedrock player through Dragonfly's attack: the health
// drops on the server and on the Bedrock client, which is knocked back and
// shown its hurt, and Java sees the damage and the new health.
func TestJavaHitsBedrock(t *testing.T) {
	srv := testserver.Start(t)
	b, j := combatants(t, srv, "BTarget", "JHitter", 1.5)
	id := j.entities().idOf("BTarget")
	time.Sleep(time.Second) // a full charge

	j.attack(id)

	require.Eventually(t, func() bool { return healthOf(t, srv, "BTarget", false) == 19 }, 5*time.Second, 50*time.Millisecond, "a charged punch is 1")
	// The attack swings the arm once, though the client also sends a swing.
	require.Eventually(t, func() bool { return b.Swings("JHitter") == 1 }, 5*time.Second, 50*time.Millisecond, "Bedrock did not see the swing")
	time.Sleep(300 * time.Millisecond)
	require.Equal(t, 1, b.Swings("JHitter"), "the attack's swing was shown twice")
	j.swing() // and a swing at nothing is shown
	require.Eventually(t, func() bool { return b.Swings("JHitter") == 2 }, 5*time.Second, 50*time.Millisecond, "a swing at the air was not shown")
	require.Eventually(t, func() bool { return b.Health() == 19 }, 5*time.Second, 50*time.Millisecond, "the Bedrock client was not told its health")
	require.Eventually(t, func() bool { return slices.Contains(b.Events("BTarget"), int32(packet.ActorEventHurt)) }, 5*time.Second, 50*time.Millisecond, "the Bedrock client was not shown its hurt")
	require.Eventually(t, func() bool { return len(b.Pushes()) > 0 }, 5*time.Second, 50*time.Millisecond, "no knockback")
	require.Eventually(t, func() bool {
		for _, d := range j.entities().Events.Damage[id] {
			if d.Type == damageTypeID(t, "player_attack") && d.Cause == 2 { // the attacker: entity 1, plus one
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "Java was not shown the damage")
	require.Eventually(t, func() bool {
		hp := -1.0
		r := j.entities()
		return r.Entities[id].meta(t, mHP, &hp) && hp == 19
	}, 5*time.Second, 50*time.Millisecond, "Java was not shown the new health")
}

// The reverse: a Bedrock player hits a Java player. The Java client sees its
// health drop, its view tilt, the damage and a push; another Java player sees
// the damage and health; Bedrock sees the hurt.
func TestBedrockHitsJava(t *testing.T) {
	srv := testserver.Start(t)
	b, j := combatants(t, srv, "BHitter", "JTarget", 1.5)
	watch := startBot(t, srv, "JWatch")
	require.Eventually(t, func() bool { return watch.entities().idOf("JTarget") >= 0 }, 10*time.Second, 50*time.Millisecond)
	hitter := j.entities().idOf("BHitter")

	require.NoError(t, b.Attack("JTarget"))

	require.Eventually(t, func() bool { return healthOf(t, srv, "JTarget", true) == 19 }, 5*time.Second, 50*time.Millisecond, "a Bedrock punch is 1")
	require.Eventually(t, func() bool { return slices.Contains(j.entities().Events.Animations[hitter], 0) }, 5*time.Second, 50*time.Millisecond, "Java did not see the Bedrock player swing")
	require.Eventually(t, func() bool { h := j.state().Seen.Health; return h != nil && *h == 19 }, 5*time.Second, 50*time.Millisecond, "the Java client was not told its health")
	require.Eventually(t, func() bool {
		r := j.entities()
		hurt := slices.ContainsFunc(r.Events.Hurt, func(h struct {
			ID  int32
			Yaw float64
		}) bool {
			return h.ID == 1
		})
		for _, d := range r.Events.Damage[1] {
			if hurt && d.Type == damageTypeID(t, "player_attack") && d.Cause == hitter+1 {
				return true
			}
		}
		return false
	}, 5*time.Second, 50*time.Millisecond, "the Java client was not shown its own hurt")
	require.Eventually(t, func() bool { return len(j.entities().Self.Velocities) > 0 }, 5*time.Second, 50*time.Millisecond, "the Java client was not knocked back")
	watched := watch.entities().idOf("JTarget")
	require.Eventually(t, func() bool {
		r := watch.entities()
		hp := -1.0
		return len(r.Events.Damage[watched]) > 0 && r.Events.Damage[watched][0].Type == damageTypeID(t, "player_attack") &&
			r.Entities[watched].meta(t, mHP, &hp) && hp == 19
	}, 5*time.Second, 50*time.Millisecond, "the other Java player did not see the damage")
	require.Eventually(t, func() bool { return slices.Contains(b.Events("JTarget"), int32(packet.ActorEventHurt)) }, 5*time.Second, 50*time.Millisecond, "Bedrock did not see the hurt")
}

// Each player follows its own edition's rules: a Java player's attacks charge
// and hit with Java's weapon damage and reach, a Bedrock player's with
// Dragonfly's (Bedrock's).
func TestEachEditionAttacksByItsOwnRules(t *testing.T) {
	srv := testserver.Start(t)
	b, j := combatants(t, srv, "BDummy", "JRules", 1.5)
	id := j.entities().idOf("BDummy")
	sword := item.NewStack(item.Sword{Tier: item.ToolTierDiamond}, 1) // Java 7, Bedrock (Dragonfly) 8
	holdJava(t, srv, "JRules", sword)
	time.Sleep(time.Second)

	j.attack(id)
	require.Eventually(t, func() bool { return healthOf(t, srv, "BDummy", false) == 13 }, 5*time.Second, 50*time.Millisecond, "a charged Java diamond sword is 7")

	time.Sleep(time.Second) // past the hurt immunity
	j.hold(1)               // a bare hand, and the cooldown starts again
	j.attack(id)
	require.Eventually(t, func() bool { return healthOf(t, srv, "BDummy", false) < 13 }, 5*time.Second, 50*time.Millisecond, "the uncharged punch missed")
	time.Sleep(200 * time.Millisecond)
	lost := 13 - healthOf(t, srv, "BDummy", false)
	require.Greater(t, lost, 0.19)
	require.Less(t, lost, 0.6, "an uncharged punch is 0.2 to about 0.5, not 1")

	t.Run("reach", func(t *testing.T) {
		b.GoTo(5.5, 0.5) // 4.7 from the eyes: past Java's 3 (and our 1), inside Dragonfly's 8
		require.Eventually(t, func() bool { p, ok := srv.PlayerPosition("BDummy"); return ok && p.X() > 5.4 }, 10*time.Second, 50*time.Millisecond)
		time.Sleep(time.Second)
		before := healthOf(t, srv, "BDummy", false)
		j.attack(id)
		time.Sleep(500 * time.Millisecond)
		require.Equal(t, before, healthOf(t, srv, "BDummy", false), "a Java player hit past Java's reach")
	})
	t.Run("bedrock keeps its own", func(t *testing.T) {
		onBedrock(t, srv, "BDummy", func(p *player.Player) {
			_ = p.SetHeldSlot(0)
			_ = p.Inventory().SetItem(0, sword)
		})
		require.NoError(t, b.Attack("JRules"))
		require.Eventually(t, func() bool { return healthOf(t, srv, "JRules", true) == 12 }, 5*time.Second, 50*time.Millisecond, "a Bedrock diamond sword is 8, at once")
	})
}

// Deaths reach Java: a dead player falls over for Java watchers (health 0 and
// the dying pose, with the death sound) instead of standing until it
// respawns, and a Java player's own death brings its death screen with Java's
// message.
func TestDeathsShowOnJava(t *testing.T) {
	srv := testserver.Start(t)
	b, j := combatants(t, srv, "BDoomed", "JDoomed", 1.5)
	watch := startBot(t, srv, "JWatch")
	require.Eventually(t, func() bool { r := watch.entities(); return r.idOf("JDoomed") >= 0 && r.idOf("BDoomed") >= 0 }, 10*time.Second, 50*time.Millisecond)

	t.Run("a Java player killed by a Bedrock player", func(t *testing.T) {
		onJava(t, srv, "JDoomed", func(p *player.Player) { p.Hurt(19, entity.VoidDamageSource{}) })
		time.Sleep(time.Second)
		require.NoError(t, b.Attack("JDoomed"))
		require.Eventually(t, func() bool { h := j.state().Seen.Health; return h != nil && *h <= 0 }, 5*time.Second, 50*time.Millisecond, "the Java client did not die")
		require.Eventually(t, func() bool { return len(j.entities().Events.Deaths) > 0 }, 5*time.Second, 50*time.Millisecond, "no death screen")
		d := j.entities().Events.Deaths[0]
		require.EqualValues(t, 1, d.ID)
		msg := plain(d.Message)
		require.Equal(t, "death.attack.player", msg.Translate)
		require.Len(t, msg.With, 2)
		require.Equal(t, "JDoomed", msg.With[0].Text)
		require.Equal(t, "BDoomed", msg.With[1].Text)
		seen := watch.entities().idOf("JDoomed")
		require.Eventually(t, func() bool { return slices.Contains(watch.entities().Events.Statuses[seen], 3) }, 5*time.Second, 50*time.Millisecond)
		_, p := seenPlayerMeta(t, watch, "JDoomed")
		require.Equal(t, pDying, p)

		j.respawn()
		require.Eventually(t, func() bool { h := j.state().Seen.Health; return h != nil && *h == 20 }, 10*time.Second, 50*time.Millisecond, "the Java player did not come back")
		require.Eventually(t, func() bool { return j.column(0, 0, -61, -61).Loaded }, 10*time.Second, 50*time.Millisecond, "the respawned client has no chunks")
		require.Eventually(t, func() bool { _, p := seenPlayerMeta(t, watch, "JDoomed"); return p == pStanding }, 10*time.Second, 50*time.Millisecond, "the respawned player still lies dead to Java")
	})
	t.Run("a Bedrock player killed by a Java player", func(t *testing.T) {
		onBedrock(t, srv, "BDoomed", func(p *player.Player) { p.Hurt(19, entity.VoidDamageSource{}) })
		time.Sleep(time.Second) // past the hurt immunity, and charged
		id := j.entities().idOf("BDoomed")
		j.attack(id)
		require.Eventually(t, func() bool { return healthOf(t, srv, "BDoomed", false) <= 0 }, 5*time.Second, 50*time.Millisecond, "the Bedrock player did not die")
		for _, eyes := range []*javaBot{j, watch} {
			seen := eyes.entities().idOf("BDoomed")
			require.Eventually(t, func() bool {
				r := eyes.entities()
				hp := -1.0
				return slices.Contains(r.Events.Statuses[seen], 3) && r.Entities[seen].meta(t, mHP, &hp) && hp == 0
			}, 5*time.Second, 50*time.Millisecond, "a Java player did not see the death")
			_, p := seenPlayerMeta(t, eyes, "BDoomed")
			require.Equal(t, pDying, p, "the dead player is not lying down")
		}
		// Dragonfly makes it invisible a second later; it stays down.
		time.Sleep(1500 * time.Millisecond)
		_, p := seenPlayerMeta(t, watch, "BDoomed")
		require.Equal(t, pDying, p, "the dead player got up before it respawned")
	})
}

// The cooldown starts again with every attack, not only with a change of item:
// a second axe blow soon after the first is weaker.
func TestJavaCooldownRestartsAfterEachAttack(t *testing.T) {
	srv := testserver.Start(t)
	_, j := combatants(t, srv, "BAxed", "JAxe", 1.5)
	id := j.entities().idOf("BAxed")
	holdJava(t, srv, "JAxe", item.NewStack(item.Axe{Tier: item.ToolTierIron}, 1)) // Java: 9, speed 0.9, 22.2 ticks
	time.Sleep(1500 * time.Millisecond)

	j.attack(id)
	require.Eventually(t, func() bool { return healthOf(t, srv, "BAxed", false) == 11 }, 5*time.Second, 50*time.Millisecond, "a charged iron axe is 9")
	time.Sleep(800 * time.Millisecond) // past the hurt immunity; about 16 of 22 ticks charged
	j.attack(id)
	require.Eventually(t, func() bool { return healthOf(t, srv, "BAxed", false) < 11 }, 5*time.Second, 50*time.Millisecond, "the second blow missed")
	time.Sleep(200 * time.Millisecond)
	lost := 11 - healthOf(t, srv, "BAxed", false)
	require.Greater(t, lost, 3.0)
	require.Less(t, lost, 8.0, "the second blow was charged from before the first")
}
