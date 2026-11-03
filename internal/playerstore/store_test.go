package playerstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server"
	"github.com/df-mc/dragonfly/server/block"
	"github.com/df-mc/dragonfly/server/entity"
	"github.com/df-mc/dragonfly/server/item"
	"github.com/df-mc/dragonfly/server/item/inventory"
	"github.com/df-mc/dragonfly/server/player"
	"github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl64"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func testWorld(t *testing.T) *world.World {
	t.Helper()
	var conf server.Config
	_ = conf.New() // finalizes the registries, as a server does
	w := world.Config{Entities: entity.DefaultRegistry, Dim: world.Overworld}.New()
	t.Cleanup(func() { _ = w.Close() })
	return w
}

func sampleConfig(id uuid.UUID) player.Config {
	inv := inventory.New(36, nil)
	_ = inv.SetItem(4, item.NewStack(block.Dirt{}, 7))
	return player.Config{
		UUID: id, Name: "Saved", Position: mgl64.Vec3{1, 2, 3}, Health: 11, MaxHealth: 20, Food: 9,
		GameMode: world.GameModeSurvival, Inventory: inv, OffHand: inventory.New(1, nil),
		Armour: inventory.NewArmour(nil), EnderChestInventory: inventory.New(27, nil),
	}
}

// saved writes a good record for id and returns its JSON as a map to spoil.
func saved(t *testing.T, dir string, w *world.World, id uuid.UUID) map[string]any {
	t.Helper()
	s, err := Open(dir, nil)
	require.NoError(t, err)
	require.NoError(t, s.Save(id, sampleConfig(id), w))
	require.NoError(t, s.Close())
	b, err := os.ReadFile(filepath.Join(dir, id.String()+".json"))
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(b, &m))
	return m
}

func TestSaveAndLoad(t *testing.T) {
	w := testWorld(t)
	dir, id := t.TempDir(), uuid.New()
	saved(t, dir, w, id)
	s, err := Open(dir, nil)
	require.NoError(t, err)
	defer s.Close()
	conf, from, err := s.Load(id, func(world.Dimension) *world.World { return w })
	require.NoError(t, err)
	require.Equal(t, w, from)
	require.Equal(t, 9, conf.Food)
	dirt, _ := conf.Inventory.Item(4)
	require.Equal(t, 7, dirt.Count())
	_, _, err = s.Load(uuid.New(), func(world.Dimension) *world.World { return w })
	require.ErrorIs(t, err, ErrNone)
	left, _ := filepath.Glob(filepath.Join(dir, "*.tmp"))
	require.Empty(t, left, "a temp file was left behind")
}

// A save waiting to be written is what Load gives, not the older file.
func TestLoadSeesPendingSave(t *testing.T) {
	w := testWorld(t)
	dir, id := t.TempDir(), uuid.New()
	saved(t, dir, w, id)
	s, err := Open(dir, nil)
	require.NoError(t, err)
	defer s.Close()
	c := sampleConfig(id)
	c.Food = 3
	require.NoError(t, s.Save(id, c, w))
	conf, _, err := s.Load(id, func(world.Dimension) *world.World { return w })
	require.NoError(t, err)
	require.Equal(t, 3, conf.Food)
}

// Each kind of bad record is refused without a panic, moved aside, and the
// account then has no data.
func TestCorruptRecordsAreMovedAside(t *testing.T) {
	w := testWorld(t)
	cases := map[string]func(m map[string]any) []byte{
		"bad uuid": func(m map[string]any) []byte {
			m["UUID"] = "not-a-uuid"
			b, _ := json.Marshal(m)
			return b
		},
		"slot out of range": func(m map[string]any) []byte {
			inv := m["Inventory"].(map[string]any)
			items := inv["Items"].([]any)
			items[0].(map[string]any)["Slot"] = 99
			b, _ := json.Marshal(m)
			return b
		},
		"truncated": func(m map[string]any) []byte {
			b, _ := json.Marshal(m)
			return b[:len(b)/2]
		},
	}
	for name, spoil := range cases {
		t.Run(name, func(t *testing.T) {
			dir, id := t.TempDir(), uuid.New()
			m := saved(t, dir, w, id)
			path := filepath.Join(dir, id.String()+".json")
			require.NoError(t, os.WriteFile(path, spoil(m), 0o644))

			s, err := Open(dir, nil)
			require.NoError(t, err)
			defer s.Close()
			_, _, err = s.Load(id, func(world.Dimension) *world.World { return w })
			require.Error(t, err)
			require.False(t, errors.Is(err, ErrNone))
			_, statErr := os.Stat(path)
			require.True(t, os.IsNotExist(statErr), "the bad record stayed in place")
			moved, _ := filepath.Glob(filepath.Join(dir, id.String()+".json.*.corrupt"))
			require.Len(t, moved, 1)
			require.True(t, strings.HasSuffix(moved[0], ".corrupt"))
			_, _, err = s.Load(id, func(world.Dimension) *world.World { return w })
			require.ErrorIs(t, err, ErrNone, "after moving it aside the account starts fresh")
		})
	}
}
