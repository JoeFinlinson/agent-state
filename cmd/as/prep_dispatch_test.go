package main

import (
	"strings"
	"testing"
)

// TestPrepDispatchStandaloneWhenSprintless verifies that the cobra
// handler for `st prep <id>` routes to PrepStandalone when the item
// has no sprint assigned, instead of erroring with the legacy
// "item X has no sprint assigned" message. I-571.
//
// Drives the call in --dry-run mode so no claude subprocess is spawned;
// the assertion is that exit is 0 and stdout shows the "Would plan"
// announcement that only PrepStandalone emits.
func TestPrepDispatchStandaloneWhenSprintless(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	stdout, code := runInProcess(t, ws, "prep", "--dry-run", "T-001")

	if code != 0 {
		t.Fatalf("prep --dry-run T-001 exit=%d, want 0\nstdout:\n%s", code, stdout)
	}
	if strings.Contains(stdout, "has no sprint assigned") {
		t.Errorf("standalone dispatch should not emit legacy 'has no sprint assigned' error:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Would plan") {
		t.Errorf("expected 'Would plan' in dry-run stdout, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "T-001") {
		t.Errorf("expected target item ID in dry-run stdout, got:\n%s", stdout)
	}
}

// TestPrepDispatchStandaloneViaItemFlag is the --item flag-form sibling
// of the test above. Same assertions; the routing path is different
// (the `else if item != ""` branch in the handler).
func TestPrepDispatchStandaloneViaItemFlag(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	stdout, code := runInProcess(t, ws, "prep", "--dry-run", "--item", "T-001")

	if code != 0 {
		t.Fatalf("prep --dry-run --item T-001 exit=%d, want 0\nstdout:\n%s", code, stdout)
	}
	if strings.Contains(stdout, "has no sprint assigned") {
		t.Errorf("--item dispatch should not emit legacy 'has no sprint assigned' error:\n%s", stdout)
	}
	if !strings.Contains(stdout, "Would plan") {
		t.Errorf("expected 'Would plan' in dry-run stdout, got:\n%s", stdout)
	}
}
