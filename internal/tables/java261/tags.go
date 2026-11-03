package java261

import (
	"strconv"
	"strings"
)

// Tag is one tag and the registry IDs it holds.
type Tag struct {
	Name string
	IDs  []int32
}

// TagRegistry is the tags of one registry.
type TagRegistry struct {
	ID   string
	Tags []Tag
}

// Tags returns the tags the client is sent in configuration, grouped by
// registry. IDs are the client's own: fixed for static registries, and
// positions in the Registries list for data-driven ones. The result is built on
// each call; callers that need it often keep what they encode from it.
func Tags() []TagRegistry {
	var out []TagRegistry
	for _, l := range lines("tags") {
		f := fields(l)
		if len(out) == 0 || out[len(out)-1].ID != f[0] {
			out = append(out, TagRegistry{ID: f[0]})
		}
		r := &out[len(out)-1]
		t := Tag{Name: f[1]}
		for _, s := range strings.Fields(f[2]) {
			n, err := strconv.Atoi(s)
			if err != nil {
				panic(err)
			}
			t.IDs = append(t.IDs, int32(n))
		}
		r.Tags = append(r.Tags, t)
	}
	return append(out, handTagRegistries()...)
}
