package worldgen

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestBedrockFirstOreBlobs checks Bedrock's seeds, scatter and ore shape against the study's golden
// chunks (golden-bedrock/seed<N>/chunk_*.json, NOTES-bedrock-exact section 10): the chunk seed, every
// listed rule's seed, the first placed blob's origin, and every position inside its spheres. The
// study found these exact (sections 2 to 4), so they are held to every value.
func TestBedrockFirstOreBlobs(t *testing.T) {
	dir := goldenBedrockDir(t)
	rows := map[string]undergroundRow{}
	for _, r := range undergroundRows {
		rows[r.ident] = r
	}
	for _, seed := range bedrockTestSeeds {
		files, _ := filepath.Glob(filepath.Join(dir, fmt.Sprintf("seed%d", seed), "chunk_*.json"))
		if len(files) == 0 {
			t.Fatalf("seed %d: no golden chunks", seed)
		}
		bs := newBedrockSeeds(seed)
		blobs, bad := 0, 0
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var c struct {
				CX        int               `json:"cx"`
				CZ        int               `json:"cz"`
				ChunkSeed uint32            `json:"chunk_seed"`
				RuleSeeds map[string]uint32 `json:"rule_seeds"`
				Blobs     map[string]struct {
					Origin [3]int   `json:"origin"`
					Blocks [][4]any `json:"blocks"`
				} `json:"ore_first_blob"`
			}
			if err := json.Unmarshal(raw, &c); err != nil {
				t.Fatal(err)
			}
			cx, cz := c.CX, c.CZ
			cs := bs.chunk(cx, cz)
			if cs != c.ChunkSeed {
				t.Fatalf("seed %d chunk %d,%d: chunk seed %d, want %d", seed, cx, cz, cs, c.ChunkSeed)
			}
			for ident, want := range c.RuleSeeds {
				if got := bedrockRuleSeed(cs, uint32(fnv1Hash(ident))); got != want {
					t.Fatalf("seed %d chunk %d,%d %s: rule seed %d, want %d", seed, cx, cz, ident, got, want)
				}
			}
			for ident, blob := range c.Blobs {
				row, ok := rows[ident]
				if !ok {
					t.Fatalf("%s: not in the underground table", ident)
				}
				rule := &bedrockRule{iterations: row.iterations, num: row.num, den: row.den, order: evalOrder(row.order), axes: row.axes}
				rs := bedrockRuleSeed(cs, uint32(fnv1Hash(ident)))
				r1, r2 := newBMT(rs), newBMT(rs)
				p, found := firstScatter(rule, r1, cx, cz)
				if !found || p != (pos{blob.Origin[0], blob.Origin[1], blob.Origin[2]}) {
					t.Errorf("seed %d chunk %d,%d %s: first blob at %v, want %v", seed, cx, cz, ident, p, blob.Origin)
					bad++
					continue
				}
				o := newBedrockOre(row.count, row.discard, row.replace)
				var all, act []oreSphere
				ours := map[[3]int]bool{}
				forBlobBlocks(o.spheres(r2, p, &all, &act), func(x, y, z int) { ours[[3]int{x, y, z}] = true })
				blobs++
				for _, b := range blob.Blocks {
					k := [3]int{int(b[0].(float64)), int(b[1].(float64)), int(b[2].(float64))}
					if !ours[k] {
						t.Errorf("seed %d chunk %d,%d %s: %v is in BDS's blob, not ours", seed, cx, cz, ident, k)
						bad++
						break
					}
				}
			}
		}
		t.Logf("seed %d: %d chunks, %d first blobs checked, %d wrong", seed, len(files), blobs, bad)
	}
}

// firstScatter gives a rule's first scatter position inside the world, as the golden files list it.
func firstScatter(rule *bedrockRule, r1 *bmt, cx, cz int) (pos, bool) {
	if rule.den > 0 && rule.num != rule.den && int(r1.nextIntN(int32(rule.den))) >= rule.num {
		return pos{}, false
	}
	for i := rule.iterations - 1; i >= 0; i-- {
		p := [3]int{cx * 16, 0, cz * 16}
		for _, a := range rule.order {
			p[a] += rule.axes[a].eval(r1)
		}
		if p[1] >= minY && p[1] <= maxY {
			return pos{p[0], p[1], p[2]}, true
		}
	}
	return pos{}, false
}
