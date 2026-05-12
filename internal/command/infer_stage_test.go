package command

import (
	"testing"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/model"
)

// stubBranchCheck returns a BranchCheck stub that always returns the given value.
func stubBranchCheck(exists bool) func(*config.Config, string) bool {
	return func(*config.Config, string) bool { return exists }
}

// stubPRFetch returns a PRFetch stub returning the given state.
func stubPRFetch(state string) func(*config.Config, string) (string, []string) {
	return func(*config.Config, string) (string, []string) {
		if state == "" {
			return "", nil
		}
		return state, []string{"https://github.com/org/repo/pull/1"}
	}
}

// runInfer is a small test helper that creates a fresh env, seeds the item,
// and invokes InferStage with stubbed signals.
func runInfer(t *testing.T, currentStage string, branchExists bool, prState string) string {
	t.Helper()
	s, cfg := setupTestEnv(t)

	if err := s.Mutate("T-001", func(it *model.Item) error {
		it.SetNested("delivery", "stage", currentStage)
		it.SetNested("work_tracking", "branch", "feat/T-001-test")
		return nil
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	rc := InferStage(s, cfg, "T-001", InferStageOpts{
		BranchCheck: stubBranchCheck(branchExists),
		PRFetch:     stubPRFetch(prState),
	})
	if rc != 0 {
		t.Errorf("InferStage rc = %d, want 0", rc)
	}

	it, _ := s.Get("T-001")
	stage, _ := getNestedField(it, "delivery", "stage")
	return stage
}

// --- Stage matrix ---

func TestInferStage_BranchPushedNoPR(t *testing.T) {
	got := runInfer(t, "coding", true, "")
	if got != "pushed" {
		t.Errorf("coding + branch-on-remote → %q, want pushed", got)
	}
}

func TestInferStage_BranchPushedPROpen(t *testing.T) {
	got := runInfer(t, "coding", true, "OPEN")
	if got != "pr_open" {
		t.Errorf("coding + branch + OPEN PR → %q, want pr_open", got)
	}
}

func TestInferStage_PushedToPROpen(t *testing.T) {
	got := runInfer(t, "pushed", true, "OPEN")
	if got != "pr_open" {
		t.Errorf("pushed + OPEN → %q, want pr_open", got)
	}
}

func TestInferStage_PushedToMerged(t *testing.T) {
	got := runInfer(t, "pushed", true, "MERGED")
	if got != "merged" {
		t.Errorf("pushed + MERGED → %q, want merged", got)
	}
}

func TestInferStage_PROpenToMerged(t *testing.T) {
	got := runInfer(t, "pr_open", true, "MERGED")
	if got != "merged" {
		t.Errorf("pr_open + MERGED → %q, want merged", got)
	}
}

func TestInferStage_MergedNoRegress(t *testing.T) {
	// MERGED PR signal must not regress merged item; it's a no-op advance.
	got := runInfer(t, "merged", true, "MERGED")
	if got != "merged" {
		t.Errorf("merged + MERGED → %q, want merged (no regress)", got)
	}
}

func TestInferStage_DeployedDevNoRegressFromPushed(t *testing.T) {
	// Branch-on-remote signal computes target=pushed; deployed_dev > pushed
	// in stage order, so advanceDeliveryStage must NOT regress.
	got := runInfer(t, "deployed_dev", true, "")
	if got != "deployed_dev" {
		t.Errorf("deployed_dev + branch → %q, want deployed_dev (no regress)", got)
	}
}

func TestInferStage_BranchAbsentNoAdvance(t *testing.T) {
	got := runInfer(t, "coding", false, "")
	if got != "coding" {
		t.Errorf("coding + no remote branch → %q, want coding", got)
	}
}

func TestInferStage_PROpenNoBranchPreservesStage(t *testing.T) {
	got := runInfer(t, "pr_open", false, "")
	if got != "pr_open" {
		t.Errorf("pr_open + no signals → %q, want pr_open (no regress)", got)
	}
}

// --- Edge cases ---

func TestInferStage_EmptyIDEmptyStackNoOp(t *testing.T) {
	s, cfg := setupTestEnv(t)
	rc := InferStage(s, cfg, "", InferStageOpts{
		BranchCheck: stubBranchCheck(true),
		PRFetch:     stubPRFetch("OPEN"),
	})
	if rc != 0 {
		t.Errorf("empty id + empty stack rc = %d, want 0", rc)
	}
}

func TestInferStage_UnknownIDNoOp(t *testing.T) {
	s, cfg := setupTestEnv(t)
	rc := InferStage(s, cfg, "T-999-nope", InferStageOpts{
		BranchCheck: stubBranchCheck(true),
		PRFetch:     stubPRFetch("OPEN"),
	})
	if rc != 0 {
		t.Errorf("unknown id rc = %d, want 0", rc)
	}
}

func TestInferStage_NoBranchOnItemNoOp(t *testing.T) {
	s, cfg := setupTestEnv(t)
	// T-001 has no work_tracking.branch by default in the fixture.
	rc := InferStage(s, cfg, "T-001", InferStageOpts{
		BranchCheck: stubBranchCheck(true),
		PRFetch:     stubPRFetch("OPEN"),
	})
	if rc != 0 {
		t.Errorf("no branch rc = %d, want 0", rc)
	}
	it, _ := s.Get("T-001")
	stage, _ := getNestedField(it, "delivery", "stage")
	if stage == "pushed" || stage == "pr_open" || stage == "merged" {
		t.Errorf("no branch should not advance, got stage = %q", stage)
	}
}

func TestInferStage_StackTopResolvedWhenIDOmitted(t *testing.T) {
	s, cfg := setupTestEnv(t)

	// Seed stage + branch on T-001
	if err := s.Mutate("T-001", func(it *model.Item) error {
		it.SetNested("delivery", "stage", "coding")
		it.SetNested("work_tracking", "branch", "feat/T-001-test")
		it.Doc.SetField("status", "active")
		it.Status = "active"
		return nil
	}); err != nil {
		t.Fatalf("mutate: %v", err)
	}

	// Push T-001 onto the stack so the resolver finds it.
	if rc := StackPush(s, cfg, "T-001", StackPushOpts{Reason: "test"}); rc != 0 {
		t.Fatalf("StackPush rc = %d", rc)
	}

	rc := InferStage(s, cfg, "", InferStageOpts{
		BranchCheck: stubBranchCheck(true),
		PRFetch:     stubPRFetch(""),
	})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}

	it, _ := s.Get("T-001")
	stage, _ := getNestedField(it, "delivery", "stage")
	if stage != "pushed" {
		t.Errorf("stack-top resolution: stage = %q, want pushed", stage)
	}
}
