package java261

import (
	"strings"
	"sync"
)

var (
	bedrockEntityOnce sync.Once
	bedrockEntities   map[string]string
)

// JavaEntityOf is the Java entity type of a Bedrock entity name, with or
// without the minecraft: prefix: its Java name and ID (EntityType), or -1.
// The pairs are data/bedrockentities.gz, from minecraft-data's Bedrock
// 1.26.60 entity, language and spawn egg names (tools/gentables
// bedrockEntities): the same name, the same name less _projectile, else the
// Java entity of the same display name ("xp_orb" is experience_orb).
// Three are written here, having neither:
//   - Bedrock's xp_bottle (its display name in the data is its name) is Java's
//     experience_bottle, the thrown bottle o' enchanting (minecraft-data's pc
//     display name "Thrown Bottle o' Enchanting").
//   - Bedrock's boat and chest_boat ("Boat", "Boat with Chest") are one entity
//     for every wood, which Java splits into oak_boat, spruce_boat and the rest
//     ("Oak Boat", "Oak Boat with Chest"). The pair gives the oak one; the wood
//     has to come from the Bedrock boat's variant once boats exist.
func JavaEntityOf(bedrockName string) (name string, id int32) {
	bedrockEntityOnce.Do(func() {
		bedrockEntities = map[string]string{"xp_bottle": "experience_bottle", "boat": "oak_boat", "chest_boat": "oak_chest_boat"}
		for _, l := range lines("bedrockentities") {
			f := fields(l)
			bedrockEntities[f[0]] = f[1]
		}
	})
	n, ok := bedrockEntities[strings.TrimPrefix(bedrockName, prefix)]
	if !ok {
		return "", -1
	}
	return n, EntityType(n)
}
