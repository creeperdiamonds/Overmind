package addon

import (
	"archive/zip"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Loader scans a directory for addons and returns parsed [Addon] values.
type Loader struct {
	log *slog.Logger
}

// NewLoader creates a Loader that uses l for diagnostic output.
func NewLoader(l *slog.Logger) *Loader {
	return &Loader{log: l}
}

// LoadDir scans dir for:
//   - *.mcaddon  — zip containing one or more pack sub-directories
//   - *.mcpack   — zip containing a single pack
//   - plain directories that contain a manifest.json at their root
//
// It returns all successfully parsed addons and logs any errors without
// stopping iteration.
func (ld *Loader) LoadDir(dir string) ([]*Addon, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create addon dir %s: %w", dir, err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read addon dir %s: %w", dir, err)
	}

	var addons []*Addon
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())

		var loaded []*Addon
		var loadErr error

		switch {
		case !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mcaddon"):
			loaded, loadErr = ld.loadZip(path, true)
		case !entry.IsDir() && strings.HasSuffix(entry.Name(), ".mcpack"):
			loaded, loadErr = ld.loadZip(path, false)
		case entry.IsDir():
			a, e := ld.loadDir(path)
			if a != nil {
				loaded = []*Addon{a}
			}
			loadErr = e
		}

		if loadErr != nil {
			ld.log.Warn("addon load error", "path", path, "err", loadErr)
			continue
		}
		for _, a := range loaded {
			ld.log.Info("Addon loaded", "name", a.Name, "type", a.Type, "version", a.Version)
			addons = append(addons, a)
		}
	}

	return addons, nil
}

// loadZip opens a zip file.  If multi is true (mcaddon), each top-level
// sub-directory that contains a manifest.json is treated as a separate pack.
// If false (mcpack), the zip root itself is the pack.
func (ld *Loader) loadZip(path string, multi bool) ([]*Addon, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, fmt.Errorf("open zip %s: %w", path, err)
	}
	defer r.Close()

	// Build a map of path → zip.File for quick lookup.
	files := make(map[string]*zip.File, len(r.File))
	for _, f := range r.File {
		files[f.Name] = f
	}

	if !multi {
		// Single pack: manifest.json is at the root of the zip.
		a, err := ld.parseManifestFromZip(files, "manifest.json", path)
		if err != nil {
			return nil, err
		}
		return []*Addon{a}, nil
	}

	// Multi-pack (.mcaddon): find all manifest.json files one level deep.
	var addons []*Addon
	for name, f := range files {
		parts := strings.Split(name, "/")
		if len(parts) == 2 && parts[1] == "manifest.json" && !f.FileInfo().IsDir() {
			a, err := ld.parseManifestFromZip(files, name, path)
			if err != nil {
				ld.log.Warn("manifest parse error in mcaddon", "file", name, "err", err)
				continue
			}
			addons = append(addons, a)
		}
	}
	return addons, nil
}

func (ld *Loader) parseManifestFromZip(files map[string]*zip.File, manifestPath, sourcePath string) (*Addon, error) {
	f, ok := files[manifestPath]
	if !ok {
		return nil, fmt.Errorf("no manifest.json at %s", manifestPath)
	}
	data, err := readZipFile(f)
	if err != nil {
		return nil, fmt.Errorf("read manifest %s: %w", manifestPath, err)
	}
	return addonFromManifestData(data, sourcePath)
}

// loadDir loads a pack from a plain directory that contains manifest.json.
func (ld *Loader) loadDir(dir string) (*Addon, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // not a pack directory — silently skip
		}
		return nil, fmt.Errorf("read %s: %w", manifestPath, err)
	}
	return addonFromManifestData(data, dir)
}

func addonFromManifestData(data []byte, sourcePath string) (*Addon, error) {
	m, err := parseManifest(data)
	if err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &Addon{
		Name:        m.Header.Name,
		UUID:        m.Header.UUID,
		Version:     m.VersionString(),
		Type:        m.PackType(),
		Description: m.Header.Description,
		SourcePath:  sourcePath,
	}, nil
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}
