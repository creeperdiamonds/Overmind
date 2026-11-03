package worldgen

import (
	"os"
	"testing"
)

// The heavy sweeps run only with OVERMIND_LONG=1 (owner's ruling 29), so the default go test ./...
// stays under 10 minutes. Each test that needs it says so where it calls skipUnlessLong.

func longTests() bool { return os.Getenv("OVERMIND_LONG") != "" }

func skipUnlessLong(t *testing.T) {
	t.Helper()
	if !longTests() {
		t.Skip("a heavy sweep: set OVERMIND_LONG=1 to run it")
	}
}
