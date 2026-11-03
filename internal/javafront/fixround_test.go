package javafront_test

import (
	"crypto/md5"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/config"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// offlineUUID is the account UUID an offline Java client gets for a name.
func offlineUUID(name string) uuid.UUID {
	sum := md5.Sum([]byte("OfflinePlayer:" + name))
	sum[6] = sum[6]&0x0f | 0x30
	sum[8] = sum[8]&0x3f | 0x80
	return uuid.UUID(sum)
}

// record is the path of an account's saved data on one edition.
func record(srv *testserver.Server, edition string, id uuid.UUID) string {
	return filepath.Join(srv.PlayersFolder, edition, id.String()+".json")
}

// readRecord reads saved data as a plain map, as anything reading the disk
// after a crash would.
func readRecord(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

var spoilers = map[string]func(m map[string]any) []byte{
	"bad uuid": func(m map[string]any) []byte {
		m["UUID"] = "not-a-uuid"
		b, _ := json.Marshal(m)
		return b
	},
	"slot out of range": func(m map[string]any) []byte {
		items := m["Inventory"].(map[string]any)["Items"].([]any)
		items[0].(map[string]any)["Slot"] = 99
		b, _ := json.Marshal(m)
		return b
	},
	"truncated": func(m map[string]any) []byte {
		b, _ := json.Marshal(m)
		return b[:len(b)/2]
	},
}

// A record that cannot be read never takes the server down: it is moved aside,
// the player starts fresh, and the next save is a good record again.
func TestCorruptPlayerRecordsStartFresh(t *testing.T) {
	mark := func(p *player.Player) {
		p.SetFood(7)
		_ = p.Inventory().SetItem(3, item.NewStack(block.Dirt{}, 5))
	}
	type state struct {
		food int
		slot item.Stack
	}
	read := func(p *player.Player) state {
		s, _ := p.Inventory().Item(3)
		return state{p.Food(), s}
	}
	check := func(t *testing.T, srv *testserver.Server, edition string, id uuid.UUID, got state) {
		require.Equal(t, 20, got.food, "the player did not start fresh")
		require.True(t, got.slot.Empty(), "the player did not start fresh")
		moved, _ := filepath.Glob(record(srv, edition, id) + ".*.corrupt")
		require.Len(t, moved, 1, "the bad record was not moved aside")
	}

	t.Run("java", func(t *testing.T) {
		srv := testserver.Start(t)
		i := 0
		for kind, spoil := range spoilers {
			i++
			name := "JSpoil" + string(rune('0'+i))
			id := offlineUUID(name)
			t.Run(kind, func(t *testing.T) {
				first := startBot(t, srv, name)
				waitOnGround(t, srv, name)
				onJava(t, srv, name, mark)
				first.quit()
				require.Eventually(t, func() bool { return !srv.JavaOnline(name) && readRecord(t, record(srv, "java", id)) != nil },
					10*time.Second, 50*time.Millisecond, "no record was saved")
				require.NoError(t, os.WriteFile(record(srv, "java", id), spoil(readRecord(t, record(srv, "java", id))), 0o644))

				again := startBot(t, srv, name)
				waitOnGround(t, srv, name)
				var got state
				onJava(t, srv, name, func(p *player.Player) { got = read(p) })
				check(t, srv, "java", id, got)
				again.quit()
				require.Eventually(t, func() bool { return readRecord(t, record(srv, "java", id)) != nil }, 10*time.Second, 50*time.Millisecond,
					"leaving did not save a good record")
			})
		}
	})
	t.Run("bedrock", func(t *testing.T) {
		srv := testserver.Start(t)
		i := 0
		for kind, spoil := range spoilers {
			i++
			name := "BSpoil" + string(rune('0'+i))
			id := uuid.New()
			t.Run(kind, func(t *testing.T) {
				first, err := bedrock.JoinAs(srv.BedrockAddr, name, id)
				require.NoError(t, err)
				onBedrock(t, srv, name, mark)
				require.NoError(t, first.Close())
				require.Eventually(t, func() bool { return !srv.PlayerByName(name) && readRecord(t, record(srv, "bedrock", id)) != nil },
					10*time.Second, 50*time.Millisecond, "no record was saved")
				require.NoError(t, os.WriteFile(record(srv, "bedrock", id), spoil(readRecord(t, record(srv, "bedrock", id))), 0o644))

				again, err := bedrock.JoinAs(srv.BedrockAddr, name, id)
				require.NoError(t, err, "a bad record kept the player out")
				var got state
				onBedrock(t, srv, name, func(p *player.Player) { got = read(p) })
				check(t, srv, "bedrock", id, got)
				require.NoError(t, again.Close())
				require.Eventually(t, func() bool { return readRecord(t, record(srv, "bedrock", id)) != nil }, 10*time.Second, 50*time.Millisecond,
					"leaving did not save a good record")
			})
		}
	})
}

// Players online are saved every SaveSeconds, so what is on disk, which is all
// a killed server leaves, is at most that old. The server is not stopped.
func TestAutosaveBoundsLoss(t *testing.T) {
	srv := testserver.StartWith(t, func(c *config.Config) { c.SaveSeconds = 1 })
	require.Equal(t, 5, config.Default().SaveSeconds)
	b, err := bedrock.JoinAs(srv.BedrockAddr, "BSaved", uuid.New())
	require.NoError(t, err)
	defer b.Close()
	startBot(t, srv, "JSaved")
	waitOnGround(t, srv, "JSaved")

	var bid uuid.UUID
	onBedrock(t, srv, "BSaved", func(p *player.Player) { bid = p.UUID(); p.SetFood(6) })
	onJava(t, srv, "JSaved", func(p *player.Player) { p.SetFood(8) })
	jid := offlineUUID("JSaved")
	hunger := func(path string) float64 {
		m := readRecord(t, path)
		if m == nil {
			return -1
		}
		f, _ := m["Hunger"].(float64)
		return f
	}
	// One period to come round and one for the write: within 2 s of the change.
	require.Eventually(t, func() bool { return hunger(record(srv, "bedrock", bid)) == 6 }, 2*time.Second, 50*time.Millisecond,
		"the Bedrock player's change was not on disk within two periods")
	require.Eventually(t, func() bool { return hunger(record(srv, "java", jid)) == 8 }, 2*time.Second, 50*time.Millisecond,
		"the Java player's change was not on disk within two periods")
	require.True(t, srv.JavaOnline("JSaved"), "the save was not of a player online")
	left, _ := filepath.Glob(filepath.Join(srv.PlayersFolder, "*", "*.tmp"))
	require.Empty(t, left, "a temp file was left behind")
}

// One account UUID is one player across editions: while it is online on one,
// the other refuses it with a clear message, and two joins at once never both
// get in.
func TestSameAccountOnBothEditions(t *testing.T) {
	t.Run("one after the other", func(t *testing.T) {
		srv := testserver.Start(t)
		j := startBot(t, srv, "JTwin")
		waitOnGround(t, srv, "JTwin")
		_, err := bedrock.JoinAs(srv.BedrockAddr, "BTwin", offlineUUID("JTwin"))
		require.ErrorContains(t, err, "other edition")
		j.quit()
		require.Eventually(t, func() bool { return !srv.JavaOnline("JTwin") }, 10*time.Second, 50*time.Millisecond)

		b, err := bedrock.JoinAs(srv.BedrockAddr, "BTwin", offlineUUID("JTwin"))
		require.NoError(t, err)
		defer b.Close()
		require.Eventually(t, func() bool { return srv.PlayerByName("BTwin") }, 15*time.Second, 50*time.Millisecond)
		require.Contains(t, startBotRaw(t, srv, "JTwin").waitKicked(t), "other edition")
	})
	t.Run("at once", func(t *testing.T) {
		for round := 0; round < 3; round++ {
			srv := testserver.Start(t)
			name := "JAtOnce" + string(rune('0'+round))
			var wg sync.WaitGroup
			wg.Add(1)
			var b *bedrock.Bot
			go func() {
				defer wg.Done()
				b, _ = bedrock.JoinAs(srv.BedrockAddr, "BAtOnce", offlineUUID(name))
			}()
			j := startBotRaw(t, srv, name)
			wg.Wait()
			if b != nil {
				defer b.Close()
			}
			time.Sleep(3 * time.Second)
			both := srv.JavaOnline(name) && srv.PlayerByName("BAtOnce")
			require.False(t, both, "round %d: one account got in on both editions", round)
			require.Equal(t, 1, srv.Online()+boolInt(!srv.JavaOnline(name) && !srv.PlayerByName("BAtOnce")),
				"round %d: online %d", round, srv.Online())
			j.kill()
		}
	})
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Bedrock chat reaches Java without what Java players may not send, and
// reaches Bedrock as it was.
func TestBedrockChatIsCleanForJava(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BRaw")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JClean")
	waitOnGround(t, srv, "JClean")

	b.Say("a\nb§cc\td\r")
	require.Eventually(t, func() bool { return playerChat(t, j, "BRaw", "abccd") }, 10*time.Second, 50*time.Millisecond, "%+v", j.chat())
	require.Eventually(t, func() bool { return bedrockChat(b, "BRaw", "a\nb§cc\td\r") }, 10*time.Second, 50*time.Millisecond,
		"Bedrock's own copy was changed: %+v", b.Texts())
	for _, l := range j.chat() {
		require.False(t, strings.ContainsAny(plain(l.Msg).Text, "\n\r\t§"), "Java got %q", plain(l.Msg).Text)
	}
}
