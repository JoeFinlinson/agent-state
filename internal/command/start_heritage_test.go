package command

import (
	"testing"
)

func TestStart_WritesHeritageMetaWhenInherited(t *testing.T) {
	resetIdentityEnv(t)
	t.Setenv("AS_AGENT_ID", "agent-child")
	t.Setenv("AS_AGENT_PARENT_ID", "agent-a")
	t.Setenv("AS_AGENT_ROOT_ID", "agent-root")
	t.Setenv("AS_AGENT_ROLE", "reviewer")
	t.Setenv("AS_SESSION_ID", "test-session-heritage")
	defer t.Setenv("AS_SESSION_ID", "")

	s, cfg := setupTestEnv(t)

	if rc := Start(s, cfg, "T-001", StartOpts{}); rc != 0 {
		t.Fatalf("Start = %d, want 0", rc)
	}

	item, _ := s.Get("T-001")
	if item.AssignedTo != "agent-child" {
		t.Errorf("AssignedTo = %q, want agent-child", item.AssignedTo)
	}
	if item.Doc == nil {
		t.Fatal("item.Doc is nil")
	}

	parent, ok := item.Doc.GetNestedField("assigned_to_meta.parent_id")
	if !ok || parent != "agent-a" {
		t.Errorf("assigned_to_meta.parent_id = %q (ok=%v), want agent-a", parent, ok)
	}
	root, ok := item.Doc.GetNestedField("assigned_to_meta.root_id")
	if !ok || root != "agent-root" {
		t.Errorf("assigned_to_meta.root_id = %q (ok=%v), want agent-root", root, ok)
	}
	role, ok := item.Doc.GetNestedField("assigned_to_meta.role")
	if !ok || role != "reviewer" {
		t.Errorf("assigned_to_meta.role = %q (ok=%v), want reviewer", role, ok)
	}
}

func TestStart_OmitsHeritageMetaWhenSolo(t *testing.T) {
	resetIdentityEnv(t)
	t.Setenv("AS_AGENT_ID", "agent-solo")
	t.Setenv("AS_SESSION_ID", "test-session-solo")
	defer t.Setenv("AS_SESSION_ID", "")

	s, cfg := setupTestEnv(t)

	if rc := Start(s, cfg, "T-001", StartOpts{}); rc != 0 {
		t.Fatalf("Start = %d, want 0", rc)
	}

	item, _ := s.Get("T-001")
	if _, ok := item.Doc.GetNestedField("assigned_to_meta.parent_id"); ok {
		t.Errorf("assigned_to_meta.parent_id should not exist for solo agent")
	}
}

func TestFormatAssignment(t *testing.T) {
	resetIdentityEnv(t)
	t.Setenv("AS_AGENT_ID", "agent-child")
	t.Setenv("AS_AGENT_PARENT_ID", "agent-a")
	t.Setenv("AS_SESSION_ID", "test-session-render")
	defer t.Setenv("AS_SESSION_ID", "")

	s, cfg := setupTestEnv(t)
	if rc := Start(s, cfg, "T-001", StartOpts{}); rc != 0 {
		t.Fatalf("Start = %d", rc)
	}
	item, _ := s.Get("T-001")
	got := formatAssignment(item)
	want := "agent-child ← agent-a"
	if got != want {
		t.Errorf("formatAssignment() = %q, want %q", got, want)
	}
}

func TestFormatAssignment_DeepChain(t *testing.T) {
	resetIdentityEnv(t)
	t.Setenv("AS_AGENT_ID", "agent-grandchild")
	t.Setenv("AS_AGENT_PARENT_ID", "agent-child")
	t.Setenv("AS_AGENT_ROOT_ID", "agent-root")
	t.Setenv("AS_SESSION_ID", "test-session-deep")
	defer t.Setenv("AS_SESSION_ID", "")

	s, cfg := setupTestEnv(t)
	if rc := Start(s, cfg, "T-001", StartOpts{}); rc != 0 {
		t.Fatalf("Start = %d", rc)
	}
	item, _ := s.Get("T-001")
	got := formatAssignment(item)
	want := "agent-grandchild ← agent-child (root: agent-root)"
	if got != want {
		t.Errorf("formatAssignment() = %q, want %q", got, want)
	}
}
