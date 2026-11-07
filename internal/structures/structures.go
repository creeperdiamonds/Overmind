// Package structures places the structures of the Overworld, the Nether (fortresses, bastions) and the
// End (end cities) and builds their pieces (Task 9), in our own Go from the
// study of BDS 1.26.60.6 (NOTES-bedrock-structures.md, NOTES-structures-phase2.md, outside the repo).
// Templates and loot tables come from the local Bedrock install at start-up (Data); nothing of
// Mojang's is in this repo. See NOTES.md for what is built and what is not.
package structures

import (
	"encoding/json"
	"sync"

	"github.com/df-mc/dragonfly/server/world"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// Structures are the structures of one world: a pure function of the seed, the generator and the
// install's data, safe on any number of workers.
type Structures struct {
	g      *worldgen.Generator
	bd     *worldgen.BedrockData
	data   *Data
	seed   int64
	seed32 uint32
	// mineA, mineB are the mineshaft's two seed words: nextInt() twice of MT(levelSeed32) (N1 2.3).
	mineA, mineB int32

	villageBiomes map[int]bool
	villageTypes  map[int]string
	// jigsawFilters are the data-driven jigsaw structures' biome filters, by identifier (N1 2.4).
	jigsawFilters  map[string]json.RawMessage
	campStructures []string
	biomeNames     map[int]string

	ringOnce   sync.Once
	ringChunks [][2]int

	assembled   assemblyCache
	mineshafts  keyCache
	strongholds keyCache
	monuments   keyCache
	fortresses  keyCache
	endCities   keyCache
	mansions    keyCache
	portals     keyCache
	oceanRuins  keyCache
	// nether says these are the Nether's structures (fortresses and bastions, nether.go); end, the
	// End's (end cities, endcity.go).
	nether, end bool
	// omF1, omF2 are the monument start seed's two floats (monument.go).
	omOnce     sync.Once
	omF1, omF2 float32
	// trace, when set (tests only), sees every block a structure writes.
	trace func(x, y, z int, rid uint32)
	// spiderDone caches whether a spider corridor placed its spawner in a chunk (mineshaft.go).
	spiderMu   sync.Mutex
	spiderDone map[spiderKey]bool
}

// New makes the structures of a generator's world. The generator must have Bedrock's feature data (its
// biome files name the village biomes and the jigsaw filters); data is the install's templates and loot
// tables, nil to place starts only. Given a world's Nether or End generator, they are that dimension's.
func New(g *worldgen.Generator, data *Data) *Structures {
	s := &Structures{g: g, bd: g.Data(), data: data, seed: g.Seed(), seed32: g.BedrockLevelSeed(), nether: g.IsNether(), end: g.IsEnd(),
		villageBiomes: map[int]bool{}, villageTypes: map[int]string{}, jigsawFilters: map[string]json.RawMessage{},
		biomeNames: map[int]string{}}
	r := worldgen.NewBedrockRandom(s.seed32)
	s.mineA, s.mineB = r.NextInt(), r.NextInt()
	for _, b := range world.Biomes() {
		s.biomeNames[b.EncodeBiome()] = b.String()
	}
	if s.bd != nil {
		for _, name := range s.bd.BiomeNames() {
			raw := s.bd.BiomeComponent(name, "minecraft:village_type")
			if raw == nil {
				continue
			}
			var vt struct {
				Type string `json:"type"`
			}
			json.Unmarshal(raw, &vt)
			if b, ok := world.BiomeByName(name); ok {
				s.villageBiomes[b.EncodeBiome()] = true
				s.villageTypes[b.EncodeBiome()] = vt.Type
			}
		}
	}
	if data != nil {
		s.jigsawFilters = data.jigsawFilters
		s.campStructures = data.campOrder
	}
	return s
}

func (s *Structures) biomeName(id int) string { return s.biomeNames[id] }
