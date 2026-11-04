package structures

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/df-mc/dragonfly/server/world"
	"github.com/df-mc/dragonfly/server/world/chunk"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Tests that need the install read it from OVERMIND_BEDROCK_DATA (a BDS behavior_packs folder) and skip
// without it; tests against BDS worlds also need OVERMIND_GOLDEN_BEDROCK (the study's golden-bedrock
// folder: structures/evidence.json, whole/*.bin.gz). Neither is in the repo.

var (
	dataOnce sync.Once
	testBD   *worldgen.BedrockData
	testData *Data
	dataErr  error
)

func installData(t testing.TB) (*worldgen.BedrockData, *Data) {
	dir := os.Getenv("OVERMIND_BEDROCK_DATA")
	if dir == "" {
		t.Skip("OVERMIND_BEDROCK_DATA is not set: point it at a Bedrock install's behavior_packs to test structures")
	}
	dataOnce.Do(func() {
		testBD, dataErr = worldgen.LoadBedrockData(dir)
		if dataErr == nil {
			testData, dataErr = LoadData(dir)
		}
	})
	if dataErr != nil {
		t.Fatal(dataErr)
	}
	return testBD, testData
}

// worldFor is a seed's Bedrock-profile generator with its structures attached.
func worldFor(t testing.TB, seed int64) (*worldgen.Generator, *Structures) {
	bd, d := installData(t)
	g := worldgen.NewWithData(seed, worldgen.ProfileBedrock, bd)
	s := New(g, d)
	g.SetStructures(s)
	return g, s
}

func goldenDir(t testing.TB) string {
	dir := os.Getenv("OVERMIND_GOLDEN_BEDROCK")
	if dir == "" {
		t.Skip("OVERMIND_GOLDEN_BEDROCK is not set: point it at the golden-bedrock folder to test against BDS")
	}
	return dir
}

// evidence is the study's scan of the four measured worlds (chunks -38..37): every block entity with a
// loot table, every spawner, and the structure volumes BDS stores (re/structures/scan_seed*.txt,
// aabb_4seeds.txt, gathered into one file outside the repo).
type evidence struct {
	Scanned [2]int `json:"scanned_chunks"`
	Seeds   map[string]struct {
		Loot     [][]any `json:"loot"`     // kind, x, y, z, table, seed
		Spawners [][]any `json:"spawners"` // x, y, z, mob
		Boxes    [][]any `json:"boxes"`    // structure, x0, y0, z0, x1, y1, z1
	} `json:"seeds"`
}

type lootEvidence struct {
	kind    string
	x, y, z int
	table   string
	seed    int32
}

func loadEvidence(t testing.TB) *evidence {
	raw, err := os.ReadFile(filepath.Join(goldenDir(t), "structures", "evidence.json"))
	if err != nil {
		t.Fatal(err)
	}
	var e evidence
	if err := json.Unmarshal(raw, &e); err != nil {
		t.Fatal(err)
	}
	return &e
}

func (e *evidence) loot(seed string) []lootEvidence {
	var out []lootEvidence
	for _, l := range e.Seeds[seed].Loot {
		out = append(out, lootEvidence{l[0].(string), int(l[1].(float64)), int(l[2].(float64)), int(l[3].(float64)), l[4].(string), int32(l[5].(float64))})
	}
	return out
}

// interior says whether a block is in the scanned area less its outer ring of chunks, which BDS loaded
// but did not decorate (a chunk is decorated once its neighbours are loaded).
func (e *evidence) interior(x, z int) bool {
	cx, cz := x>>4, z>>4
	return cx > e.Scanned[0] && cx < e.Scanned[1] && cz > e.Scanned[0] && cz < e.Scanned[1]
}

// generate makes a chunk with its block entities.
func generate(g *worldgen.Generator, cx, cz int) *chunk.Column {
	col := &chunk.Column{Chunk: chunk.New(world.DefaultBlockRegistry, world.Overworld.Range())}
	g.GenerateColumn(world.ChunkPos{int32(cx), int32(cz)}, col)
	return col
}

var testSeeds = []int64{100, 1, 12345, 987654321}
