package java261

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// entityNames reads minecraft-data's entity names as the generator found them:
// Java 26.1's with their display names, Bedrock 1.26.60's (entities.json and
// language.json) with theirs, and the names of Bedrock's spawn eggs.
func entityNames(t *testing.T) (java map[string]string, bedrock map[string][]string, eggs []string) {
	raw, err := os.ReadFile("testdata/entitynames.json")
	require.NoError(t, err)
	var m struct {
		Java    map[string]string   `json:"java"`
		Bedrock map[string][]string `json:"bedrock"`
		Eggs    []string            `json:"eggs"`
	}
	require.NoError(t, json.Unmarshal(raw, &m))
	return m.Java, m.Bedrock, m.Eggs
}

// Every Java entity Bedrock has too, by minecraft-data, has a Bedrock name
// JavaEntityOf maps to it: Bedrock's entity of the same name, the same name
// with _projectile, the same display name (when only one Java entity has it),
// or a spawn egg of the same name. Every pair names a real Java entity.
func TestEntityPairsCompleteAgainstMinecraftData(t *testing.T) {
	java, bedrock, eggs := entityNames(t)
	byDisplay := map[string][]string{}
	for n, d := range java {
		byDisplay[d] = append(byDisplay[d], n)
	}
	shared := map[string]string{} // Java name -> a Bedrock name that is the same entity
	for b, ds := range bedrock {
		if _, ok := java[b]; ok {
			shared[b] = b
			continue
		}
		if s, ok := strings.CutSuffix(b, "_projectile"); ok && java[s] != "" {
			shared[s] = b
			continue
		}
		for _, d := range ds {
			if len(byDisplay[d]) == 1 {
				shared[byDisplay[d][0]] = b
				break
			}
		}
	}
	for _, e := range eggs {
		if _, ok := java[e]; ok && shared[e] == "" {
			shared[e] = e
		}
	}
	reached := map[string]bool{}
	for b := range bedrock {
		if n, id := JavaEntityOf(b); n != "" {
			require.GreaterOrEqual(t, id, int32(0), "%s pairs with %s, which Java does not have", b, n)
			reached[n] = true
		}
	}
	for _, e := range eggs {
		if n, _ := JavaEntityOf("minecraft:" + e); n != "" {
			reached[n] = true
		}
	}
	var missing []string
	for j, b := range shared {
		if !reached[j] {
			missing = append(missing, j+" (Bedrock "+b+")")
		}
	}
	sort.Strings(missing)
	t.Logf("%d Java entities shared with Bedrock", len(shared))
	require.Empty(t, missing, "shared entities JavaEntityOf does not reach")

	// The entities from The Wild Update on, each by its Bedrock name.
	for b, want := range map[string]string{
		"allay": "allay", "frog": "frog", "tadpole": "tadpole", "warden": "warden",
		"camel": "camel", "sniffer": "sniffer", "armadillo": "armadillo", "breeze": "breeze",
		"bogged": "bogged", "wind_charge_projectile": "wind_charge",
		"breeze_wind_charge_projectile": "breeze_wind_charge", "creaking": "creaking",
		"happy_ghast": "happy_ghast", "copper_golem": "copper_golem", "nautilus": "nautilus",
		"zombie_nautilus": "zombie_nautilus", "camel_husk": "camel_husk", "parched": "parched",
		"trader_llama": "trader_llama", "evocation_illager": "evoker", "villager_v2": "villager",
		"boat": "oak_boat", "chest_boat": "oak_chest_boat",
	} {
		n, id := JavaEntityOf("minecraft:" + b)
		require.Equal(t, want, n, b)
		require.GreaterOrEqual(t, id, int32(0), b)
	}
}
