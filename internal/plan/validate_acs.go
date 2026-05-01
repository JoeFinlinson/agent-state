package plan

import (
	"fmt"
	"regexp"
	"strings"
)

// ACFinding describes one un-verifiable acceptance criterion. The
// caller decides how to render: Save uses it for stderr warnings,
// `st plan approve --strict` uses it as a hard rejection. I-511.
type ACFinding struct {
	Index  int    // 1-based position of the AC in the list (for human-readable error messages)
	AC     string // the offending AC text (verbatim)
	Reason string // why it's not verifiable + a suggested fix
}

func (f ACFinding) String() string {
	return fmt.Sprintf("AC #%d %q: %s", f.Index, f.AC, f.Reason)
}

// ValidateACs reports findings for any acceptance criteria that lack a
// recognizable verification method. An AC is "verifiable" if it
// contains at least one of:
//
//   - a `cmd:` prefix (existing executable-check convention)
//   - a recognized test-suite name (api_unit, web_e2e, go test, ...)
//   - an assertion-shaped verb (returns, exits, equals, asserts, ...)
//   - a Go-style `TestFoo` / JS-style `it("...")` test reference
//   - a measurable threshold (e.g. `< 50ms`, `>= 99%`)
//
// The patterns are deliberately permissive — the goal is to flag the
// `"fix the bug"` and `"works correctly"` shape, not to grade prose
// quality. Returns nil when every AC is verifiable. I-511.
func ValidateACs(acs []string) []ACFinding {
	var findings []ACFinding
	for i, ac := range acs {
		trimmed := strings.TrimSpace(ac)
		if trimmed == "" {
			findings = append(findings, ACFinding{
				Index:  i + 1,
				AC:     ac,
				Reason: "empty AC — drop or replace with a concrete check",
			})
			continue
		}
		if isVerifiable(trimmed) {
			continue
		}
		findings = append(findings, ACFinding{
			Index:  i + 1,
			AC:     trimmed,
			Reason: "not verifiable — prefix with `cmd:` (e.g. `cmd: go test ./internal/foo/...`), name the test that proves it (e.g. `TestFoo passes`), or include a measurable threshold (e.g. `< 50ms`, `returns 200`)",
		})
	}
	return findings
}

// suiteNames are the TheraPrac Tier-1/Tier-2 suite identifiers + a few
// common cross-language test runners. Matches case-insensitively.
var suiteNames = []string{
	"api_unit", "api_lint", "api_integration",
	"web_typecheck", "web_unit", "web_integration", "web_e2e",
	"bats", "go test", "pytest", "jest", "vitest", "playwright",
}

// assertionVerbs are the action words that signal an AC is observably
// testable. Match is whole-word, case-insensitive. A single match is
// enough to consider the AC verifiable.
var assertionVerbs = []string{
	"returns", "exits", "equals", "contains", "matches", "asserts",
	"outputs", "produces", "blocks", "rejects", "accepts", "surfaces",
	"emits", "fails", "passes", "succeeds", "denies", "allows",
	"renders", "displays", "shows",
}

// goTestPattern matches Go test names like `TestFoo` or `TestFoo_Bar`.
var goTestPattern = regexp.MustCompile(`\bTest[A-Z]\w*`)

// thresholdPattern matches measurable thresholds: `< 50ms`, `>= 99%`,
// `> 100`, `<= 5s`, `~ 200`, `200 OK`, etc. Permissive on whitespace
// and unit suffix.
var thresholdPattern = regexp.MustCompile(`(?i)([<>]=?|~)\s*\d+|status\s+\d{3}|\b\d{3}\s+(ok|created|accepted|no\s+content|bad\s+request|unauthorized|forbidden|not\s+found)\b`)

// jsTestPattern matches JavaScript test-name calls: `it("...")`,
// `test("...")`, `describe("...")`. Single-quoted variants too.
var jsTestPattern = regexp.MustCompile(`\b(it|test|describe)\s*\(\s*['"]`)

func isVerifiable(ac string) bool {
	lower := strings.ToLower(ac)

	// cmd: prefix — explicit executable check.
	if strings.HasPrefix(lower, "cmd:") {
		return true
	}

	// Recognized suite names.
	for _, suite := range suiteNames {
		if strings.Contains(lower, suite) {
			return true
		}
	}

	// Assertion verbs (whole-word match).
	for _, verb := range assertionVerbs {
		if containsWord(lower, verb) {
			return true
		}
	}

	// Go / JS test references.
	if goTestPattern.MatchString(ac) {
		return true
	}
	if jsTestPattern.MatchString(ac) {
		return true
	}

	// Measurable thresholds.
	if thresholdPattern.MatchString(ac) {
		return true
	}

	return false
}

// containsWord reports whether word appears in haystack as a whole
// word (bordered by start-of-string, end-of-string, or non-word char).
// Avoids matching `accepts` inside `unaccepting` and similar.
func containsWord(haystack, word string) bool {
	idx := 0
	for {
		i := strings.Index(haystack[idx:], word)
		if i < 0 {
			return false
		}
		i += idx
		left := i == 0 || !isWordChar(haystack[i-1])
		right := i+len(word) == len(haystack) || !isWordChar(haystack[i+len(word)])
		if left && right {
			return true
		}
		idx = i + 1
	}
}

func isWordChar(b byte) bool {
	return (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '_'
}
