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
// The pairs are data/bedrockentities.gz: the same name, else the Java entity
// of the same display name in minecraft-data ("xp_orb" is experience_orb).
// Bedrock's xp_bottle is the one with neither (its display name in the data is
// its name); it is Java's experience_bottle, the thrown bottle o' enchanting
// (minecraft-data's pc display name "Thrown Bottle o' Enchanting").
func JavaEntityOf(bedrockName string) (name string, id int32) {
	bedrockEntityOnce.Do(func() {
		bedrockEntities = map[string]string{"xp_bottle": "experience_bottle"}
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
