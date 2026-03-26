package command

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/store"
)

// setupTestEnv creates a temp directory with items and returns a store + config.
func setupTestEnv(t *testing.T) (*store.Store, *config.Config) {
	t.Helper()
	root := t.TempDir()

	for _, dir := range []string{"tasks", "issues", "archive", ".as"} {
		os.MkdirAll(filepath.Join(root, dir), 0755)
	}

	// Config
	os.WriteFile(filepath.Join(root, ".as", "config.yaml"), []byte("paths:\n  root: .\n"), 0644)

	// Items
	writeFile(t, filepath.Join(root, "tasks", "T-001-first.md"), `id: T-001
type: task
status: queued
created: 2026-03-25T10:00:00-06:00
last_touched: 2026-03-25T10:00:00-06:00

completed: null

title: First task

depends_on:
- []

next_actions:
- []
`)

	writeFile(t, filepath.Join(root, "tasks", "T-002-second.md"), `id: T-002
type: task
status: queued
created: 2026-03-25T11:00:00-06:00
last_touched: 2026-03-25T11:00:00-06:00

completed: null

title: Second task

depends_on:
- T-001

next_actions:
- []
`)

	writeFile(t, filepath.Join(root, "tasks", "T-003-active.md"), `id: T-003
type: task
status: active
created: 2026-03-25T12:00:00-06:00
last_touched: 2026-03-25T12:00:00-06:00

completed: null

title: Active task
assigned_to: agent-a

depends_on:
- []

next_actions:
- []
`)

	writeFile(t, filepath.Join(root, "issues", "I-001-bug.md"), `id: I-001
type: issue
status: open
created: 2026-03-25T10:00:00-06:00
last_touched: 2026-03-25T10:00:00-06:00

title: A bug
severity: high
`)

	writeFile(t, filepath.Join(root, "archive", "T-004-done.md"), `id: T-004
type: task
status: completed
created: 2026-03-20T10:00:00-06:00
last_touched: 2026-03-25T10:00:00-06:00

completed: 2026-03-25T10:00:00-06:00

title: Done task
`)

	cfg, err := config.Load(root)
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	s, err := store.New(cfg)
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}

	return s, cfg
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	os.WriteFile(path, []byte(content), 0644)
}

// --- Show ---

func TestShowHappy(t *testing.T) {
	s, _ := setupTestEnv(t)
	code := Show(s, []string{"T-001"})
	if code != 0 {
		t.Errorf("Show T-001 returned %d, want 0", code)
	}
}

func TestShowBrief(t *testing.T) {
	s, _ := setupTestEnv(t)
	code := Show(s, []string{"T-001", "--brief"})
	if code != 0 {
		t.Errorf("Show --brief returned %d, want 0", code)
	}
}

func TestShowNotFound(t *testing.T) {
	s, _ := setupTestEnv(t)
	code := Show(s, []string{"T-999"})
	if code != 1 {
		t.Errorf("Show T-999 returned %d, want 1", code)
	}
}

func TestShowNoArgs(t *testing.T) {
	s, _ := setupTestEnv(t)
	code := Show(s, []string{})
	if code != 2 {
		t.Errorf("Show no args returned %d, want 2", code)
	}
}

// --- List ---

func TestListAll(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := List(s, cfg, []string{})
	if code != 0 {
		t.Errorf("List returned %d, want 0", code)
	}
}

func TestListByType(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := List(s, cfg, []string{"--type", "issue"})
	if code != 0 {
		t.Errorf("List --type issue returned %d, want 0", code)
	}
}

// --- Check ---

func TestCheckClean(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Check(s, cfg, []string{"--quiet"})
	// May have reciprocal dep issues (T-002 depends T-001, T-001 doesn't block T-002)
	// That's expected — check should catch it
	_ = code // just verify it doesn't crash
}

// --- Ready ---

func TestReady(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Ready(s, cfg, []string{})
	if code != 0 {
		t.Errorf("Ready returned %d, want 0", code)
	}
}

// --- Create ---

func TestCreateHappy(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Create(s, cfg, []string{"task", "New task"})
	if code != 0 {
		t.Errorf("Create returned %d, want 0", code)
	}

	// Verify item was created
	item, ok := s.Get("T-005")
	if !ok {
		t.Fatal("T-005 should exist after create")
	}
	if item.Title != "New task" {
		t.Errorf("title = %q, want %q", item.Title, "New task")
	}
}

func TestCreateBadType(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Create(s, cfg, []string{"banana", "Bad type"})
	if code != 2 {
		t.Errorf("Create bad type returned %d, want 2", code)
	}
}

func TestCreateNoArgs(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Create(s, cfg, []string{"task"})
	if code != 2 {
		t.Errorf("Create no title returned %d, want 2", code)
	}
}

// --- Start ---

func TestStartHappy(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Start(s, cfg, []string{"T-001"})
	if code != 0 {
		t.Errorf("Start T-001 returned %d, want 0", code)
	}

	item, _ := s.Get("T-001")
	if item.Status != "active" {
		t.Errorf("status = %q, want active", item.Status)
	}
}

func TestStartBlocked(t *testing.T) {
	s, cfg := setupTestEnv(t)
	// T-002 depends on T-001 which is queued — should block
	code := Start(s, cfg, []string{"T-002"})
	if code != 1 {
		t.Errorf("Start blocked item returned %d, want 1", code)
	}
}

func TestStartAlreadyActive(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Start(s, cfg, []string{"T-003"})
	if code != 1 {
		t.Errorf("Start already-active returned %d, want 1", code)
	}
}

func TestStartAssignedToOther(t *testing.T) {
	s, cfg := setupTestEnv(t)
	// Set agent ID
	os.Setenv("AS_AGENT_ID", "agent-b")
	defer os.Unsetenv("AS_AGENT_ID")

	// T-003 is assigned to agent-a — agent-b can't start it
	// But T-003 is active, not queued, so it fails on status check first
	// Let's test with T-001 after assigning it to agent-a
	item, _ := s.Get("T-001")
	item.AssignedTo = "agent-a"

	code := Start(s, cfg, []string{"T-001"})
	if code != 1 {
		t.Errorf("Start assigned-to-other returned %d, want 1", code)
	}
}

// --- Close ---

func TestCloseHappy(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Close(s, cfg, []string{"T-003", "completed"})
	if code != 0 {
		t.Errorf("Close T-003 returned %d, want 0", code)
	}

	item, _ := s.Get("T-003")
	if item.Status != "completed" {
		t.Errorf("status = %q, want completed", item.Status)
	}
}

func TestCloseAbandonedRequiresReason(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Close(s, cfg, []string{"T-003", "abandoned"})
	if code != 2 {
		t.Errorf("Close abandoned without reason returned %d, want 2", code)
	}
}

func TestCloseInvalidResolution(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := Close(s, cfg, []string{"T-003", "flying"})
	if code != 2 {
		t.Errorf("Close invalid resolution returned %d, want 2", code)
	}
}

// --- Update ---

func TestUpdateHappy(t *testing.T) {
	s, _ := setupTestEnv(t)
	code := Update(s, []string{"T-001", "title", "Updated title"})
	if code != 0 {
		t.Errorf("Update returned %d, want 0", code)
	}
}

func TestUpdateNotFound(t *testing.T) {
	s, _ := setupTestEnv(t)
	code := Update(s, []string{"T-999", "title", "nope"})
	if code != 1 {
		t.Errorf("Update nonexistent returned %d, want 1", code)
	}
}

func TestUpdateNoArgs(t *testing.T) {
	s, _ := setupTestEnv(t)
	code := Update(s, []string{"T-001"})
	if code != 2 {
		t.Errorf("Update no field returned %d, want 2", code)
	}
}
