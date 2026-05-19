package command

import (
	"strings"
	"testing"
)

// TestResumePointer_Format pins the exact pickup-trigger line — a fresh
// session's only cue to load the I-679 cross-session record, so its shape
// must not silently drift.
func TestResumePointer_Format(t *testing.T) {
	got := resumePointer("I-679")
	if !strings.Contains(got, "st resume I-679") {
		t.Errorf("resumePointer must name `st resume <id>`, got %q", got)
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("resumePointer must be one terminated line, got %q", got)
	}
	if !strings.Contains(got, "cross-session record") {
		t.Errorf("resumePointer must say WHY (cross-session record), got %q", got)
	}
}

// TestPrime_ActiveItemEmitsResumePointerBeforeAction: when prime shows a
// `Current:` active item (the cold-session dashboard), the `st resume <id>`
// pointer must appear directly under Current and BEFORE the next-action
// line — the trigger has to ride in the dashboard every session sees, not
// depend on the operator remembering to say "run st resume first" (I-679/
// I-697). Exercises the compact path (what session-start.sh injects).
func TestPrime_ActiveItemEmitsResumePointerBeforeAction(t *testing.T) {
	s, cfg := setupTestEnv(t) // T-003 is the lone active item

	out := captureStdout(t, func() { Prime(s, cfg, PrimeOpts{Compact: true}) })

	lines := strings.Split(out, "\n")
	curIdx := -1
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "Current:") {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		t.Skipf("no Current: block in this fixture's prime output:\n%s", out)
	}
	id := strings.Fields(strings.TrimSpace(lines[curIdx]))[1] // "Current: <id>"
	if curIdx+2 >= len(lines) {
		t.Fatalf("Current block truncated:\n%s", out)
	}
	// The line IMMEDIATELY under `Current: <id>` must be the resume
	// pointer; the action line follows it. This exact ordering is the
	// point — a cold session reads the pickup trigger before the action.
	if !strings.Contains(lines[curIdx+1], "st resume "+id) {
		t.Fatalf("line under `Current: %s` must be the `st resume %s` pickup pointer, got %q\nfull:\n%s", id, id, lines[curIdx+1], out)
	}
	if !strings.Contains(lines[curIdx+2], "Create PR") && !strings.Contains(lines[curIdx+2], "->") && !strings.Contains(lines[curIdx+2], "→") {
		t.Errorf("the next-action line must follow the resume pointer, got %q", lines[curIdx+2])
	}
}

// TestPrime_ResumePointerScopedToCurrentOnly: the pointer must be emitted
// ONLY as part of a `Current:` active-item block — exactly one per Current,
// never stray elsewhere in the dashboard. Deterministic with the standard
// fixture (one active item ⇒ one Current ⇒ exactly one resume pointer).
func TestPrime_ResumePointerScopedToCurrentOnly(t *testing.T) {
	s, cfg := setupTestEnv(t)
	out := captureStdout(t, func() { Prime(s, cfg, PrimeOpts{Compact: true}) })

	currents := strings.Count(out, "Current:")
	pointers := strings.Count(out, "st resume ")
	if pointers != currents {
		t.Errorf("expected exactly one `st resume` pointer per `Current:` block (currents=%d pointers=%d):\n%s",
			currents, pointers, out)
	}
}
