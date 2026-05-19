package command

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/jfinlinson/agent-state/internal/agentps"
	"github.com/jfinlinson/agent-state/internal/model"
)

func TestAgentPS_RendersFleetWithLiveAndActiveItem(t *testing.T) {
	s, cfg := setupTestEnv(t)
	t.Setenv("CLAUDE_PROJECTS_DIR", t.TempDir())

	// Roster dir with one agent.
	wsDir := filepath.Join(t.TempDir(), ".as", "agent-workspaces")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wsDir, "agent-tt.yaml"),
		[]byte("agent_id: agent-tt\npath: /tmp/ws/theraprac-agent-tt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ST_AGENT_WORKSPACES_DIR", wsDir)

	// Live registration (this process's pid ⇒ IsPIDLive true) + session.
	sid := "sess-aps-1"
	writeFixtureSession(t, "/tmp/tp-fixture", sid) // gives the session a JSONL → LAST-UPDATE
	if err := os.MkdirAll(cfg.AgentsDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfg.AgentsDir(), "agent-tt.yaml"),
		[]byte("agent_id: agent-tt\nroot: agent-tt\npid: "+strconv.Itoa(os.Getpid())+
			"\nstarted: 2026-05-19T10:00:00Z\nsession_id: "+sid+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// An active agent-state item assigned to agent-tt.
	if err := s.Mutate("T-001", func(it *model.Item) error {
		it.Status = "active"
		it.AssignedTo = "agent-tt"
		if it.Delivery == nil {
			it.Delivery = map[string]interface{}{}
		}
		it.Delivery["stage"] = "coding"
		return nil
	}); err != nil {
		t.Fatalf("Mutate T-001: %v", err)
	}

	out := captureStdout(t, func() {
		if code := AgentPS(s, cfg, AgentPSOpts{}); code != 0 {
			t.Fatalf("AgentPS exit %d, want 0", code)
		}
	})
	if !strings.Contains(out, "AGENT") || !strings.Contains(out, "LAST-UPDATE") {
		t.Errorf("missing header:\n%s", out)
	}
	row := ""
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "agent-tt") {
			row = l
		}
	}
	if row == "" {
		t.Fatalf("no agent-tt row:\n%s", out)
	}
	for _, want := range []string{"theraprac-agent-tt", "✓", "T-001 (coding)", "sess-aps"} {
		if !strings.Contains(row, want) {
			t.Errorf("agent-tt row missing %q:\n%s", want, row)
		}
	}

	// --json emits the joined rows pre-render.
	jout := captureStdout(t, func() {
		if code := AgentPS(s, cfg, AgentPSOpts{JSON: true}); code != 0 {
			t.Fatalf("--json exit %d", code)
		}
	})
	var rows []agentps.Row
	if err := json.Unmarshal([]byte(jout), &rows); err != nil {
		t.Fatalf("--json not valid []Row: %v\n%s", err, jout)
	}
	if len(rows) != 1 || rows[0].AgentID != "agent-tt" || rows[0].Item == nil || rows[0].Item.ID != "T-001" {
		t.Errorf("--json rows wrong: %+v", rows)
	}
}

func TestAgentPS_MissingRosterIsReported(t *testing.T) {
	s, cfg := setupTestEnv(t)
	// Point at a non-existent roster dir explicitly → reported, non-zero
	// (absence surfaced, never a silent blank table).
	t.Setenv("ST_AGENT_WORKSPACES_DIR", filepath.Join(t.TempDir(), "does-not-exist"))
	if code := AgentPS(s, cfg, AgentPSOpts{}); code != 1 {
		t.Errorf("missing roster exit %d, want 1", code)
	}
}
