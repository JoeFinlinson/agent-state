package command

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/pricing"
)

// PricingRefreshOpts controls st pricing refresh behaviour.
type PricingRefreshOpts struct {
	DryRun    bool
	SanityPct float64 // max allowed per-field % change; default 50

	// TablePath overrides the default resolved path to table.go (for tests).
	TablePath string
	// AsDir overrides the as-repo root used for go build and git ops (for tests).
	AsDir string
	// Fetcher overrides the live HTTP fetch (for tests).
	Fetcher func(*http.Client) (map[string]pricing.Rate, error)
	// RunCmd overrides os/exec command execution (for tests).
	RunCmd func(dir string, args ...string) error
}

// PricingRefresh implements `st pricing refresh`: fetches Anthropic pricing,
// diffs against the hardcoded table, and auto-commits updates within the
// sanity bound. Returns a shell exit code (0 = success, 1 = error/blocked).
func PricingRefresh(cfg *config.Config, opts PricingRefreshOpts) int {
	if opts.SanityPct == 0 {
		opts.SanityPct = 50.0
	}

	// Resolve paths
	asDir := opts.AsDir
	if asDir == "" {
		root := cfg.AgentRoot()
		if root == "" {
			fmt.Fprintln(os.Stderr, "pricing refresh: cannot resolve agent root")
			return 1
		}
		asDir = filepath.Join(root, "as")
	}
	tablePath := opts.TablePath
	if tablePath == "" {
		tablePath = filepath.Join(asDir, "internal", "pricing", "table.go")
	}

	// Fetch live rates
	fetcher := opts.Fetcher
	if fetcher == nil {
		fetcher = pricing.FetchAnthropicRates
	}
	fetched, err := fetcher(nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pricing refresh: %v\n", err)
		return 1
	}

	// Diff against hardcoded table
	diffs := pricing.DiffRates(pricing.KnownRates(), fetched)
	fmt.Print(pricing.FormatDiff(diffs))
	if len(diffs) == 0 {
		return 0
	}

	// Resolve the command runner once for all subsequent exec operations.
	runner := opts.RunCmd
	if runner == nil {
		runner = runExec
	}

	// Sanity check — block if any field changed >SanityPct%
	if !pricing.SanityCheck(diffs, opts.SanityPct) {
		fmt.Fprintf(os.Stderr, "pricing refresh: rate change exceeds %.0f%% sanity bound — filing issue for manual review\n", opts.SanityPct)
		_ = createSanityIssue(pricing.FormatDiff(diffs), runner)
		return 1
	}

	if opts.DryRun {
		fmt.Println("dry run — no files modified")
		return 0
	}

	// Rewrite table.go
	src := pricing.RenderTable(fetched)
	if err := os.WriteFile(tablePath, []byte(src), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "pricing refresh: write table.go: %v\n", err)
		return 1
	}

	// Verify the generated file compiles
	if err := runner(asDir, "go", "build", "./..."); err != nil {
		fmt.Fprintf(os.Stderr, "pricing refresh: go build failed after table update: %v — restoring original\n", err)
		// Best-effort restore; ignore error
		_ = exec.Command("git", "-C", asDir, "checkout", "--", tablePath).Run()
		return 1
	}

	// Commit and push
	if err := runner(asDir, "git", "add", tablePath); err != nil {
		fmt.Fprintf(os.Stderr, "pricing refresh: git add: %v\n", err)
		return 1
	}
	commitMsg := "chore: update pricing table via st pricing refresh"
	if err := runner(asDir, "git", "commit", "-m", commitMsg); err != nil {
		fmt.Fprintf(os.Stderr, "pricing refresh: git commit: %v\n", err)
		return 1
	}
	if err := runner(asDir, "git", "push", "origin", "HEAD"); err != nil {
		fmt.Fprintf(os.Stderr, "pricing refresh: git push: %v\n", err)
		return 1
	}

	fmt.Println("pricing table updated, committed, and pushed")
	return 0
}

// runExec runs a command in dir, streaming stdout/stderr to the terminal.
// An empty dir uses the current working directory.
func runExec(dir string, args ...string) error {
	if len(args) == 0 {
		return fmt.Errorf("runExec: no command")
	}
	cmd := exec.Command(args[0], args[1:]...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// createSanityIssue files a GitHub issue in JoeFinlinson/agent-state with the
// diff body using the provided runner. Failures are non-fatal — the command
// still returns 1. Pass runExec as runner in production.
func createSanityIssue(diffBody string, runner func(dir string, args ...string) error) error {
	title := "Pricing sanity: >50% rate change detected — manual review required"
	body := "Automated `st pricing refresh` detected a rate change exceeding the 50% sanity bound.\n\n```\n" + strings.TrimSpace(diffBody) + "\n```\n\nVerify against https://docs.anthropic.com/en/docs/about-claude/pricing and update table.go manually."
	return runner("", "gh", "issue", "create",
		"--repo", "JoeFinlinson/agent-state",
		"--title", title,
		"--body", body,
	)
}
