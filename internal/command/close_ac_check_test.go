package command

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theraprac/agent-state/internal/model"
	"github.com/theraprac/agent-state/internal/testutil"
)

// seedUATPass writes the testing_evidence.uat=pass marker st uat would record.
func seedUATPass(t *testing.T, env *testutil.Env, id string) {
	t.Helper()
	if err := env.S.Mutate(id, func(it *model.Item) error {
		it.SetNested("testing_evidence", "uat", "pass 2026-06-28T00:00:00Z")
		return nil
	}); err != nil {
		t.Fatalf("seedUATPass(%s): %v", id, err)
	}
}

// --- closeACCheck unit tests (marker-based, no AC re-run) ------------------

func TestCloseACGateBlocksWithoutUATMarker(t *testing.T) {
	item := &model.Item{ID: "T-1"}
	if msg := closeACCheck(item, CloseOpts{}); msg == "" {
		t.Fatalf("expected a block message when no st uat marker is present")
	}
}

func TestCloseACGatePassesWithUATMarker(t *testing.T) {
	item := &model.Item{ID: "T-1", TestingEvidence: map[string]interface{}{"uat": "pass 2026-06-28T00:00:00Z"}}
	if msg := closeACCheck(item, CloseOpts{}); msg != "" {
		t.Fatalf("expected pass (empty) with a uat=pass marker, got: %q", msg)
	}
}

func TestCloseACGateBlocksOnFailedUATMarker(t *testing.T) {
	item := &model.Item{ID: "T-1", TestingEvidence: map[string]interface{}{"uat": "fail: 2 acceptance_criteria failing 2026-06-28T00:00:00Z"}}
	if msg := closeACCheck(item, CloseOpts{}); msg == "" {
		t.Fatalf("expected a block message when the uat marker is a failure")
	}
}

func TestCloseACGateSkipACBypasses(t *testing.T) {
	item := &model.Item{ID: "T-1"} // no marker
	if msg := closeACCheck(item, CloseOpts{SkipACRequested: true, SkipAC: "AC needs a live API unavailable in CI"}); msg != "" {
		t.Fatalf("skip-ac with reason should bypass, got: %q", msg)
	}
}

func TestCloseACGateSkipACRequiresReason(t *testing.T) {
	item := &model.Item{ID: "T-1"}
	msg := closeACCheck(item, CloseOpts{SkipACRequested: true, SkipAC: "  "})
	if msg == "" || !strings.Contains(msg, "non-empty reason") {
		t.Fatalf("skip-ac with empty reason should be rejected; got: %q", msg)
	}
}

// --- Close()-level integration: the gate actually fires (review [8]) -------

func TestClose_DoneBlockedWithoutUAT(t *testing.T) {
	env := testutil.NewEnv(t)
	seedWorkTime(t, env, "T-003")
	seedTokens(t, env, "T-003") // capture present, but no uat marker
	if code := Close(env.S, env.Cfg, "T-003", "done", CloseOpts{}); code == 0 {
		t.Fatalf("done close should be blocked without a uat marker")
	}
	it, _ := env.S.Get("T-003")
	if it.Status == "done" {
		t.Errorf("item closed despite no uat marker (status=%q)", it.Status)
	}
}

func TestClose_DonePassesWithUAT(t *testing.T) {
	env := testutil.NewEnv(t)
	seedWorkTime(t, env, "T-003")
	seedTokens(t, env, "T-003")
	seedUATPass(t, env, "T-003")
	if code := Close(env.S, env.Cfg, "T-003", "done", CloseOpts{}); code != 0 {
		t.Fatalf("done close should succeed with capture + uat marker, got %d", code)
	}
	it, _ := env.S.Get("T-003")
	if it.Status != "done" {
		t.Errorf("status = %q, want done", it.Status)
	}
}

func TestClose_ForceBypassesACGate(t *testing.T) {
	env := testutil.NewEnv(t)
	seedWorkTime(t, env, "T-003")
	seedTokens(t, env, "T-003") // capture present so the capture gate passes
	// no uat marker; --force bypasses the AC gate
	if code := Close(env.S, env.Cfg, "T-003", "done", CloseOpts{Force: true}); code != 0 {
		t.Fatalf("--force should bypass the AC gate, got %d", code)
	}
}

func TestClose_SkipACAuditedAfterClose(t *testing.T) {
	env := testutil.NewEnv(t)
	seedWorkTime(t, env, "T-003")
	seedTokens(t, env, "T-003") // capture ok; no uat marker
	if code := Close(env.S, env.Cfg, "T-003", "done", CloseOpts{SkipACRequested: true, SkipAC: "verified manually outside st uat"}); code != 0 {
		t.Fatalf("--skip-ac should allow the close, got %d", code)
	}
	logPath := filepath.Join(env.Cfg.Root(), ".as", "close-ac-skip.log")
	data, err := os.ReadFile(logPath)
	if err != nil || !strings.Contains(string(data), "T-003") {
		t.Errorf("close-ac-skip.log missing the audited bypass: err=%v data=%q", err, string(data))
	}
}
