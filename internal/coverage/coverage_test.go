package coverage

import (
	"strings"
	"testing"
)

func TestParseGoCoverprofile(t *testing.T) {
	input := `mode: set
github.com/user/repo/internal/db/billing.go:10.2,15.3 2 1
github.com/user/repo/internal/db/billing.go:17.2,20.3 1 0
github.com/user/repo/internal/db/billing.go:22.2,25.3 3 1
github.com/user/repo/cmd/server/main.go:5.2,10.3 4 4
`
	cov, err := ParseGoCoverprofile(strings.NewReader(input), "github.com/user/repo/")
	if err != nil {
		t.Fatalf("ParseGoCoverprofile: %v", err)
	}

	if len(cov) != 2 {
		t.Fatalf("files = %d, want 2", len(cov))
	}

	billing := cov["internal/db/billing.go"]
	// 2 covered + 0 uncovered + 3 covered = 5 covered out of 6 total
	expected := 5.0 / 6.0 * 100
	if billing.Lines < expected-0.1 || billing.Lines > expected+0.1 {
		t.Errorf("billing.Lines = %.1f, want %.1f", billing.Lines, expected)
	}

	main := cov["cmd/server/main.go"]
	if main.Lines != 100 {
		t.Errorf("main.Lines = %.1f, want 100", main.Lines)
	}

	// Go doesn't track branches/functions — should default to 100
	if billing.Branches != 100 || billing.Functions != 100 {
		t.Errorf("billing branches=%.0f functions=%.0f, want 100/100", billing.Branches, billing.Functions)
	}
}

func TestParseGoCoverprofileNoModule(t *testing.T) {
	input := `mode: set
internal/db/billing.go:10.2,15.3 2 1
`
	cov, err := ParseGoCoverprofile(strings.NewReader(input), "")
	if err != nil {
		t.Fatalf("ParseGoCoverprofile: %v", err)
	}
	if _, ok := cov["internal/db/billing.go"]; !ok {
		t.Error("expected billing.go in coverage")
	}
}

func TestParseGoCoverprofileEmpty(t *testing.T) {
	input := `mode: set
`
	cov, err := ParseGoCoverprofile(strings.NewReader(input), "")
	if err != nil {
		t.Fatalf("ParseGoCoverprofile: %v", err)
	}
	if len(cov) != 0 {
		t.Errorf("files = %d, want 0", len(cov))
	}
}

func TestParseGoCoverprofileZeroCoverage(t *testing.T) {
	input := `mode: set
github.com/user/repo/pkg/unused.go:1.2,5.3 3 0
github.com/user/repo/pkg/unused.go:7.2,10.3 2 0
`
	cov, err := ParseGoCoverprofile(strings.NewReader(input), "github.com/user/repo/")
	if err != nil {
		t.Fatalf("ParseGoCoverprofile: %v", err)
	}
	if cov["pkg/unused.go"].Lines != 0 {
		t.Errorf("Lines = %.1f, want 0", cov["pkg/unused.go"].Lines)
	}
}

func TestParseVitestSummary(t *testing.T) {
	input := `{
  "total": {
    "lines": {"total": 100, "covered": 90, "pct": 90.0},
    "branches": {"total": 50, "covered": 40, "pct": 80.0},
    "functions": {"total": 20, "covered": 20, "pct": 100.0},
    "statements": {"total": 120, "covered": 110, "pct": 91.67}
  },
  "src/utils.ts": {
    "lines": {"total": 50, "covered": 48, "pct": 96.0},
    "branches": {"total": 20, "covered": 18, "pct": 90.0},
    "functions": {"total": 10, "covered": 10, "pct": 100.0},
    "statements": {"total": 60, "covered": 58, "pct": 96.67}
  },
  "src/Button.tsx": {
    "lines": {"total": 50, "covered": 42, "pct": 84.0},
    "branches": {"total": 30, "covered": 22, "pct": 73.33},
    "functions": {"total": 10, "covered": 10, "pct": 100.0},
    "statements": {"total": 60, "covered": 52, "pct": 86.67}
  }
}`
	cov, err := ParseVitestSummary(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseVitestSummary: %v", err)
	}

	// Should not include "total"
	if _, ok := cov["total"]; ok {
		t.Error("should not include 'total' aggregate")
	}

	if len(cov) != 2 {
		t.Fatalf("files = %d, want 2", len(cov))
	}

	utils := cov["src/utils.ts"]
	if utils.Lines != 96.0 || utils.Branches != 90.0 || utils.Functions != 100.0 {
		t.Errorf("utils = %+v", utils)
	}

	button := cov["src/Button.tsx"]
	if button.Lines != 84.0 || button.Branches != 73.33 {
		t.Errorf("button = %+v", button)
	}
}

func TestParseVitestSummaryEmpty(t *testing.T) {
	input := `{}`
	cov, err := ParseVitestSummary(strings.NewReader(input))
	if err != nil {
		t.Fatalf("ParseVitestSummary: %v", err)
	}
	if len(cov) != 0 {
		t.Errorf("files = %d, want 0", len(cov))
	}
}

func TestParseVitestSummaryBadJSON(t *testing.T) {
	_, err := ParseVitestSummary(strings.NewReader("not json"))
	if err == nil {
		t.Error("expected error on bad JSON")
	}
}

func TestCheckThresholds(t *testing.T) {
	coverage := map[string]FileCoverage{
		"src/good.ts":    {Lines: 95, Branches: 85, Functions: 100},
		"src/bad.ts":     {Lines: 70, Branches: 60, Functions: 80},
		"src/partial.ts": {Lines: 91, Branches: 75, Functions: 100},
	}

	thresh := Thresholds{Lines: 90, Branches: 80, Functions: 100}
	files := []string{"src/good.ts", "src/bad.ts", "src/partial.ts"}

	violations := CheckThresholds(coverage, files, thresh)

	// bad.ts: lines (70<90), branches (60<80), functions (80<100)
	// partial.ts: branches (75<80)
	if len(violations) != 4 {
		t.Fatalf("violations = %d, want 4: %v", len(violations), violations)
	}

	// Verify violation details
	foundBadLines := false
	for _, v := range violations {
		if v.Path == "src/bad.ts" && v.Metric == "lines" && v.Actual == 70 {
			foundBadLines = true
		}
	}
	if !foundBadLines {
		t.Error("missing violation for src/bad.ts lines")
	}
}

func TestCheckThresholdsNoViolations(t *testing.T) {
	coverage := map[string]FileCoverage{
		"src/good.ts": {Lines: 95, Branches: 85, Functions: 100},
	}
	violations := CheckThresholds(coverage, []string{"src/good.ts"}, DefaultThresholds())
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none", violations)
	}
}

func TestCheckThresholdsFileNotInCoverage(t *testing.T) {
	coverage := map[string]FileCoverage{}
	violations := CheckThresholds(coverage, []string{"src/missing.ts"}, DefaultThresholds())
	if len(violations) != 0 {
		t.Errorf("violations = %v, want none (file not in report)", violations)
	}
}

func TestViolationString(t *testing.T) {
	v := Violation{Path: "src/bad.ts", Metric: "lines", Actual: 70, Threshold: 90}
	s := v.String()
	if s != "src/bad.ts: lines 70.0% < 90.0%" {
		t.Errorf("String() = %q", s)
	}
}

func TestDefaultThresholds(t *testing.T) {
	d := DefaultThresholds()
	if d.Lines != 90 || d.Branches != 80 || d.Functions != 100 {
		t.Errorf("defaults = %+v", d)
	}
}
