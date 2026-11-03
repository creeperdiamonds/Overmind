package server

import (
	"testing"

	"github.com/df-mc/dragonfly/server/item/recipe"
)

// Overmind fork: vanilla data is registered once per process, however many
// servers it makes.
func TestVanillaDataRegisteredOnce(t *testing.T) {
	var conf Config
	first := conf.New()
	n := len(recipe.Recipes())
	second := conf.New()
	if got := len(recipe.Recipes()); got != n || n == 0 {
		t.Fatalf("recipes: %d after the first server, %d after the second", n, got)
	}
	_ = first.World().Close()
	_ = second.World().Close()
}
