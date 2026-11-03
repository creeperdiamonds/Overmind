package player

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
)

// A Bedrock session must keep working as a Reporter without any change to it.
var _ Reporter = (*session.Session)(nil)

// fakeReporter panics on anything it does not override, which proves the
// player only reaches it through the interface.
type fakeReporter struct {
	Reporter
	messages []string
}

func (f *fakeReporter) SendMessage(m string) { f.messages = append(f.messages, m) }
func (f *fakeReporter) HandleInventories(*world.Tx, session.Controllable, *inventory.Inventory, *inventory.Inventory, *inventory.Inventory, *inventory.Inventory, *inventory.Armour, *uint32) {
}

func TestPlayerReportsThroughReporter(t *testing.T) {
	f := &fakeReporter{}
	var conf Config
	conf.Session = f
	var data world.EntityData
	conf.Apply(&data)
	p := &Player{playerData: data.Data.(*playerData)}
	p.Message("hello")
	if len(f.messages) != 1 || f.messages[0] != "hello" {
		t.Fatalf("messages = %v", f.messages)
	}
	if p.session() != Reporter(f) {
		t.Fatal("session() should return the configured Reporter")
	}
}

func TestPlayerWithoutReporterUsesNop(t *testing.T) {
	var data world.EntityData
	Config{}.Apply(&data)
	p := &Player{playerData: data.Data.(*playerData)}
	if p.session() != session.Nop {
		t.Fatal("a player with no reporter should use session.Nop")
	}
}
