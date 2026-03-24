// Package addon loads Bedrock Edition addons (.mcaddon, .mcpack, plain directories)
// from the server's addon folder.
//
// An addon is a zip/directory that contains one or more packs, each identified
// by a manifest.json.  The Loader discovers packs, parses their manifests, and
// returns a flat list of [Addon] values ready for the server to apply.
package addon

// Addon represents a single loaded Bedrock pack (behaviour or resource).
type Addon struct {
	// Name is the human-readable pack name from manifest.json > header > name.
	Name string
	// UUID is the pack's unique identifier.
	UUID string
	// Version is the semver-formatted version string (e.g. "1.0.0").
	Version string
	// Type is whether this is a behaviour pack or resource pack.
	Type PackType
	// Description is the optional description from the manifest header.
	Description string
	// SourcePath is the file system path from which this addon was loaded.
	SourcePath string
}
