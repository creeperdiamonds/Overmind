package addon

import (
	"encoding/json"
	"fmt"
)

// PackType identifies what kind of content a pack provides.
type PackType string

const (
	PackTypeBehavior  PackType = "data"      // behaviour pack (mob AI, items, recipes)
	PackTypeResources PackType = "resources" // resource pack (textures, sounds, models)
	PackTypeUnknown   PackType = "unknown"
)

// Manifest is the parsed representation of a Bedrock pack's manifest.json.
type Manifest struct {
	FormatVersion int            `json:"format_version"`
	Header        ManifestHeader `json:"header"`
	Modules       []Module       `json:"modules"`
}

type ManifestHeader struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	UUID        string    `json:"uuid"`
	Version     [3]int    `json:"version"`
}

type Module struct {
	Type    string `json:"type"`
	UUID    string `json:"uuid"`
	Version [3]int `json:"version"`
}

// PackType returns the type of this pack based on the first module's type.
func (m *Manifest) PackType() PackType {
	if len(m.Modules) == 0 {
		return PackTypeUnknown
	}
	switch m.Modules[0].Type {
	case "data", "javascript":
		return PackTypeBehavior
	case "resources":
		return PackTypeResources
	default:
		return PackTypeUnknown
	}
}

// VersionString returns the header version as a "major.minor.patch" string.
func (m *Manifest) VersionString() string {
	v := m.Header.Version
	return fmt.Sprintf("%d.%d.%d", v[0], v[1], v[2])
}

func parseManifest(data []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}
