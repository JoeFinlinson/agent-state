package plan

import (
	"strings"
	"testing"
)

func TestValidateACs_VerifiableShapes(t *testing.T) {
	// Each entry should be considered verifiable — no findings expected.
	verifiable := []string{
		"cmd: go test ./internal/plan/...",
		"cmd: bats tests/foo.bats",
		"api_unit suite passes",
		"web_unit covers the new helper",
		"go test -run TestPreFlightDetectsMidRebase -count=1",
		"TestValidateACs passes",
		"TestWriteOK_RejectsUnknownType passes",
		`it("denies edit when plan not approved", () => {})`,
		"endpoint returns 200 OK",
		"latency < 50ms",
		"error rate >= 99%",
		"asserts the file moved to archive/",
		"hook denies the Edit when plan_approved is false",
		"emits a warning to stderr listing each un-verifiable AC",
	}
	for _, ac := range verifiable {
		t.Run(ac, func(t *testing.T) {
			findings := ValidateACs([]string{ac})
			if len(findings) > 0 {
				t.Errorf("expected no findings for %q; got: %v", ac, findings)
			}
		})
	}
}

func TestValidateACs_UnverifiableShapes(t *testing.T) {
	unverifiable := []string{
		"fix the bug",
		"works correctly",
		"users see the modal",
		"the feature is fast",
		"performance is good",
	}
	for _, ac := range unverifiable {
		t.Run(ac, func(t *testing.T) {
			findings := ValidateACs([]string{ac})
			if len(findings) != 1 {
				t.Fatalf("expected 1 finding for %q; got %d: %v", ac, len(findings), findings)
			}
			if !strings.Contains(findings[0].Reason, "not verifiable") {
				t.Errorf("finding reason should mention 'not verifiable'; got %q", findings[0].Reason)
			}
		})
	}
}

func TestValidateACs_EmptyACReportsFinding(t *testing.T) {
	findings := ValidateACs([]string{"", "  "})
	if len(findings) != 2 {
		t.Errorf("expected 2 findings for empty/whitespace ACs; got %d", len(findings))
	}
	for _, f := range findings {
		if !strings.Contains(f.Reason, "empty") {
			t.Errorf("empty AC reason should mention 'empty'; got %q", f.Reason)
		}
	}
}

func TestValidateACs_FindingIndexIs1Based(t *testing.T) {
	findings := ValidateACs([]string{
		"cmd: go test ./...",  // OK, no finding
		"works correctly",     // un-verifiable, finding
		"cmd: pytest",         // OK
		"the feature is fast", // un-verifiable, finding
	})
	if len(findings) != 2 {
		t.Fatalf("expected 2 findings; got %d", len(findings))
	}
	if findings[0].Index != 2 || findings[1].Index != 4 {
		t.Errorf("expected indices [2,4]; got [%d,%d]", findings[0].Index, findings[1].Index)
	}
}

func TestValidateACs_FindingStringIncludesACAndIndex(t *testing.T) {
	findings := ValidateACs([]string{"vague AC"})
	if len(findings) != 1 {
		t.Fatalf("expected 1 finding; got %d", len(findings))
	}
	s := findings[0].String()
	if !strings.Contains(s, "vague AC") {
		t.Errorf("String() should include AC text; got %q", s)
	}
	if !strings.Contains(s, "#1") {
		t.Errorf("String() should include 1-based index; got %q", s)
	}
}

func TestContainsWord_WordBoundaries(t *testing.T) {
	cases := []struct {
		hay, needle string
		want        bool
	}{
		{"the test passes", "passes", true},
		{"the test passed", "passes", false}, // close-but-not-equal
		{"unaccepting input", "accepts", false},
		{"system accepts the input", "accepts", true},
		{"prefix returns 200 ok suffix", "returns", true},
		{"returnsmetadata", "returns", false},
	}
	for _, c := range cases {
		t.Run(c.hay+"/"+c.needle, func(t *testing.T) {
			got := containsWord(c.hay, c.needle)
			if got != c.want {
				t.Errorf("containsWord(%q,%q) = %v, want %v", c.hay, c.needle, got, c.want)
			}
		})
	}
}
