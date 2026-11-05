package structures

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/mcdb"
)

// BDS worlds kept as leveldb folders (the study's structure worlds: one folder per world with a
// sub-folder per run, "big", "strips", ...). OVERMIND_BDS_WORLDS lists the folders holding them,
// separated by ';'. Not in the repo.

func bdsWorldDir(t testing.TB, name string) string {
	for _, d := range strings.Split(os.Getenv("OVERMIND_BDS_WORLDS"), ";") {
		if d == "" {
			continue
		}
		if p := filepath.Join(d, name); dirExists(p) {
			return p
		}
	}
	t.Skipf("BDS world %s not found: set OVERMIND_BDS_WORLDS", name)
	return ""
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func copyTree(t testing.TB, from, to string) {
	err := filepath.Walk(from, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(from, p)
		dst := filepath.Join(to, rel)
		if info.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if info.Name() == "LOCK" || strings.HasPrefix(info.Name(), "level.dat") {
			return nil // A new level.dat is made: Dragonfly does not read BDS 1.26's.
		}
		if false {
			return nil
		}
		in, err := os.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		out, err := os.Create(dst)
		if err != nil {
			return err
		}
		defer out.Close()
		_, err = io.Copy(out, in)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// bdsRuns is a BDS world's runs read into the whole-world form: the first run's blocks, with every cell
// some run disagrees on masked, and the first run's block entities.
type bdsRuns struct {
	*bdsWorld
	bes map[[3]int]map[string]any
}

func loadBDSRuns(t testing.TB, world_ string, runs []string, cx0, cx1, cz0, cz1 int) *bdsRuns {
	dir := bdsWorldDir(t, world_)
	world.DefaultBlockRegistry.Finalize()
	w := &bdsRuns{bdsWorld: &bdsWorld{chunks: map[[2]int]*bdsChunk{}}, bes: map[[3]int]map[string]any{}}
	ids := map[string]uint16{}
	idOf := func(n string) uint16 {
		if v, ok := ids[n]; ok {
			return v
		}
		ids[n] = uint16(len(w.names))
		w.names = append(w.names, n)
		return ids[n]
	}
	for ri, run := range runs {
		tmp := t.TempDir()
		copyTree(t, filepath.Join(dir, run), tmp)
		db, err := mcdb.Open(tmp)
		if err != nil {
			t.Fatal(err)
		}
		for cx := cx0; cx <= cx1; cx++ {
			for cz := cz0; cz <= cz1; cz++ {
				col, err := db.LoadColumn(world.ChunkPos{int32(cx), int32(cz)}, world.Overworld)
				if err != nil || col == nil {
					continue
				}
				c := w.chunks[[2]int{cx, cz}]
				if ri == 0 {
					c = &bdsChunk{ids: make([]uint16, chunkCells), unstable: make([]byte, chunkCells/8)}
					w.chunks[[2]int{cx, cz}] = c
					for _, b := range col.BlockEntities {
						w.bes[b.Pos] = b.Data
					}
				} else if c == nil {
					continue
				}
				for y := minY; y <= maxY; y++ {
					for z := 0; z < 16; z++ {
						for x := 0; x < 16; x++ {
							b, _ := world.BlockByRuntimeID(col.Chunk.Block(uint8(x), int16(y), uint8(z), 0))
							n, _ := b.EncodeBlock()
							id := idOf(strings.TrimPrefix(n, "minecraft:"))
							cell := cellOf(x, y, z)
							if ri == 0 {
								c.ids[cell] = id
							} else if c.ids[cell] != id {
								c.unstable[cell>>3] |= 1 << (cell & 7)
							}
						}
					}
				}
			}
		}
		db.Close()
	}
	return w
}
