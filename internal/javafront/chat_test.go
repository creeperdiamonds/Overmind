package javafront_test

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/stretchr/testify/require"

	"github.com/creeperdiamonds/overmind/bots/bedrock"
	"github.com/creeperdiamonds/overmind/internal/tables/java261"
	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// chatTypeIndex is minecraft:chat's place in the chat_type registry the client
// was sent.
func chatTypeIndex(t *testing.T) int {
	t.Helper()
	for _, r := range java261.Registries() {
		if r.ID != "minecraft:chat_type" {
			continue
		}
		for i, e := range r.Entries {
			if e == "minecraft:chat" {
				return i
			}
		}
	}
	t.Fatal("no minecraft:chat in the chat_type registry")
	return -1
}

// chatTypeOf reads the chat type minecraft-protocol parsed from a registry
// reference: the registry index, however the parser wraps it.
func chatTypeOf(raw json.RawMessage) int {
	var n int
	if json.Unmarshal(raw, &n) == nil {
		return n
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) == nil {
		for _, v := range m {
			if json.Unmarshal(v, &n) == nil {
				return n
			}
		}
	}
	return -1
}

// playerChat finds a player's chat message the Java bot was shown, of the
// minecraft:chat type.
func playerChat(t *testing.T, b *javaBot, sender, text string) bool {
	want := chatTypeIndex(t)
	for _, l := range b.chat() {
		if l.Kind == "disguised" && plain(l.Msg).Text == text && plain(l.Name).Text == sender && chatTypeOf(l.Type) == want {
			return true
		}
	}
	return false
}

// bedrockChat finds a chat line a Bedrock bot was shown, as Dragonfly formats
// chat for Bedrock.
func bedrockChat(b *bedrock.Bot, sender, text string) bool {
	for _, m := range b.Texts() {
		if !m.Translation && m.Message == "<"+sender+"> "+text {
			return true
		}
	}
	return false
}

// Chat reaches everyone, each edition in its own format, with non-ASCII text
// whole.
func TestChatBothWays(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BTalker")
	require.NoError(t, err)
	defer b.Close()
	j := startBot(t, srv, "JTalker")
	waitOnGround(t, srv, "JTalker")
	other := startBot(t, srv, "JHears")
	waitOnGround(t, srv, "JHears")

	// Bedrock to Java: unsigned chat of the minecraft:chat type, from the sender.
	b.Say("héllo ✓ from bedrock")
	require.Eventually(t, func() bool { return playerChat(t, j, "BTalker", "héllo ✓ from bedrock") }, 10*time.Second, 50*time.Millisecond,
		"Java never got the Bedrock chat: %+v", j.chat())
	require.Eventually(t, func() bool { return bedrockChat(b, "BTalker", "héllo ✓ from bedrock") }, 10*time.Second, 50*time.Millisecond,
		"the Bedrock sender did not see its own message")

	// Java to Bedrock and to Java, the sender included.
	j.say("héllo ✓ from java")
	require.Eventually(t, func() bool { return bedrockChat(b, "JTalker", "héllo ✓ from java") }, 10*time.Second, 50*time.Millisecond,
		"Bedrock never got the Java chat: %+v", b.Texts())
	require.Eventually(t, func() bool { return playerChat(t, other, "JTalker", "héllo ✓ from java") }, 10*time.Second, 50*time.Millisecond)
	require.Eventually(t, func() bool { return playerChat(t, j, "JTalker", "héllo ✓ from java") }, 10*time.Second, 50*time.Millisecond,
		"the Java sender did not see its own message")

	// Past the Basic Multilingual Plane. The bot reads NBT as plain UTF-8, so
	// only Bedrock can check this whole; javaproto's tests check the bytes Java
	// gets.
	j.say("emoji 😀 ok")
	require.Eventually(t, func() bool { return bedrockChat(b, "JTalker", "emoji 😀 ok") }, 10*time.Second, 50*time.Millisecond, "%+v", b.Texts())

	// Quotes and JSON from Bedrock are text, nothing more. A section sign is
	// dropped on the way to Java (TestBedrockChatIsCleanForJava).
	tricky := `"quoted" {"text":"x"} §cred \n`
	b.Say(tricky)
	require.Eventually(t, func() bool { return playerChat(t, j, "BTalker", strings.ReplaceAll(tricky, "§", "")) }, 10*time.Second, 50*time.Millisecond, "%+v", j.chat())
	require.False(t, j.ended())
}

// A Java player follows Java's chat rules: 256 characters at most, and none of
// the characters vanilla refuses. Either way it is disconnected, as vanilla does.
func TestJavaChatRules(t *testing.T) {
	srv := testserver.Start(t)
	b, err := bedrock.Join(srv.BedrockAddr, "BReads")
	require.NoError(t, err)
	defer b.Close()

	ok := startBot(t, srv, "JLong")
	waitOnGround(t, srv, "JLong")
	longest := strings.Repeat("é", 256)
	ok.say(longest)
	require.Eventually(t, func() bool { return bedrockChat(b, "JLong", longest) }, 10*time.Second, 50*time.Millisecond, "256 characters did not get through")

	for name, text := range map[string]string{
		"JTooLong": strings.Repeat("a", 257),
		"JSection": "§cred",
		"JControl": "bell\a",
	} {
		bot := startBot(t, srv, name)
		waitOnGround(t, srv, name)
		bot.say(text)
		require.Eventually(t, func() bool { return !srv.JavaOnline(name) }, 10*time.Second, 50*time.Millisecond, "%s stayed online", name)
		bot.clearErrors()
	}
	time.Sleep(300 * time.Millisecond)
	for _, m := range b.Texts() {
		require.NotContains(t, m.Message, "§cred", "a refused message got through")
		require.NotContains(t, m.Message, "aaaaaaaaaa", "a refused message got through")
	}
	require.True(t, srv.JavaOnline("JLong"))
}

var registerPing sync.Once

type pingCommand struct{}

func (pingCommand) Run(_ cmd.Source, o *cmd.Output, _ *world.Tx) { o.Print("pong") }

// Commands run as the player, and their output comes back as system chat.
func TestJavaCommands(t *testing.T) {
	registerPing.Do(func() { cmd.Register(cmd.New("javaping", "answers pong", nil, pingCommand{})) })
	srv := testserver.Start(t)
	j := startBot(t, srv, "JCommander")
	waitOnGround(t, srv, "JCommander")

	systemText := func(want string) bool {
		for _, l := range j.chat() {
			if l.Kind == "system" && strings.Contains(plain(l.Msg).flat(), want) {
				return true
			}
		}
		return false
	}
	j.command("javaping")
	require.Eventually(t, func() bool { return systemText("pong") }, 10*time.Second, 50*time.Millisecond, "%+v", j.chat())
	j.command("nosuchcommand at all")
	require.Eventually(t, func() bool { return systemText("nosuchcommand") }, 10*time.Second, 50*time.Millisecond, "no unknown-command message: %+v", j.chat())
	require.False(t, j.ended())
}
