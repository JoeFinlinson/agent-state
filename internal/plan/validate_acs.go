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
	// the plan-review sub-agent showed ~half its value was catching one
	// recurring shape — an AC that exits 0 regardless of the real result, so
	// it "passes" without exercising the behavior it claims to verify. That
	// pattern is mechanizable, moving it from a 4-6min LLM re-explore into
	// this <1s deterministic gate.
	//
	// Correctness is the governing constraint (I-1478): a false positive
	// hard-blocks a valid plan with no override, which is worse than the
	// latency this gate removes. So acExitAlwaysZero is shell-aware — it
	// tokenizes quote-respectingly and evaluates the &&/||/;/| operator chain
	// by real exit-code semantics, rather than regex-matching tokens that may
	// live inside quoted arguments. The fuzzier semantic cases (a
	// `go test -run X` filter that matches zero tests; a disabled test inside
	// a spec file the AC text never shows) are deliberately NOT flagged — a
	// static check cannot judge them without false-flagging good ACs; that is
	// what the opt-in `--review` sub-agent is for.
	for i, ac := range acs {
		trimmed := strings.TrimSpace(ac)
		if !strings.HasPrefix(strings.ToLower(trimmed), "cmd:") {
			continue
		}
		cmd := strings.TrimSpace(trimmed[4:])
		if acExitAlwaysZero(cmd) {
			findings = append(findings, ACFinding{
				Index:  i + 1,
				AC:     trimmed,
				Reason: "hollow AC — this command exits 0 regardless of the real result (failure masked by a `|| <no-op>` / `; <no-op>` / `| <no-op>` terminal, or no command that can actually fail). Make the AC fail when the behavior is wrong (run a test, grep with non-zero-on-absence, or compare output)",
			})
		}
	}

	return findings
}

// alwaysZeroExitHeads are command heads whose exit status is 0 regardless of
// the work an AC claims to verify — a segment headed by one of these (and
// with no redirection that could itself fail) asserts nothing. `cd`/`export`
// are intentionally absent: `cd missing` / `export` can legitimately fail and
// be the real assertion (I-933 review false-positive fix).
var alwaysZeroExitHeads = map[string]bool{
	"true": true, ":": true, "echo": true, "printf": true,
	"pwd": true, "sleep": true,
}

// acExitAlwaysZero reports whether a cmd: AC always exits 0 — i.e. no shell
// path through it can produce a non-zero status, so it can never fail.
//
// It splits the command into top-level segments (quote-aware) and walks the
// joining operators by their real exit semantics:
//
//	A ; B   → exit B            (A's status discarded)
//	A | B   → exit B            (no pipefail; A's status discarded)
//	A && B  → exit A if A!=0 else B
//	A || B  → 0 if A==0 else B  (B masks A's failure)
//
// An AC is hollow when, after the full chain, no segment that can fail
// determines the result.
func acExitAlwaysZero(cmd string) bool {
	segs, ops := splitTopLevel(cmd)
	sawCmd := false
	nonzeroPossible := false
	for i, seg := range segs {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue // empty / trailing-operator segment
		}
		canFail := !segmentAlwaysZero(seg)
		if !sawCmd {
			sawCmd = true
			nonzeroPossible = canFail
			continue
		}
		switch ops[i] {
		case ";", "|":
			nonzeroPossible = canFail // left status discarded
		case "&&":
			nonzeroPossible = nonzeroPossible || canFail
		case "||":
			nonzeroPossible = nonzeroPossible && canFail
		default:
			nonzeroPossible = canFail
		}
	}
	if !sawCmd {
		return false
	}
	return !nonzeroPossible
}

// segmentAlwaysZero reports whether a single command segment's exit status is
// always 0. True only when its head is in alwaysZeroExitHeads AND it carries
// no unquoted redirection (`>`/`<` can fail on permission / missing dir, so a
// redirecting segment can in fact fail).
func segmentAlwaysZero(seg string) bool {
	if hasUnquotedRedirect(seg) {
		return false
	}
	fields := strings.Fields(seg)
	if len(fields) == 0 {
		return false
	}
	head := strings.ToLower(fields[0])
	// `exit 0` always succeeds; `exit <n>` / bare `exit` (exits last status)
	// can be non-zero, so they are real terminals.
	if head == "exit" {
		return len(fields) >= 2 && fields[1] == "0"
	}
	return alwaysZeroExitHeads[head]
}

// hasUnquotedRedirect reports whether seg contains a `<` or `>` outside of
// single/double quotes (a real shell redirection, not literal text).
func hasUnquotedRedirect(seg string) bool {
	var quote rune
	for _, r := range seg {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			}
		case r == '\'' || r == '"':
			quote = r
		case r == '<' || r == '>':
			return true
		}
	}
	return false
}

// splitTopLevel splits a shell command into segments separated by the
// top-level control operators `&&`, `||`, `;`, and `|`, ignoring any that
// appear inside single or double quotes. ops[k] is the operator that precedes
// segs[k] (ops[0] == ""). Escaped quotes and here-docs are not handled — good
// enough for AC linting, where the goal is detecting always-zero-exit shapes.
func splitTopLevel(cmd string) (segs []string, ops []string) {
	var cur strings.Builder
	op := ""
	var quote rune
	runes := []rune(cmd)
	flush := func(next string) {
		segs = append(segs, cur.String())
		ops = append(ops, op)
		cur.Reset()
		op = next
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if quote != 0 {
			cur.WriteRune(r)
			if r == quote {
				quote = 0
			}
			continue
		}
		switch r {
		case '\'', '"':
			quote = r
			cur.WriteRune(r)
		case '&':
			if i+1 < len(runes) && runes[i+1] == '&' {
				flush("&&")
				i++
			} else {
				cur.WriteRune(r)
			}
		case '|':
			if i+1 < len(runes) && runes[i+1] == '|' {
				flush("||")
				i++
			} else {
				flush("|")
			}
		case ';':
			flush(";")
		default:
			cur.WriteRune(r)
		}
	}
	segs = append(segs, cur.String())
	ops = append(ops, op)
	return segs, ops
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
