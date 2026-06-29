package command

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestGoalBreakdownNoActiveGoals: with no active goals, prints a sentinel
// message and exits 0.
func TestGoalBreakdownNoActiveGoals(t *testing.T) {
	_, s, cfg := newGoalEnv(t)

	var buf bytes.Buffer
	code := goalBreakdownTo(&buf, s, cfg, GoalBreakdownOpts{Top: 3})
	if code != 0 {
		t.Errorf("expected exit 0 with no goals, got %d", code)
	}
	if !strings.Contains(buf.String(), "No active goals") {
		t.Errorf("expected 'No active goals' in output, got: %q", buf.String())
	}
}

// TestGoalBreakdownTopN: with two active goals and several tasks, each goal
// section shows at most --top N items.
func TestGoalBreakdownTopN(t *testing.T) {
	t.Setenv("AS_AGENT_ID", "agent-a")
	_, s, cfg := newGoalEnv(t)

	// Seed two active goals.
	seedGoalFile(t, cfg, "G-001", "active", 80)
	seedGoalFile(t, cfg, "G-002", "active", 20)

	// Seed four queued tasks linked to G-001.
	for _, id := range []string{"T-001", "T-002", "T-003", "T-004"} {
		seedTaskInGoalEnv(t, cfg, id, "queued")
	}
	// One task linked to G-002.
	seedTaskInGoalEnv(t, cfg, "T-005", "queued")

	s = reloadStoreGoal(t, cfg)

	// Link tasks to goals.
	if rc := ItemGoalsAdd(s, cfg, "T-001", []string{"G-001"}); rc != 0 {
		t.Fatalf("ItemGoalsAdd T-001 rc=%d", rc)
	}
	if rc := ItemGoalsAdd(s, cfg, "T-002", []string{"G-001"}); rc != 0 {
		t.Fatalf("ItemGoalsAdd T-002 rc=%d", rc)
	}
	if rc := ItemGoalsAdd(s, cfg, "T-003", []string{"G-001"}); rc != 0 {
		t.Fatalf("ItemGoalsAdd T-003 rc=%d", rc)
	}
	if rc := ItemGoalsAdd(s, cfg, "T-004", []string{"G-001"}); rc != 0 {
		t.Fatalf("ItemGoalsAdd T-004 rc=%d", rc)
	}
	if rc := ItemGoalsAdd(s, cfg, "T-005", []string{"G-002"}); rc != 0 {
		t.Fatalf("ItemGoalsAdd T-005 rc=%d", rc)
	}
	s = reloadStoreGoal(t, cfg)

	var buf bytes.Buffer
	code := goalBreakdownTo(&buf, s, cfg, GoalBreakdownOpts{Top: 2})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, buf.String())
	}

	out := buf.String()

	// Both goals appear in the output.
	if !strings.Contains(out, "G-001") {
		t.Error("expected G-001 in output")
	}
	if !strings.Contains(out, "G-002") {
		t.Error("expected G-002 in output")
	}

	// G-001 appears before G-002 (sorted by weight descending: 80 > 20).
	g1pos := strings.Index(out, "G-001")
	g2pos := strings.Index(out, "G-002")
	if g1pos >= g2pos {
		t.Errorf("G-001 (weight 80) should appear before G-002 (weight 20)")
	}

	// At most 2 items per goal. T-001 through T-004 are in G-001 but only 2 show.
	// Count how many T-00x IDs appear between G-001 and G-002 headers.
	g1Section := out[g1pos:g2pos]
	g1Items := 0
	for _, id := range []string{"T-001", "T-002", "T-003", "T-004"} {
		if strings.Contains(g1Section, id) {
			g1Items++
		}
	}
	if g1Items > 2 {
		t.Errorf("expected at most 2 items in G-001 section (--top 2), got %d\n%s", g1Items, g1Section)
	}
	if g1Items == 0 {
		t.Errorf("expected at least 1 item in G-001 section, got 0\n%s", g1Section)
	}

	// T-005 appears in G-002 section.
	if !strings.Contains(out[g2pos:], "T-005") {
		t.Errorf("expected T-005 in G-002 section\n%s", out[g2pos:])
	}
}

// TestGoalBreakdownJSON: --json flag emits valid JSON with expected shape.
func TestGoalBreakdownJSON(t *testing.T) {
	t.Setenv("AS_AGENT_ID", "agent-a")
	_, s, cfg := newGoalEnv(t)

	seedGoalFile(t, cfg, "G-010", "active", 50)
	seedTaskInGoalEnv(t, cfg, "T-010", "queued")
	s = reloadStoreGoal(t, cfg)
	if rc := ItemGoalsAdd(s, cfg, "T-010", []string{"G-010"}); rc != 0 {
		t.Fatalf("ItemGoalsAdd rc=%d", rc)
	}
	s = reloadStoreGoal(t, cfg)

	var buf bytes.Buffer
	code := goalBreakdownTo(&buf, s, cfg, GoalBreakdownOpts{Top: 3, JSON: true})
	if code != 0 {
		t.Fatalf("expected exit 0, got %d\noutput: %s", code, buf.String())
	}

	var out []goalBreakdownGoalJSON
	if err := json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, buf.String())
	}

	if len(out) != 1 {
		t.Fatalf("expected 1 goal in JSON, got %d", len(out))
	}
	g := out[0]
	if g.GoalID != "G-010" {
		t.Errorf("expected goal_id 'G-010', got %q", g.GoalID)
	}
	if g.Weight != 50 {
		t.Errorf("expected weight 50, got %d", g.Weight)
	}
	if len(g.Items) != 1 {
		t.Errorf("expected 1 item under G-010, got %d", len(g.Items))
	}
	if len(g.Items) > 0 && g.Items[0].ID != "T-010" {
		t.Errorf("expected item T-010, got %q", g.Items[0].ID)
	}
}

// TestGoalBreakdownEmptyGoalSection: a goal with no workable items shows
// "(no workable items)" in text mode rather than crashing or being omitted.
func TestGoalBreakdownEmptyGoalSection(t *testing.T) {
	_, s, cfg := newGoalEnv(t)
	seedGoalFile(t, cfg, "G-020", "active", 30)
	// No tasks seeded — goal has no workable items.
	s = reloadStoreGoal(t, cfg)

	var buf bytes.Buffer
	code := goalBreakdownTo(&buf, s, cfg, GoalBreakdownOpts{Top: 3})
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	out := buf.String()
	if !strings.Contains(out, "G-020") {
		t.Error("expected G-020 to appear even with no items")
	}
	if !strings.Contains(out, "no workable items") {
		t.Errorf("expected 'no workable items' in output, got: %q", out)
	}
}
