package java261

import (
	"strings"
	"sync"
)

var (
	metadataOnce  sync.Once
	metadataKeys  map[[2]string]int32
	metadataTypes map[string]int32
)

func loadMetadata() {
	metadataKeys, metadataTypes = map[[2]string]int32{}, map[string]int32{}
	for _, l := range lines("metadata") {
		f := fields(l)
		switch f[0] {
		case "key":
			metadataKeys[[2]string{f[1], f[2]}] = int32(atoi(f[3]))
		case "type":
			metadataTypes[f[1]] = int32(atoi(f[2]))
		}
	}
}

// MetadataIndex is the index of an entity's metadata key ("health" of
// "player"), from minecraft-data's entities.json, or -1 when unknown. The entity
// name may carry the minecraft: prefix.
func MetadataIndex(entity, key string) int32 {
	metadataOnce.Do(loadMetadata)
	if i, ok := metadataKeys[[2]string{strings.TrimPrefix(entity, prefix), key}]; ok {
		return i
	}
	return -1
}

// MetadataType is the ID of a metadata value type ("byte", "item_stack"), from
// protocol.json's entityMetadataEntry, or -1 when unknown.
func MetadataType(name string) int32 {
	metadataOnce.Do(loadMetadata)
	if id, ok := metadataTypes[name]; ok {
		return id
	}
	return -1
}
