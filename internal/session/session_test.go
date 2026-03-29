package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func tempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "sessions")
}

func TestEnsureSession_CreatesNew(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	s, err := mgr.EnsureSession("abc-123", "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "abc-123" {
		t.Errorf("ID = %q, want %q", s.ID, "abc-123")
	}
	if s.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", s.AgentID, "test-agent")
	}
	if s.StartedAt.IsZero() {
		t.Error("StartedAt should be set")
	}
	if s.LastActive.IsZero() {
		t.Error("LastActive should be set")
	}

	// File should exist
	if _, err := os.Stat(filepath.Join(dir, "abc-123.yaml")); err != nil {
		t.Errorf("session file should exist: %v", err)
	}
}

func TestEnsureSession_ReturnsExisting(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	s1, _ := mgr.EnsureSession("abc-123", "agent-1")
	s2, err := mgr.EnsureSession("abc-123", "agent-2")
	if err != nil {
		t.Fatal(err)
	}

	// Should return existing, not overwrite
	if s2.AgentID != s1.AgentID {
		t.Errorf("AgentID = %q, want %q (should keep original)", s2.AgentID, s1.AgentID)
	}
}

func TestLoadSaveRoundtrip(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	now := time.Now().Truncate(time.Second)
	original := &Session{
		ID:           "sess-456",
		StartedAt:    now,
		AgentID:      "my-agent",
		Sprint:       "cool-sprint",
		LastActive:   now,
		ClaimedItems: []string{"T-001", "T-002"},
	}

	if err := mgr.Save(original); err != nil {
		t.Fatal(err)
	}

	loaded, err := mgr.Load("sess-456")
	if err != nil {
		t.Fatal(err)
	}
	if loaded == nil {
		t.Fatal("Load returned nil")
	}

	if loaded.ID != "sess-456" {
		t.Errorf("ID = %q", loaded.ID)
	}
	if loaded.AgentID != "my-agent" {
		t.Errorf("AgentID = %q", loaded.AgentID)
	}
	if loaded.Sprint != "cool-sprint" {
		t.Errorf("Sprint = %q", loaded.Sprint)
	}
	if len(loaded.ClaimedItems) != 2 {
		t.Fatalf("ClaimedItems len = %d, want 2", len(loaded.ClaimedItems))
	}
	if loaded.ClaimedItems[0] != "T-001" || loaded.ClaimedItems[1] != "T-002" {
		t.Errorf("ClaimedItems = %v", loaded.ClaimedItems)
	}
}

func TestLoad_NotFound(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	s, err := mgr.Load("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if s != nil {
		t.Error("Load should return nil for nonexistent session")
	}
}

func TestAddClaim(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	mgr.EnsureSession("sess-1", "agent")

	if err := mgr.AddClaim("sess-1", "T-001"); err != nil {
		t.Fatal(err)
	}

	s, _ := mgr.Load("sess-1")
	if len(s.ClaimedItems) != 1 || s.ClaimedItems[0] != "T-001" {
		t.Errorf("ClaimedItems = %v, want [T-001]", s.ClaimedItems)
	}

	// Adding same claim again is idempotent
	if err := mgr.AddClaim("sess-1", "T-001"); err != nil {
		t.Fatal(err)
	}
	s, _ = mgr.Load("sess-1")
	if len(s.ClaimedItems) != 1 {
		t.Errorf("duplicate add: ClaimedItems len = %d, want 1", len(s.ClaimedItems))
	}
}

func TestAddClaim_SessionNotFound(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	err := mgr.AddClaim("nonexistent", "T-001")
	if err == nil {
		t.Error("AddClaim should fail for nonexistent session")
	}
}

func TestRemoveClaim(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	mgr.EnsureSession("sess-1", "agent")
	mgr.AddClaim("sess-1", "T-001")
	mgr.AddClaim("sess-1", "T-002")

	if err := mgr.RemoveClaim("sess-1", "T-001"); err != nil {
		t.Fatal(err)
	}

	s, _ := mgr.Load("sess-1")
	if len(s.ClaimedItems) != 1 || s.ClaimedItems[0] != "T-002" {
		t.Errorf("after remove: ClaimedItems = %v", s.ClaimedItems)
	}
}

func TestRemoveClaim_SessionGone(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	// Should not error when session doesn't exist
	err := mgr.RemoveClaim("gone-session", "T-001")
	if err != nil {
		t.Errorf("RemoveClaim should not error for missing session: %v", err)
	}
}

func TestTouch(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	mgr.EnsureSession("sess-1", "agent")

	// Artificially age the session
	s, _ := mgr.Load("sess-1")
	s.LastActive = time.Now().Add(-1 * time.Hour)
	mgr.Save(s)

	before, _ := mgr.Load("sess-1")
	if err := mgr.Touch("sess-1"); err != nil {
		t.Fatal(err)
	}
	after, _ := mgr.Load("sess-1")

	if !after.LastActive.After(before.LastActive) {
		t.Error("Touch should update LastActive")
	}
}

func TestTouch_NoSession(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	// Touch on nonexistent session should be no-op
	if err := mgr.Touch("nonexistent"); err != nil {
		t.Errorf("Touch should not error for missing session: %v", err)
	}
}

func TestIsStale(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 1*time.Hour)

	fresh := &Session{LastActive: time.Now()}
	if mgr.IsStale(fresh) {
		t.Error("fresh session should not be stale")
	}

	old := &Session{LastActive: time.Now().Add(-2 * time.Hour)}
	if !mgr.IsStale(old) {
		t.Error("old session should be stale")
	}
}

func TestIsStale_ZeroTTL(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 0)

	old := &Session{LastActive: time.Now().Add(-24 * time.Hour)}
	if mgr.IsStale(old) {
		t.Error("zero TTL should never be stale")
	}
}

func TestListSessions(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	mgr.EnsureSession("sess-1", "agent-1")
	mgr.EnsureSession("sess-2", "agent-2")

	sessions, err := mgr.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Errorf("ListSessions len = %d, want 2", len(sessions))
	}
}

func TestListSessions_EmptyDir(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	sessions, err := mgr.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 0 {
		t.Errorf("ListSessions len = %d, want 0", len(sessions))
	}
}

func TestStaleSessions(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 1*time.Hour)

	// Create a fresh session
	mgr.EnsureSession("fresh", "agent")

	// Create a stale session (manually set old time)
	mgr.EnsureSession("stale", "agent")
	s, _ := mgr.Load("stale")
	s.LastActive = time.Now().Add(-3 * time.Hour)
	mgr.Save(s)

	stale, err := mgr.StaleSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("StaleSessions len = %d, want 1", len(stale))
	}
	if stale[0].ID != "stale" {
		t.Errorf("stale session ID = %q, want %q", stale[0].ID, "stale")
	}
}

func TestLoadWithSprintField(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	s := &Session{
		ID:         "sess-sprint",
		StartedAt:  time.Now(),
		AgentID:    "agent",
		Sprint:     "cool-sprint-name",
		LastActive: time.Now(),
	}
	mgr.Save(s)

	loaded, _ := mgr.Load("sess-sprint")
	if loaded.Sprint != "cool-sprint-name" {
		t.Errorf("Sprint = %q, want %q", loaded.Sprint, "cool-sprint-name")
	}
}

func TestEnsureSession_Error(t *testing.T) {
	// Use a path that can't be created
	mgr := NewManager("/dev/null/sessions", 2*time.Hour)
	_, err := mgr.EnsureSession("sess-1", "agent")
	if err == nil {
		t.Error("EnsureSession should fail with invalid path")
	}
}

func TestSaveWithoutAgentOrSprint(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	s := &Session{
		ID:         "sess-bare",
		StartedAt:  time.Now(),
		LastActive: time.Now(),
	}
	if err := mgr.Save(s); err != nil {
		t.Fatal(err)
	}
	loaded, _ := mgr.Load("sess-bare")
	if loaded.AgentID != "" {
		t.Errorf("AgentID should be empty, got %q", loaded.AgentID)
	}
	if loaded.Sprint != "" {
		t.Errorf("Sprint should be empty, got %q", loaded.Sprint)
	}
}

func TestListSessions_SkipsNonYaml(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	mgr.EnsureSession("sess-1", "agent")
	// Create a non-yaml file
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a session"), 0644)

	sessions, err := mgr.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Errorf("ListSessions len = %d, want 1 (should skip non-yaml)", len(sessions))
	}
}

// --- PruneStaleSessions ---

func TestPruneStaleSessions(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 1*time.Hour)

	// Create a fresh session
	mgr.EnsureSession("fresh", "agent")

	// Create a stale session with no claims
	mgr.EnsureSession("stale-no-claims", "agent")
	s, _ := mgr.Load("stale-no-claims")
	s.LastActive = time.Now().Add(-3 * time.Hour)
	mgr.Save(s)

	// Create a stale session WITH claims (should not be pruned)
	mgr.EnsureSession("stale-with-claims", "agent")
	s2, _ := mgr.Load("stale-with-claims")
	s2.LastActive = time.Now().Add(-3 * time.Hour)
	s2.ClaimedItems = []string{"T-001"}
	mgr.Save(s2)

	pruned, err := mgr.PruneStaleSessions()
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}

	// Verify stale-no-claims is gone
	s3, _ := mgr.Load("stale-no-claims")
	if s3 != nil {
		t.Error("stale-no-claims should be deleted")
	}

	// Verify stale-with-claims still exists
	s4, _ := mgr.Load("stale-with-claims")
	if s4 == nil {
		t.Error("stale-with-claims should still exist")
	}

	// Verify fresh session still exists
	s5, _ := mgr.Load("fresh")
	if s5 == nil {
		t.Error("fresh session should still exist")
	}
}

func TestPruneStaleSessionsEmpty(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 1*time.Hour)

	pruned, err := mgr.PruneStaleSessions()
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
}

func TestPruneStaleSessionsAllFresh(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 1*time.Hour)

	mgr.EnsureSession("sess-1", "agent")
	mgr.EnsureSession("sess-2", "agent")

	pruned, err := mgr.PruneStaleSessions()
	if err != nil {
		t.Fatal(err)
	}
	if pruned != 0 {
		t.Errorf("pruned = %d, want 0", pruned)
	}
}

func TestDeleteSession(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	mgr.EnsureSession("sess-to-delete", "agent")

	// Verify exists
	s, _ := mgr.Load("sess-to-delete")
	if s == nil {
		t.Fatal("session should exist before delete")
	}

	if err := mgr.DeleteSession("sess-to-delete"); err != nil {
		t.Fatal(err)
	}

	// Verify gone
	s, _ = mgr.Load("sess-to-delete")
	if s != nil {
		t.Error("session should be deleted")
	}
}

// --- Concurrent session scenarios ---

func TestMultipleSessionsSameSprint(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	// Session 1 joins sprint
	s1, _ := mgr.EnsureSession("sess-1", "agent-a")
	s1.Sprint = "my-sprint"
	mgr.Save(s1)

	// Session 2 joins same sprint
	s2, _ := mgr.EnsureSession("sess-2", "agent-b")
	s2.Sprint = "my-sprint"
	mgr.Save(s2)

	// Both should have the sprint
	loaded1, _ := mgr.Load("sess-1")
	loaded2, _ := mgr.Load("sess-2")
	if loaded1.Sprint != "my-sprint" {
		t.Errorf("sess-1 sprint = %q", loaded1.Sprint)
	}
	if loaded2.Sprint != "my-sprint" {
		t.Errorf("sess-2 sprint = %q", loaded2.Sprint)
	}
}

func TestConcurrentClaimDifferentItems(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	mgr.EnsureSession("sess-1", "agent-a")
	mgr.EnsureSession("sess-2", "agent-b")

	// Session 1 claims T-001
	mgr.AddClaim("sess-1", "T-001")
	// Session 2 claims T-002
	mgr.AddClaim("sess-2", "T-002")

	s1, _ := mgr.Load("sess-1")
	s2, _ := mgr.Load("sess-2")

	if len(s1.ClaimedItems) != 1 || s1.ClaimedItems[0] != "T-001" {
		t.Errorf("sess-1 claims = %v, want [T-001]", s1.ClaimedItems)
	}
	if len(s2.ClaimedItems) != 1 || s2.ClaimedItems[0] != "T-002" {
		t.Errorf("sess-2 claims = %v, want [T-002]", s2.ClaimedItems)
	}
}

func TestSessionDeathRecovery(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 1*time.Hour)

	// Create a dead session (stale + has claims)
	mgr.EnsureSession("dead-session", "agent")
	s, _ := mgr.Load("dead-session")
	s.LastActive = time.Now().Add(-3 * time.Hour)
	s.ClaimedItems = []string{"T-001", "T-002"}
	mgr.Save(s)

	// Verify it's stale
	if !mgr.IsStale(s) {
		t.Error("session should be stale")
	}

	// Remove claims (simulating recovery)
	mgr.RemoveClaim("dead-session", "T-001")
	mgr.RemoveClaim("dead-session", "T-002")

	// Now prune should clean it up
	s2, _ := mgr.Load("dead-session")
	s2.LastActive = time.Now().Add(-3 * time.Hour) // re-age since RemoveClaim touches LastActive
	mgr.Save(s2)

	pruned, _ := mgr.PruneStaleSessions()
	if pruned != 1 {
		t.Errorf("pruned = %d, want 1", pruned)
	}
}

func TestSaveEmptyClaimedItems(t *testing.T) {
	dir := tempDir(t)
	mgr := NewManager(dir, 2*time.Hour)

	s := &Session{
		ID:         "sess-empty",
		StartedAt:  time.Now(),
		LastActive: time.Now(),
	}
	if err := mgr.Save(s); err != nil {
		t.Fatal(err)
	}

	loaded, _ := mgr.Load("sess-empty")
	if len(loaded.ClaimedItems) != 0 {
		t.Errorf("empty claimed items loaded as %v", loaded.ClaimedItems)
	}
}
