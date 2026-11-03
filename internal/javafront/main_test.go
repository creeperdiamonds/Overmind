package javafront_test

import (
	"os"
	"testing"

	"github.com/creeperdiamonds/overmind/internal/testserver"
)

// The front end's tests build at fixed heights on Dragonfly's flat world (the
// ground's top at y -61), as they did before worldgen; they test the front end,
// not the terrain.
func TestMain(m *testing.M) {
	testserver.FlatWorld = true
	os.Exit(m.Run())
}
