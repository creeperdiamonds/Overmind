package player

import (
	"context"
	"testing"

	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/session"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
)

// halving is a Reporter whose players hit for half and never critically.
type halving struct {
	Reporter
	asked int
}

// Disconnect, Close and CloseConnection end the player as a front end would,
// when the world closes.
func (h *halving) Disconnect(string) {}
func (h *halving) CloseConnection()  {}
func (h *halving) Close(tx *world.Tx, c session.Controllable) {
	if tx != nil {
		tx.RemoveEntity(c)
	}
}

func (h *halving) AttackDamage(_ item.Stack, dmg float64, _ bool) (float64, bool) {
	h.asked++
	return dmg / 2, false
}

// attack has a player hit another bare-handed, the attacker reporting to rep,
// and returns the victim's health after.
func attack(t *testing.T, rep Reporter) float64 {
	t.Helper()
	w := world.Config{Entities: entity.DefaultRegistry, Generator: world.NopGenerator{}, Dim: world.Overworld}.New()
	t.Cleanup(func() { _ = w.Close() })
	spawn := func(tx *world.Tx, pos mgl64.Vec3, s Reporter) *Player {
		id := uuid.New()
		conf := Config{Name: id.String()[:8], UUID: id, Position: pos, GameMode: world.GameModeSurvival, Session: s}
		return tx.AddEntity(world.EntitySpawnOpts{Position: pos, ID: id}.New(Type, conf)).(*Player)
	}
	health, err := world.Call(context.Background(), w, func(tx *world.Tx) (float64, error) {
		a := spawn(tx, mgl64.Vec3{0.5, 100, 0.5}, rep)
		v := spawn(tx, mgl64.Vec3{1.5, 100, 0.5}, nil)
		if !a.AttackEntity(v) {
			t.Error("the attack did not land")
		}
		return v.Health(), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return health
}

// A Reporter with AttackRules decides the damage of its player's attacks.
func TestAttackRulesDecideDamage(t *testing.T) {
	h := &halving{Reporter: session.Nop}
	if got := attack(t, h); got != 19.5 {
		t.Errorf("victim health %v after a halved bare-handed hit, want 19.5", got)
	}
	if h.asked != 1 {
		t.Errorf("AttackDamage asked %d times, want 1", h.asked)
	}
}

// Without them Dragonfly's own damage stands: a bare hand does 1.
func TestNoAttackRulesKeepsDragonflyDamage(t *testing.T) {
	if got := attack(t, nil); got != 19 {
		t.Errorf("victim health %v after a bare-handed hit, want 19", got)
	}
}
