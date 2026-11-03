package playerdb

// Overmind fork: this whole file is new. It lets a store other than the LevelDB
// Provider keep player data in the same format.

import (
	"fmt"

	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
)

// Data is a player's saved data in the form this package marshals to JSON.
type Data = jsonData

// Snapshot copies the data of a player into a Data that no longer refers to
// the player's inventories, so it can be marshalled on another goroutine.
func Snapshot(conf player.Config, w *world.World) Data {
	return (&Provider{}).toJson(conf, w)
}

// Restore turns saved Data back into a player.Config and its world. Data that
// cannot be restored (a bad UUID, an inventory slot out of range) gives an
// error instead of a panic.
func Restore(d Data, lookup func(world.Dimension) *world.World) (conf player.Config, w *world.World, err error) {
	defer func() {
		if r := recover(); r != nil {
			conf, w, err = player.Config{}, nil, fmt.Errorf("playerdb: bad player data: %v", r)
		}
	}()
	conf, w = (&Provider{}).fromJson(d, lookup)
	return conf, w, nil
}
