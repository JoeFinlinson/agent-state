package plan

import (
	"fmt"
	"regexp"
	"strings"

	"mvdan.cc/sh/v3/syntax"
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

	// Pass 1: verifiability — each AC must have a recognizable proof method.
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

	// Pass 2: portability — cmd: ACs must not contain bare workspace-relative
	// paths. "agent-state/" and "theraprac-workspace/" only resolve from the
	// main workspace root; they silently fail when UAT runs from a worktree
	// base. The UAT runner injects $ST_WORKSPACE_ROOT (absolute) so authors
	// can write portable file checks that work in any run context.
	for i, ac := range acs {
		trimmed := strings.TrimSpace(ac)
		if !strings.HasPrefix(strings.ToLower(trimmed), "cmd:") {
			continue
		}
		if hasBareWorkspacePath(trimmed[4:]) {
			findings = append(findings, ACFinding{
				Index:  i + 1,
				AC:     trimmed,
				Reason: `non-portable workspace path — replace "agent-state/" or "theraprac-workspace/" with "$ST_WORKSPACE_ROOT/agent-state/" or "$ST_WORKSPACE_ROOT/theraprac-workspace/" so the check resolves from any run context`,
			})
		}
	}

	// Pass 3: hollow / false-pass detection (I-933). A full-corpus audit of
	// the plan-review sub-agent showed a recurring shape it kept catching —
	// an AC that "passes" without exercising the behavior it claims, because
	// its result is always 0 (headline case: a trailing `|| true`).
	//
	// Correctness is the governing constraint (I-1478): a false positive
	// hard-blocks a valid plan with NO override path, which is strictly worse
	// than the latency this gate removes. Regex/tokenizer approximations of
	// "always exits 0" produced false positives on escaped quotes, command
	// substitution, and redirects, so this uses a real shell AST parser
	// (mvdan.cc/sh — the shfmt engine) and walks the exit-status structure on
	// proper nodes. See acAlwaysZeroExit.
	for i, ac := range acs {
		trimmed := strings.TrimSpace(ac)
		if !strings.HasPrefix(strings.ToLower(trimmed), "cmd:") {
			continue
		}
		cmd := strings.TrimSpace(trimmed[4:])
		if acAlwaysZeroExit(cmd) {
			findings = append(findings, ACFinding{
				Index:  i + 1,
				AC:     trimmed,
				Reason: "hollow AC — always exits 0 regardless of the real result (a no-op like `true`/`echo`, or its result is masked: `|| true`, `; true`, `| true`, `; exit 0`). Make the AC fail when the behavior is wrong (run a test, grep with non-zero-on-absence, or compare output)",
			})
		}
	}

	return findings
}

// acAlwaysZeroExit reports whether a cmd: AC can never exit non-zero — i.e. it
// is structurally a no-op or its result is masked, so it "passes" without
// testing anything (I-933). It parses the command into a real shell AST
// (mvdan.cc/sh) and walks the exit-status structure, which stays correct under
// quoting, escapes, command substitution, and redirects that defeated the
// earlier regex/tokenizer approximations.
//
// It is deliberately conservative: anything it cannot PROVE always-zero (an
// unknown command head, a redirect that could itself fail, a subshell / loop /
// conditional, or a parse error) is treated as "can fail" and NOT flagged, so
// a false positive never hard-blocks a valid plan (I-1478). The residual
// runtime-only gaps — `printf '%d'` failing, `| cat` swallowing a status, a
// redirect that in practice never fails — are left to the opt-in `--review`.
func acAlwaysZeroExit(cmd string) bool {
	f, err := syntax.NewParser().Parse(strings.NewReader(cmd), "")
	if err != nil || f == nil || len(f.Stmts) == 0 {
		return false
	}
	// A shell script's exit status is that of its last statement.
	return !stmtCanFail(f.Stmts[len(f.Stmts)-1])
}

// stmtCanFail reports whether a statement can produce a non-zero exit status.
func stmtCanFail(st *syntax.Stmt) bool {
	if st == nil {
		return true
	}
	// `! cmd` can yield non-zero; any redirect on the statement can fail
	// (permission, missing target dir) — treat both as failable.
	if st.Negated || len(st.Redirs) > 0 {
		return true
	}
	return cmdCanFail(st.Cmd)
}

// cmdCanFail walks the exit-status structure of a command node.
func cmdCanFail(cmd syntax.Command) bool {
	switch c := cmd.(type) {
	case *syntax.CallExpr:
		return !callAlwaysZero(c)
	case *syntax.BinaryCmd:
		switch c.Op {
		case syntax.Pipe, syntax.PipeAll:
			return stmtCanFail(c.Y) // exit = last pipeline stage (no pipefail)
		case syntax.AndStmt: // &&
			return stmtCanFail(c.X) || stmtCanFail(c.Y)
		case syntax.OrStmt: // ||
			return stmtCanFail(c.X) && stmtCanFail(c.Y)
		}
		return true
	default:
		// Subshell, block, if/for/while/case, etc. — not provably zero.
		return true
	}
}

// alwaysZeroHeads are command words whose exit status is reliably 0 regardless
// of the work an AC claims to verify. `printf` is excluded (`printf '%d' x`
// exits non-zero); `cat`/`pwd` are excluded (`cat missing` / a deleted cwd can
// fail) — keeping the set conservative so the gate never false-positives.
var alwaysZeroHeads = map[string]bool{"true": true, ":": true, "echo": true}

// callAlwaysZero reports whether a simple command always exits 0.
func callAlwaysZero(c *syntax.CallExpr) bool {
	if c == nil || len(c.Args) == 0 {
		return false // bare assignment / empty — can fail (cmdsubst, etc.)
	}
	head, ok := wordLit(c.Args[0])
	if !ok {
		return false // non-literal head (variable / cmdsubst) — unknown
	}
	if head == "exit" {
		// `exit 0` always succeeds; `exit <n>` / bare exit can be non-zero.
		if len(c.Args) >= 2 {
			if v, ok := wordLit(c.Args[1]); ok {
				return v == "0"
			}
		}
		return false
	}
	return alwaysZeroHeads[head]
}

// wordLit returns the literal string of a word when it is a single unquoted
// literal (e.g. `echo`, `true`, `exit`); ok is false for anything else
// (quoted, expanded, or command-substituted words).
func wordLit(w *syntax.Word) (string, bool) {
	if w == nil || len(w.Parts) != 1 {
		return "", false
	}
	lit, ok := w.Parts[0].(*syntax.Lit)
	if !ok {
		return "", false
	}
	return lit.Value, true
}

// bareWorkspacePathPatterns are substrings whose presence in a cmd: AC
// (without $ST_WORKSPACE_ROOT) indicates a non-portable workspace-relative
// path that silently breaks in worktree UAT runs.
var bareWorkspacePathPatterns = []string{
	"agent-state/",
	"theraprac-workspace/",
}

func hasBareWorkspacePath(cmd string) bool {
	if strings.Contains(cmd, "$ST_WORKSPACE_ROOT") {
		return false
	}
	for _, p := range bareWorkspacePathPatterns {
		if strings.Contains(cmd, p) {
			return true
		}
	}
	return false
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
//
// Note: `passes` / `succeeds` were intentionally NOT included here
// because they're commonly used in vague prose ("the feature passes
// review", "everything succeeds"). The verifiable case
// `"TestFoo passes"` is already covered by goTestPattern, which
// matches the named test reference itself.
var assertionVerbs = []string{
	"returns", "exits", "equals", "contains", "matches", "asserts",
	"outputs", "produces", "blocks", "rejects", "accepts", "surfaces",
	"emits", "fails", "denies", "allows",
	"renders", "displays", "shows",
}

// goTestPattern matches Go test names like `TestFoo` or `TestFoo_Bar`.
var goTestPattern = regexp.MustCompile(`\bTest[A-Z]\w*`)

// thresholdPattern matches measurable thresholds. The number must
// carry a unit (or `%`) so vague comparisons like `"errors > 0"` or
// `"coverage > 0%"` aren't treated as testable — both halves of a
// real threshold (the comparator AND a quantifier) must be present.
//
// Recognized shapes:
//   - `<NN[unit]`, `>= NN[unit]`, etc. where unit is a 1-3 letter
//     suffix (ms, s, kb, mb, ...) or `%`
//   - HTTP status references: `status 200`, `200 OK`, `404 Not Found`
var thresholdPattern = regexp.MustCompile(
	// Comparator + number + (% OR unit-suffix-with-word-boundary).
	// `%` is non-word so \b after it doesn't trigger; place \b inside
	// the alternation to anchor only the unit-suffix branch.
	`(?i)(?:[<>]=?|~)\s*\d+(?:\.\d+)?\s*(?:%|[a-z]{1,3}\b)` +
		`|status\s+\d{3}` +
		`|\b\d{3}\s+(?:ok|created|accepted|no\s+content|bad\s+request|unauthorized|forbidden|not\s+found)\b`)

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
// word (bordered by start-of-string, end-of-string, or non-word
// rune). Avoids matching `accepts` inside `unaccepting` and similar.
//
// Operates on runes (not bytes) so multibyte UTF-8 input — em-dashes,
// accented characters, curly quotes — produces correct boundary
// decisions. word itself is assumed to be ASCII (the assertionVerbs
// list is all ASCII).
func containsWord(haystack, word string) bool {
	hayRunes := []rune(haystack)
	wordRunes := []rune(word)
	if len(wordRunes) == 0 || len(hayRunes) < len(wordRunes) {
		return false
	}
	for i := 0; i+len(wordRunes) <= len(hayRunes); i++ {
		match := true
		for j, r := range wordRunes {
			if hayRunes[i+j] != r {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		left := i == 0 || !isWordRune(hayRunes[i-1])
		right := i+len(wordRunes) == len(hayRunes) || !isWordRune(hayRunes[i+len(wordRunes)])
		if left && right {
			return true
		}
	}
	return false
}

func isWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= '0' && r <= '9') ||
		r == '_'
}
