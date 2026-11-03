package testserver

import (
	"strings"
	"testing"
)

// TestServerCapCounts checks the per-process server cap: starts are counted, and the one past the cap
// is refused with the reason, rather than leaving a later bot join to fail mysteriously.
func TestServerCapCounts(t *testing.T) {
	savedMade, savedCap := made.Load(), serverCap
	defer func() { made.Store(savedMade); serverCap = savedCap }()
	made.Store(0)
	serverCap = 2
	for i := range 2 {
		if err := claimServer(); err != nil {
			t.Fatalf("start %d refused under the cap: %v", i+1, err)
		}
	}
	err := claimServer()
	if err == nil || !strings.Contains(err.Error(), "creative items") {
		t.Fatalf("start past the cap: got %v, want a refusal that says why", err)
	}
	if got := Made(); got != 2 {
		t.Errorf("Made() = %d after a refused start, want 2", got)
	}
}
