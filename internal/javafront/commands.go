package javafront

import (
	"errors"

	"github.com/df-mc/dragonfly/server/cmd"
	"github.com/df-mc/dragonfly/server/player"
	"golang.org/x/text/language"

	"github.com/creeperdiamonds/overmind/internal/javaproto"
)

// sendCommands sends the client its command tree: /give for a player allowed
// to run it (an operator), and for anyone else the root alone, so the client
// suggests nothing. Dragonfly's other commands are not in the tree; a Java
// client sends a command it does not know all the same.
func (c *conn) sendCommands(p *player.Player) {
	nodes := []javaproto.CommandNode{{Flags: javaproto.NodeRoot}}
	if g, ok := cmd.ByAlias("give"); ok && len(g.Runnables(p)) > 0 {
		nodes[0].Children = append(nodes[0].Children, int32(len(nodes)))
		nodes = append(nodes, giveNodes(int32(len(nodes)))...)
	}
	c.send(javaproto.DeclareCommands(nodes, 0))
}

// giveNodes is vanilla's /give tree, its first node at index at: give
// <targets> <item> [<count>], with the targets players only, the item an
// item_stack (which the client completes, components too) and the count at
// least 1.
func giveNodes(at int32) []javaproto.CommandNode {
	return []javaproto.CommandNode{
		{Flags: javaproto.NodeLiteral | javaproto.NodeRestricted, Children: []int32{at + 1}, Name: "give"},
		{Flags: javaproto.NodeArgument, Children: []int32{at + 2}, Name: "targets", Parser: javaproto.ParserEntity, Props: []byte{javaproto.EntityPlayersOnly}},
		{Flags: javaproto.NodeArgument | javaproto.NodeExecutable, Children: []int32{at + 3}, Name: "item", Parser: javaproto.ParserItemStack},
		{Flags: javaproto.NodeArgument | javaproto.NodeExecutable, Name: "count", Parser: javaproto.ParserInteger, Props: javaproto.IntegerMin(1)},
	}
}

// javaTexter is a command message with a Java form of its own, such as /give's
// reply as Java's translation.
type javaTexter interface{ JavaText() javaproto.Text }

// SendCommandOutput shows what a command said as system chat, errors in red.
// A command that does not exist, or that the player may not run, is answered
// as vanilla answers it: "Unknown or incomplete command, see below for error"
// and the command marked <--[HERE].
func (c *conn) SendCommandOutput(o *cmd.Output, _ language.Tag) {
	for _, m := range o.Messages() {
		if j, ok := m.(javaTexter); ok {
			c.send(javaproto.SystemChat(j.JavaText()))
			continue
		}
		c.send(javaproto.SystemChat(javaproto.Text{Text: m.String()}))
	}
	for _, e := range o.Errors() {
		if unknownCommand(e) {
			c.send(javaproto.SystemChat(javaproto.Text{Translate: "command.unknown.command", Color: "red"}))
			c.send(javaproto.SystemChat(javaproto.Text{Color: "gray", Extra: []javaproto.Text{
				{Text: c.lastCommand, Color: "red"},
				{Translate: "command.context.here", Color: "red"},
			}}))
			continue
		}
		c.send(javaproto.SystemChat(javaproto.Text{Text: e.Error(), Color: "red"}))
	}
}

// unknownCommand reports whether an error is Dragonfly's cmd.MessageUnknown,
// which it gives both for a command that does not exist and for one the source
// may not run.
func unknownCommand(err error) bool {
	var t interface{ Resolve(language.Tag) string }
	if !errors.As(err, &t) {
		return false
	}
	// A translation's key resolves the same whatever its arguments.
	return t.Resolve(language.English) == cmd.MessageUnknown.F("").Resolve(language.English)
}
