package command

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jfinlinson/agent-state/internal/changelog"
	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/store"
)

// TestRecordOpts holds injectable functions for the test command.
type TestRecordOpts struct {
	// Injectable for testing (nil = use real git)
	GitHeadSHA func(repoDir string) (string, error)
}

// TestRecord records a test suite pass for an item.
// Named TestRecord (not Test) to avoid Go test function naming collision.
func TestRecord(s *store.Store, cfg *config.Config, id, suite string, opts TestRecordOpts) int {
	item, ok := s.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "not found: %s\n", id)
		return 1
	}

	if item.Status != "active" {
		fmt.Fprintf(os.Stderr, "%s is %s — must be active to record test evidence\n", id, item.Status)
		return 1
	}

	// Validate suite exists in config
	if cfg.Testing == nil {
		fmt.Fprintln(os.Stderr, "no testing configuration found")
		return 1
	}

	isRequired := false
	isScope := false
	if _, ok := cfg.Testing.RequiredSuites[suite]; ok {
		isRequired = true
	}
	if _, ok := cfg.Testing.ScopeSuites[suite]; ok {
		isScope = true
	}
	if !isRequired && !isScope {
		fmt.Fprintf(os.Stderr, "unknown suite %q — not in required_suites or scope_suites\n", suite)
		return 1
	}

	// For scope suites, warn if not triggered by st pr
	if isScope {
		current, _ := getNestedField(item, "testing_evidence", suite)
		if current != "required" && !strings.HasPrefix(current, "pass") {
			fmt.Fprintf(os.Stderr, "warning: scope suite %q was not triggered by `st pr` — recording anyway\n", suite)
		}
	}

	// Get HEAD SHA
	sha := "unknown"
	if opts.GitHeadSHA != nil {
		out, err := opts.GitHeadSHA(".")
		if err == nil {
			sha = strings.TrimSpace(out)
		}
	} else {
		out, err := runGit(".", "rev-parse", "HEAD")
		if err == nil {
			sha = strings.TrimSpace(out)
		}
	}
	if len(sha) > 7 {
		sha = sha[:7]
	}

	// Build evidence string
	now := time.Now()
	evidence := fmt.Sprintf("pass %s %s", sha, now.Format(time.RFC3339))

	// Record in testing_evidence
	setNestedField(item, "testing_evidence", suite, evidence)
	item.Doc.SetField("last_touched", now.Format(time.RFC3339))

	if err := s.Write(item); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", id, err)
		return 1
	}

	changelog.Append(cfg, id, changelog.Entry{
		Op:       "test_recorded",
		Field:    "testing_evidence." + suite,
		NewValue: evidence,
	})

	fmt.Printf("Recorded %s pass on %s (sha:%s)\n", suite, id, sha)
	return 0
}
