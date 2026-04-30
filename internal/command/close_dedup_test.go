package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfinlinson/agent-state/internal/testutil"
)

// TestClose_RemovesStaleDuplicate verifies the I-472 fix: when an item
// is closed and a stale duplicate file with the SAME basename exists in
// another type-dir (typical cause: peer-merged feature branch carrying
// the pre-archive copy), Close sweeps the duplicate so only the
// canonical archive copy remains.
func TestClose_RemovesStaleDuplicate(t *testing.T) {
	env := testutil.NewEnv(t)

	// Simulate the peer-merge resurrection: an archive/ duplicate with
	// the SAME basename as the canonical file already exists when
	// close runs. Close mutates tasks/T-003-active.md, renames it to
	// archive/T-003-active.md, and must remove the pre-existing
	// archive/T-003-active.md that the merge re-introduced.
	body := `id: T-003
type: task
status: active
created: 2026-03-25T12:00:00-06:00
last_touched: 2026-03-25T12:00:00-06:00

title: Active task
`
	stale := filepath.Join(env.Root, "archive", "T-003-active.md")
	if err := os.WriteFile(stale, []byte(body), 0644); err != nil {
		t.Fatalf("seed stale dup: %v", err)
	}
	env.Reload(t)

	code := Close(env.S, env.Cfg, "T-003", "done", CloseOpts{Force: true})
	if code != 0 {
		t.Fatalf("Close exit=%d", code)
	}

	// Walk the tree — exactly one T-003-*.md should remain, in archive/.
	var found []string
	for _, dir := range []string{"tasks", "issues", "archive"} {
		entries, err := os.ReadDir(filepath.Join(env.Root, dir))
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if name := e.Name(); name == "T-003.md" || (len(name) > 6 && name[:6] == "T-003-") {
				found = append(found, filepath.Join(dir, name))
			}
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 T-003 file post-close, got %d: %v", len(found), found)
	}
	if filepath.Dir(found[0]) != "archive" {
		t.Errorf("surviving file should be in archive/, got %s", found[0])
	}
}
