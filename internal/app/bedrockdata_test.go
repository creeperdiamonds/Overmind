package app

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/creeperdiamonds/overmind/internal/worldgen"
)

// TestBedrockDataFallsBack checks that a missing or unreadable install never stops the start: it logs
// one line saying features are approximate and gives no data.
func TestBedrockDataFallsBack(t *testing.T) {
	for _, dir := range []string{"", filepath.Join(t.TempDir(), "no-such-install")} {
		var buf bytes.Buffer
		log := slog.New(slog.NewTextHandler(&buf, nil))
		if bd := bedrockData(log, dir, worldgen.ProfileBedrock); bd != nil {
			t.Fatalf("%q: got data", dir)
		}
		if out := buf.String(); strings.Count(out, "\n") != 1 || !strings.Contains(out, "approximate") {
			t.Errorf("%q: log %q, want one line saying features are approximate", dir, out)
		}
	}
	if bd := bedrockData(slog.Default(), "x", worldgen.ProfileOverjava); bd != nil {
		t.Error("Overjava got Bedrock data")
	}
}
