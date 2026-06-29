package command

import (
	"testing"
)

// --- parseDiffStat ---

func TestParseDiffStat_FilesAndLines(t *testing.T) {
	stat := " internal/command/foo.go | 30 ++++++++++++\n internal/command/bar.go | 10 ++++------\n 2 files changed, 24 insertions(+), 10 deletions(-)"
	files, lines := parseDiffStat(stat)
	if files != 2 {
		t.Errorf("files: got %d, want 2", files)
	}
	if lines != 34 {
		t.Errorf("lines: got %d, want 34 (24+10)", lines)
	}
}

func TestParseDiffStat_InsertionsOnly(t *testing.T) {
	stat := " internal/command/new.go | 45 +++++++++++++++++++++++++++++++++++++++++++++\n 1 file changed, 45 insertions(+)"
	files, lines := parseDiffStat(stat)
	if files != 1 {
		t.Errorf("files: got %d, want 1", files)
	}
	if lines != 45 {
		t.Errorf("lines: got %d, want 45", lines)
	}
}

func TestParseDiffStat_DeletionsOnly(t *testing.T) {
	stat := " foo.go | 5 -----\n 1 file changed, 5 deletions(-)"
	files, lines := parseDiffStat(stat)
	if files != 1 || lines != 5 {
		t.Errorf("got (%d, %d), want (1, 5)", files, lines)
	}
}

func TestParseDiffStat_Empty(t *testing.T) {
	files, lines := parseDiffStat("")
	if files != 0 || lines != 0 {
		t.Errorf("got (%d, %d), want (0, 0)", files, lines)
	}
}

// --- computeDepth ---

func TestComputeDepth_SmallReturnsLow(t *testing.T) {
	got := computeDepth(2, 30, []string{"internal/command/foo.go"})
	if got != "low" {
		t.Errorf("got %q, want %q", got, "low")
	}
}

func TestComputeDepth_ExactSmallBoundaryReturnsLow(t *testing.T) {
	got := computeDepth(3, 50, []string{"cmd/as/app.go"})
	if got != "low" {
		t.Errorf("got %q, want %q", got, "low")
	}
}

func TestComputeDepth_LargeLineCountReturnsHigh(t *testing.T) {
	got := computeDepth(4, 250, []string{"internal/command/foo.go"})
	if got != "high" {
		t.Errorf("got %q, want %q", got, "high")
	}
}

func TestComputeDepth_LargeFileCountReturnsHigh(t *testing.T) {
	got := computeDepth(7, 40, []string{"internal/command/foo.go"})
	if got != "high" {
		t.Errorf("got %q, want %q", got, "high")
	}
}

// TestComputeDepth_BlastRadius covers all blast-radius path fragments in a
// single table so adding a new entry to blastRadiusPaths automatically
// requires a corresponding test case.
func TestComputeDepth_BlastRadius(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"auth", "internal/auth/middleware.go"},
		{"payment", "internal/payment/stripe.go"},
		{"billing", "internal/billing/invoice.go"},
		{"db_changelog", "db/changelog/V1__init.sql"},
		{"hooks", "claude-config/hooks/pre-pr.sh"},
		{"workflows", ".github/workflows/ci.yml"},
		{"infra", "theraprac-infra/terraform/main.tf"},
		{"ansible", "ansible/roles/app/tasks/main.yml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := computeDepth(1, 5, []string{tc.path})
			if got != "high" {
				t.Errorf("path %q: got %q, want %q", tc.path, got, "high")
			}
		})
	}
}

func TestComputeDepth_MediumReturns(t *testing.T) {
	got := computeDepth(5, 80, []string{"internal/command/foo.go"})
	if got != "medium" {
		t.Errorf("got %q, want %q", got, "medium")
	}
}

func TestComputeDepth_JustOverSmallCeilingReturnsMedium(t *testing.T) {
	got := computeDepth(3, 51, []string{"internal/command/foo.go"})
	if got != "medium" {
		t.Errorf("got %q, want %q", got, "medium")
	}
}

func TestComputeDepth_NilPathsNoBlastRadius(t *testing.T) {
	got := computeDepth(2, 30, nil)
	if got != "low" {
		t.Errorf("got %q, want %q", got, "low")
	}
}

// --- ReviewDepth — missing item ---

func TestReviewDepth_MissingItemReturnsNonZero(t *testing.T) {
	s, cfg := setupTestEnv(t)
	code := ReviewDepth(s, cfg, "I-MISSING", ReviewDepthOpts{})
	if code == 0 {
		t.Error("expected non-zero exit for missing item")
	}
}
