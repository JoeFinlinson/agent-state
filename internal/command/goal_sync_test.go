package command

import (
	"testing"

	"github.com/theraprac/agent-state/internal/config"
	"github.com/theraprac/agent-state/internal/store"
)

// I-1715: goal.go historically called autoSync zero times across the entire
// goal lifecycle (create/activate/mark-met/drop). New per-entity files are
// invisible to `git add -u` (I-442), and mark-met/drop additionally rename
// the file goals/ -> archive/ (a delete-old + untracked-new from git's
// perspective), so a goal could sit fully unpersisted until an unrelated
// command happened to sync it — and a peer's `st reconcile` running in that
// window could silently drop the orphaned entry (observed live 2026-07-01,
// G-023). These tests assert each lifecycle command syncs the correct path
// itself, without relying on any later command to sweep it up.

// captureAutoSyncPaths swaps in a fake autoSyncGitFn that records every
// newPaths argument passed to it, restoring the original on test cleanup.
// Returns an accessor rather than a plain value because production code
// (GoalCreate, GoalActivate, ...) runs across several unwrapped statements
// after this returns and keeps appending to the recording.
func captureAutoSyncPaths(t *testing.T) func() [][]string {
	t.Helper()
	var calls [][]string
	orig := autoSyncGitFn
	t.Cleanup(func() { autoSyncGitFn = orig })
	autoSyncGitFn = func(_ *store.Store, _ string, newPaths ...string) error {
		calls = append(calls, append([]string(nil), newPaths...))
		return nil
	}
	return func() [][]string { return calls }
}

func pathsContain(calls [][]string, want string) bool {
	for _, paths := range calls {
		for _, p := range paths {
			if p == want {
				return true
			}
		}
	}
	return false
}

// setupActiveGoal creates and activates a goal with the given title, returning
// the reloaded store and the new goal's ID. Shared by the mark-met/drop tests,
// which both need an active goal before exercising the transition under test.
func setupActiveGoal(t *testing.T, title string) (s *store.Store, cfg *config.Config, id string) {
	t.Helper()
	_, s, cfg = newGoalEnv(t)
	if rc := GoalCreate(s, cfg, title, 10, GoalCreateOpts{SuccessCriterion: "done"}); rc != 0 {
		t.Fatalf("GoalCreate rc=%d", rc)
	}
	s = reloadStoreGoal(t, cfg)
	goals := s.List(store.TypeFilter("goal"))
	if len(goals) == 0 {
		t.Fatal("no goals after create")
	}
	id = goals[0].ID
	if rc := GoalActivate(s, cfg, id); rc != 0 {
		t.Fatalf("GoalActivate rc=%d", rc)
	}
	return s, cfg, id
}

func TestGoalCreateCommitsAndPushes(t *testing.T) {
	_, s, cfg := newGoalEnv(t)
	calls := captureAutoSyncPaths(t)

	rc := GoalCreate(s, cfg, "Sync Test Goal", 10, GoalCreateOpts{SuccessCriterion: "done when synced"})
	if rc != 0 {
		t.Fatalf("GoalCreate rc=%d", rc)
	}

	if len(calls()) == 0 {
		t.Fatal("GoalCreate never called autoSync — new goal file would sit untracked (I-1715 regression)")
	}

	s2 := reloadStoreGoal(t, cfg)
	goals := s2.List(store.TypeFilter("goal"))
	if len(goals) == 0 {
		t.Fatal("no goals after create")
	}
	newPath, ok := s.Path(goals[0].ID)
	if !ok {
		t.Fatalf("no path for created goal %s", goals[0].ID)
	}
	if !pathsContain(calls(), newPath) {
		t.Errorf("autoSync was called but never passed the new goal's path %q explicitly — git add -u cannot stage a new untracked file on its own (I-442)", newPath)
	}
}

func TestGoalActivateSyncs(t *testing.T) {
	_, s, cfg := newGoalEnv(t)
	rc := GoalCreate(s, cfg, "Activate Sync Goal", 10, GoalCreateOpts{SuccessCriterion: "done"})
	if rc != 0 {
		t.Fatalf("GoalCreate rc=%d", rc)
	}
	s2 := reloadStoreGoal(t, cfg)
	goals := s2.List(store.TypeFilter("goal"))
	if len(goals) == 0 {
		t.Fatal("no goals after create")
	}
	id := goals[0].ID

	calls := captureAutoSyncPaths(t)
	if rc := GoalActivate(s2, cfg, id); rc != 0 {
		t.Fatalf("GoalActivate rc=%d", rc)
	}
	if len(calls()) == 0 {
		t.Fatal("GoalActivate never called autoSync — status flip would sit unpersisted (I-1715 regression)")
	}
}

func TestGoalMarkMetSyncs(t *testing.T) {
	s2, cfg, id := setupActiveGoal(t, "MarkMet Sync Goal")

	calls := captureAutoSyncPaths(t)
	if rc := GoalMarkMet(s2, cfg, id, GoalMarkMetOpts{}); rc != 0 {
		t.Fatalf("GoalMarkMet rc=%d", rc)
	}
	if len(calls()) == 0 {
		t.Fatal("GoalMarkMet never called autoSync — the goals/ -> archive/ rename would leave the file untracked at its new path (I-1715 regression)")
	}
	newPath, ok := s2.Path(id)
	if !ok {
		t.Fatalf("no path for %s after mark-met", id)
	}
	if !pathsContain(calls(), newPath) {
		t.Errorf("autoSync was called but never passed the post-move archive path %q explicitly", newPath)
	}
}

func TestGoalDropSyncs(t *testing.T) {
	s2, cfg, id := setupActiveGoal(t, "Drop Sync Goal")

	calls := captureAutoSyncPaths(t)
	if rc := GoalDrop(s2, cfg, id, "superseded"); rc != 0 {
		t.Fatalf("GoalDrop rc=%d", rc)
	}
	if len(calls()) == 0 {
		t.Fatal("GoalDrop never called autoSync — the goals/ -> archive/ rename would leave the file untracked at its new path (I-1715 regression)")
	}
	newPath, ok := s2.Path(id)
	if !ok {
		t.Fatalf("no path for %s after drop", id)
	}
	if !pathsContain(calls(), newPath) {
		t.Errorf("autoSync was called but never passed the post-move archive path %q explicitly", newPath)
	}
}
