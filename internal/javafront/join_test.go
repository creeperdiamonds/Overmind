package javafront_test

import (
	"encoding/json"
	"net"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/sandertv/go-raknet"
	"github.com/stretchr/testify/require"

	"github.com/overmind/overmind/bots/bedrock"
	"github.com/overmind/overmind/internal/javaproto"
	"github.com/overmind/overmind/internal/javawire"
	"github.com/overmind/overmind/internal/tables/java261"
	"github.com/overmind/overmind/internal/testserver"
)

// onJava runs f on a Java player's world goroutine and waits for it.
func onJava(t *testing.T, srv *testserver.Server, name string, f func(p *player.Player)) {
	t.Helper()
	h, ok := srv.Java.Player(name)
	require.True(t, ok, "%s is not online on Java", name)
	done := make(chan struct{})
	player.Do(h, func(_ *world.Tx, p *player.Player) { f(p); close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the world never ran a task for %s", name)
	}
}

// onBedrock is onJava for a Bedrock player.
func onBedrock(t *testing.T, srv *testserver.Server, name string, f func(p *player.Player)) {
	t.Helper()
	var h *world.EntityHandle
	require.Eventually(t, func() bool {
		var ok bool
		h, ok = srv.Server.Server.PlayerByName(name)
		return ok
	}, 15*time.Second, 50*time.Millisecond, "%s never came online on Bedrock", name)
	done := make(chan struct{})
	h.Do(func(_ *world.Tx, e world.Entity) { f(e.(*player.Player)); close(done) })
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatalf("the world never ran a task for %s", name)
	}
}

// bedrockTranslations counts the translated messages a Bedrock bot got with
// this key and these arguments.
func bedrockTranslations(b *bedrock.Bot, key string, params ...string) int {
	n := 0
	for _, m := range b.Texts() {
		if m.Translation && strings.Contains(m.Message, "%"+key) && slices.Equal(m.Params, params) {
			n++
		}
	}
	return n
}

// javaTranslations counts the system messages a Java bot got that are this
// translation, in this colour, with these arguments.
func javaTranslations(b *javaBot, key, color string, params ...string) int {
	n := 0
	for _, l := range b.chat() {
		c := plain(l.Msg)
		if l.Kind != "system" || c.Translate != key || c.Color != color || len(c.With) != len(params) {
			continue
		}
		same := true
		for i, w := range c.With {
			same = same && w.Text == params[i]
		}
		if same {
			n++
		}
	}
	return n
}

// Joining and leaving go through one place for both editions: each edition is
// told in its own format, once.
func TestJoinAndLeaveSeenByBothEditions(t *testing.T) {
	srv := testserver.Start(t)
	first, err := bedrock.Join(srv.BedrockAddr, "BFirst")
	require.NoError(t, err)
	defer first.Close()
	java := startBot(t, srv, "JSecond")
	waitOnGround(t, srv, "JSecond")

	// Bedrock's format: a translation its client looks up.
	require.Eventually(t, func() bool { return bedrockTranslations(first, "multiplayer.player.joined", "JSecond") == 1 }, 10*time.Second, 50*time.Millisecond,
		"Bedrock was not told the Java player joined: %+v", first.Texts())
	// Java's: system chat with Java's own key, in yellow.
	third, err := bedrock.Join(srv.BedrockAddr, "BThird")
	require.NoError(t, err)
	require.Eventually(t, func() bool { return javaTranslations(java, "multiplayer.player.joined", "yellow", "BThird") == 1 }, 10*time.Second, 50*time.Millisecond,
		"Java was not told the Bedrock player joined: %+v", java.chat())
	require.Eventually(t, func() bool { return bedrockTranslations(first, "multiplayer.player.joined", "BThird") == 1 }, 10*time.Second, 50*time.Millisecond)

	require.NoError(t, third.Close())
	require.Eventually(t, func() bool { return javaTranslations(java, "multiplayer.player.left", "yellow", "BThird") == 1 }, 10*time.Second, 50*time.Millisecond,
		"Java was not told the Bedrock player left: %+v", java.chat())
	java.quit()
	require.Eventually(t, func() bool { return bedrockTranslations(first, "multiplayer.player.left", "JSecond") == 1 }, 10*time.Second, 50*time.Millisecond,
		"Bedrock was not told the Java player left: %+v", first.Texts())

	// Dragonfly's own messages are off, so nothing came twice.
	time.Sleep(500 * time.Millisecond)
	require.Equal(t, 1, bedrockTranslations(first, "multiplayer.player.joined", "BThird"))
	require.Equal(t, 1, bedrockTranslations(first, "multiplayer.player.left", "BThird"))
}

// Both editions list every player, whether or not they can see them.
func TestPlayerListsShowBothEditions(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BListed")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JListed")
	waitOnGround(t, srv, "JListed")
	other := startBot(t, srv, "JOther")
	waitOnGround(t, srv, "JOther")

	all := []string{"BListed", "JListed", "JOther"}
	hasAll := func(names []string) bool {
		for _, n := range all {
			if !slices.Contains(names, n) {
				return false
			}
		}
		return true
	}
	require.Eventually(t, func() bool { return hasAll(j.tab()) }, 10*time.Second, 50*time.Millisecond, "Java's list: %v", j.tab())
	require.Eventually(t, func() bool { return hasAll(b.Listed()) }, 10*time.Second, 50*time.Millisecond, "Bedrock's list: %v", b.Listed())

	// Out of sight is not out of the list.
	onJava(t, srv, "JListed", func(p *player.Player) { p.Teleport(mgl64.Vec3{3000.5, 100, 3000.5}) })
	require.Eventually(t, func() bool { _, seen := b.Player("JListed"); return !seen }, 10*time.Second, 50*time.Millisecond, "Bedrock still sees the far player")
	require.Eventually(t, func() bool { _, seen := j.players()["BListed"]; return !seen }, 10*time.Second, 50*time.Millisecond, "Java still sees the far player")
	time.Sleep(500 * time.Millisecond)
	require.True(t, hasAll(j.tab()), "Java's list lost a player it cannot see: %v", j.tab())
	require.True(t, hasAll(b.Listed()), "Bedrock's list lost a player it cannot see: %v", b.Listed())

	// Leaving takes the rows away, on both.
	j.quit()
	require.Eventually(t, func() bool { return !slices.Contains(b.Listed(), "JListed") }, 10*time.Second, 50*time.Millisecond, "Bedrock still lists the Java player")
	require.Eventually(t, func() bool { return !slices.Contains(other.tab(), "JListed") }, 10*time.Second, 50*time.Millisecond, "Java still lists the Java player")
	require.NoError(t, b.Close())
	require.Eventually(t, func() bool { return !slices.Contains(other.tab(), "BListed") }, 10*time.Second, 50*time.Millisecond, "Java still lists the Bedrock player")
}

// One count of players online, the same for both editions' server lists.
func TestOnlineCountIsBothEditions(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BCounted")
	require.NoError(t, err)
	defer b.Close()
	startBot(t, srv, "JCounted")
	waitOnGround(t, srv, "JCounted")

	require.Eventually(t, func() bool { return srv.Online() == 2 }, 10*time.Second, 50*time.Millisecond)
	require.Equal(t, 2, javaStatusOnline(t, srv.JavaAddr), "the Java server list")
	require.Eventually(t, func() bool { return bedrockStatusOnline(t, srv.BedrockAddr) == 2 }, 15*time.Second, 200*time.Millisecond, "the Bedrock server list")
}

// javaStatusOnline asks the Java server list how many are online.
func javaStatusOnline(t *testing.T, addr string) int {
	t.Helper()
	nc, err := net.DialTimeout("tcp", addr, 5*time.Second)
	require.NoError(t, err)
	defer nc.Close()
	_ = nc.SetDeadline(time.Now().Add(5 * time.Second))
	wc := javawire.NewConn(nc)
	host, port, _ := net.SplitHostPort(addr)
	p, _ := strconv.Atoi(port)
	hs := &javaproto.Writer{}
	hs.VarInt(javaproto.HandshakeSetProtocol)
	hs.VarInt(java261.ProtocolVersion)
	hs.String(host)
	hs.B = append(hs.B, byte(p>>8), byte(p))
	hs.VarInt(javaproto.NextStatus)
	require.NoError(t, wc.WriteFrame(hs.B))
	req := &javaproto.Writer{}
	req.VarInt(javaproto.StatusRequest)
	require.NoError(t, wc.WriteFrame(req.B))
	frame, err := wc.ReadFrame()
	require.NoError(t, err)
	r := javaproto.NewReader(frame)
	require.Equal(t, javaproto.StatusResponse, r.VarInt())
	var st struct {
		Players struct {
			Online int `json:"online"`
		} `json:"players"`
	}
	require.NoError(t, json.Unmarshal([]byte(r.String(32767)), &st))
	return st.Players.Online
}

// bedrockStatusOnline asks the Bedrock server list (a RakNet pong) how many are
// online: the fifth field.
func bedrockStatusOnline(t *testing.T, addr string) int {
	t.Helper()
	pong, err := raknet.PingTimeout(addr, 2*time.Second)
	if err != nil {
		return -1
	}
	f := strings.Split(string(pong), ";")
	if len(f) < 5 {
		return -1
	}
	n, _ := strconv.Atoi(f[4])
	return n
}

// What a player was when it left is what it is when it comes back: position,
// health, food, inventory and game mode, on each edition by its own account.
func TestPlayerDataSurvivesRejoin(t *testing.T) {
	where := mgl64.Vec3{8.5, groundY, 3.5}
	set := func(p *player.Player) {
		p.Hurt(7, entity.VoidDamageSource{})
		p.SetFood(7)
		_ = p.Inventory().SetItem(3, item.NewStack(block.Dirt{}, 5))
		_ = p.Inventory().SetItem(20, item.NewStack(item.Stick{}, 9))
		p.Armour().SetHelmet(item.NewStack(item.Helmet{Tier: item.ArmourTierIron{}}, 1))
		p.SetGameMode(world.GameModeCreative)
		p.Teleport(where)
	}
	// Read on the world's goroutine, checked on the test's: a failed require
	// must not end the world's goroutine.
	type snapshot struct {
		pos          mgl64.Vec3
		health       float64
		food         int
		mode         world.GameMode
		dirt, sticks item.Stack
		helmet       item.Stack
	}
	read := func(p *player.Player) snapshot {
		s := snapshot{pos: p.Position(), health: p.Health(), food: p.Food(), mode: p.GameMode(), helmet: p.Armour().Helmet()}
		s.dirt, _ = p.Inventory().Item(3)
		s.sticks, _ = p.Inventory().Item(20)
		return s
	}
	check := func(t *testing.T, s snapshot) {
		require.Less(t, s.pos.Sub(where).Len(), 0.6, "position %v", s.pos)
		require.InDelta(t, 13, s.health, 0.01, "health")
		require.Equal(t, 7, s.food, "food")
		require.Equal(t, world.GameModeCreative, s.mode, "game mode")
		require.True(t, s.dirt.Comparable(item.NewStack(block.Dirt{}, 1)) && s.dirt.Count() == 5, "slot 3 holds %v", s.dirt)
		require.Equal(t, 9, s.sticks.Count(), "slot 20 holds %v", s.sticks)
		_, isHelmet := s.helmet.Item().(item.Helmet)
		require.True(t, isHelmet, "the helmet is gone")
	}

	t.Run("java", func(t *testing.T) {
		srv := testserver.Start(t)
		first := startBot(t, srv, "Keeper")
		waitOnGround(t, srv, "Keeper")
		onJava(t, srv, "Keeper", set)
		require.Eventually(t, func() bool { return first.state().Me.X == where.X() }, 5*time.Second, 50*time.Millisecond, "the client never moved")
		first.quit()
		require.Eventually(t, func() bool { return !srv.JavaOnline("Keeper") }, 10*time.Second, 50*time.Millisecond)

		again := startBot(t, srv, "Keeper")
		require.Eventually(t, func() bool { return srv.JavaOnline("Keeper") }, 10*time.Second, 50*time.Millisecond)
		var got snapshot
		onJava(t, srv, "Keeper", func(p *player.Player) { got = read(p) })
		check(t, got)
		s := again.state()
		require.Equal(t, "creative", s.Seen.Login.WorldState.GameMode, "the client was told the wrong game mode")
		require.InDelta(t, where.X(), s.Me.X, 0.01, "the client was put somewhere else")
		require.InDelta(t, where.Z(), s.Me.Z, 0.01, "the client was put somewhere else")
		// And it was shown the inventory it came back to, at once.
		require.Eventually(t, func() bool {
			w := botWindow(again.inventory())
			return w[39] == "dirt:5" && w[20] == "stick:9" && w[5] == "iron_helmet:1"
		}, 5*time.Second, 50*time.Millisecond, "the client shows %v", botWindow(again.inventory()))
	})
	t.Run("bedrock", func(t *testing.T) {
		srv := testserver.Start(t)
		account := uuid.New()
		first, err := bedrock.JoinAs(srv.BedrockAddr, "BKeeper", account)
		require.NoError(t, err)
		onBedrock(t, srv, "BKeeper", set)
		require.NoError(t, first.Close())
		require.Eventually(t, func() bool { return !srv.PlayerByName("BKeeper") }, 10*time.Second, 50*time.Millisecond)

		again, err := bedrock.JoinAs(srv.BedrockAddr, "BKeeper", account)
		require.NoError(t, err)
		defer again.Close()
		var got snapshot
		onBedrock(t, srv, "BKeeper", func(p *player.Player) { got = read(p) })
		check(t, got)
	})
	t.Run("editions keep apart", func(t *testing.T) {
		// The same UUID on the other edition is another account.
		srv := testserver.Start(t)
		first := startBot(t, srv, "Twin")
		waitOnGround(t, srv, "Twin")
		onJava(t, srv, "Twin", set)
		var id uuid.UUID
		onJava(t, srv, "Twin", func(p *player.Player) { id = p.UUID() })
		first.quit()
		require.Eventually(t, func() bool { return !srv.JavaOnline("Twin") }, 10*time.Second, 50*time.Millisecond)

		b, err := bedrock.JoinAs(srv.BedrockAddr, "Twin", id)
		require.NoError(t, err)
		defer b.Close()
		var got snapshot
		onBedrock(t, srv, "Twin", func(p *player.Player) { got = read(p) })
		require.InDelta(t, 20, got.health, 0.01, "Bedrock got the Java player's data")
		require.True(t, got.dirt.Empty(), "Bedrock got the Java player's items")
	})
}
