package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

// runInProcessCapturingStderr is a sibling of runInProcess that also
// captures stderr, used by T-376 to assert on the deprecation banner.
func runInProcessCapturingStderr(t *testing.T, cwd string, args ...string) (stdout, stderr string, code int) {
	t.Helper()

	origStdout, origStderr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW

	exitCode = 0
	os.Setenv("AS_AGENT_ID", "test-agent")
	os.Setenv("AS_SESSION_ID", "test-session")

	app := newApp(cwd)
	app.SetArgs(args)
	app.SetErr(errW)
	_ = app.Execute()

	outW.Close()
	errW.Close()
	os.Stdout, os.Stderr = origStdout, origStderr

	outBuf := &bytes.Buffer{}
	errBuf := &bytes.Buffer{}
	_, _ = io.Copy(outBuf, outR)
	_, _ = io.Copy(errBuf, errR)
	return outBuf.String(), errBuf.String(), exitCode
}

// TestStPrepPrintsDeprecationBanner verifies T-376's deprecation
// posture: invoking `st prep` (top-level alias) emits a one-line
// banner on stderr before dispatching.
func TestStPrepPrintsDeprecationBanner(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	_, stderr, code := runInProcessCapturingStderr(t, ws, "prep", "--dry-run", "T-001")

	if code != 0 {
		t.Fatalf("st prep --dry-run T-001 exit=%d, want 0\nstderr:\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "as: deprecation:") {
		t.Errorf("expected deprecation prefix `as: deprecation:` in stderr; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "st plan prep") {
		t.Errorf("expected deprecation banner to point at `st plan prep`; got:\n%s", stderr)
	}
}

// TestStPlanPrepDoesNotPrintDeprecationBanner confirms the new
// subcommand path does NOT emit the deprecation banner. The banner
// belongs to the alias, not the canonical name.
func TestStPlanPrepDoesNotPrintDeprecationBanner(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	_, stderr, code := runInProcessCapturingStderr(t, ws, "plan", "prep", "--dry-run", "T-001")

	if code != 0 {
		t.Fatalf("st plan prep --dry-run T-001 exit=%d, want 0\nstderr:\n%s", code, stderr)
	}
	if strings.Contains(stderr, "as: deprecation:") {
		t.Errorf("st plan prep should NOT emit deprecation banner; got stderr:\n%s", stderr)
	}
}

// TestPlanPrepSubcommandDispatchesToSameHandlers asserts that
// `st prep <id>` and `st plan prep <id>` invoke the same code path
// — verified by stdout-content parity from the --dry-run announcement.
// If they ever diverge, this test catches it.
func TestPlanPrepSubcommandDispatchesToSameHandlers(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	prepOut, _, prepCode := runInProcessCapturingStderr(t, ws, "prep", "--dry-run", "T-001")
	planPrepOut, _, planPrepCode := runInProcessCapturingStderr(t, ws, "plan", "prep", "--dry-run", "T-001")

	if prepCode != planPrepCode {
		t.Errorf("exit codes diverged: prep=%d, plan prep=%d", prepCode, planPrepCode)
	}
	// Both should announce the same item with the same "Would plan" wording.
	for _, want := range []string{"Would plan", "T-001"} {
		if !strings.Contains(prepOut, want) {
			t.Errorf("`st prep` stdout missing %q:\n%s", want, prepOut)
		}
		if !strings.Contains(planPrepOut, want) {
			t.Errorf("`st plan prep` stdout missing %q:\n%s", want, planPrepOut)
		}
	}
}

// TestStPlanPrepWorksWithStandaloneItem confirms the new subcommand
// hits the PrepStandalone routing path the same way `st prep` does
// (I-571). Standalone = item with no sprint assignment.
func TestStPlanPrepWorksWithStandaloneItem(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	stdout, stderr, code := runInProcessCapturingStderr(t, ws, "plan", "prep", "--dry-run", "T-001")

	if code != 0 {
		t.Fatalf("st plan prep --dry-run T-001 exit=%d, want 0\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
	}
	if strings.Contains(stdout, "has no sprint assigned") {
		t.Errorf("standalone dispatch should not emit legacy error: %s", stdout)
	}
	if !strings.Contains(stdout, "Would plan") {
		t.Errorf("expected 'Would plan' in dry-run stdout, got:\n%s", stdout)
	}
}

// TestStPlanPrepViaItemFlag is the --item flag-form sibling of the
// test above for `st plan prep`.
func TestStPlanPrepViaItemFlag(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	stdout, _, code := runInProcessCapturingStderr(t, ws, "plan", "prep", "--dry-run", "--item", "T-001")

	if code != 0 {
		t.Fatalf("st plan prep --dry-run --item T-001 exit=%d, want 0\nstdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "Would plan") {
		t.Errorf("expected 'Would plan' in stdout, got:\n%s", stdout)
	}
}

// TestStPrepStillWorks confirms the deprecated alias still functions
// (deprecation is announcement-only; the alias does not refuse). This
// is the back-compat guarantee for the one-release deprecation window.
func TestStPrepStillWorks(t *testing.T) {
	ws := setupInProcessWorkspace(t)
	stdout, _, code := runInProcessCapturingStderr(t, ws, "prep", "--dry-run", "T-001")

	if code != 0 {
		t.Fatalf("st prep --dry-run T-001 exit=%d, want 0\nstdout:\n%s", code, stdout)
	}
	if !strings.Contains(stdout, "Would plan") {
		t.Errorf("expected 'Would plan' in stdout, got:\n%s", stdout)
	}
}
