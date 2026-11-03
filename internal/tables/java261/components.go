package java261

import (
	"strings"
	"sync"
)

var (
	componentOnce sync.Once
	components    map[string]int32
)

// ComponentType returns the ID an item component type is known by in an item
// slot, by its name with or without the minecraft: prefix (damage,
// custom_name), or -1 when unknown.
func ComponentType(name string) int32 {
	componentOnce.Do(func() {
		ls := lines("components")
		components = make(map[string]int32, len(ls))
		for _, l := range ls {
			f := fields(l)
			components[f[0]] = int32(atoi(f[1]))
		}
	})
	if id, ok := components[strings.TrimPrefix(name, prefix)]; ok {
		return id
	}
	return -1
}
