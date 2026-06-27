package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// addUntrackedFile writes dir/rel without staging or committing it, so git
// status --porcelain shows it as untracked (??).
func addUntrackedFile(t *testing.T, repoDir, rel, content string) {
	t.Helper()
	full := filepath.Join(repoDir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

// gitPorcelain returns `git status --porcelain` for repoDir (whole tree).
func gitPorcelain(t *testing.T, repoDir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// checkoutBranch creates and switches to a feature branch in repoDir.
func checkoutBranch(t *testing.T, repoDir, branch string) {
	t.Helper()
	if out, err := exec.Command("git", "-C", repoDir, "checkout", "-b", branch).CombinedOutput(); err != nil {
		t.Fatalf("git checkout -b %s: %v\n%s", branch, err, out)
	}
}

// currentBranchName returns the checked-out branch (initGitRepo's default may
// be main or master depending on the host git config).
func currentBranchName(t *testing.T, repoDir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repoDir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

const nsItemDir = "agent-state"
const nsAgent = "agent-a"

func TestNonStateStash_StashesStagedNonStateOnMain(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// A tracked, staged non-state edit — this is the case that trips the
	// checkNonStateGate and refuses `st sync` (failure-mode A).
	addTrackedDirtyFile(t, dir, "scripts/foo.py", "print('changed')\n")
	if out, err := exec.Command("git", "-C", dir, "add", "scripts/foo.py").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}

	stashed := NonStateStash(dir, nsItemDir, nsAgent)
	if len(stashed) != 1 {
		t.Fatalf("expected 1 stash, got %d: %v", len(stashed), stashed)
	}
	if !strings.Contains(stashed[0], "scripts/foo.py") {
		t.Errorf("stash should reference scripts/foo.py; got %q", stashed[0])
	}
	if gitPorcelain(t, dir) != "" {
		t.Errorf("tree should be clean after stash; git status: %q", gitPorcelain(t, dir))
	}
	// The stash carries attribution.
	out, _ := exec.Command("git", "-C", dir, "stash", "list").Output()
	if !strings.Contains(string(out), "st-nonstate-residue: scripts/foo.py dropped-by:"+nsAgent) {
		t.Errorf("stash label missing attribution; got:\n%s", out)
	}
}

func TestNonStateStash_StashesUntrackedNonStateOnMain(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// An untracked non-state file — this is what blocks the session-start
	// `git pull --ff-only` (failure-mode B). The agent-state gate skips it,
	// so only this stash clears it.
	addUntrackedFile(t, dir, "docs/junk.md", "redundant\n")

	stashed := NonStateStash(dir, nsItemDir, nsAgent)
	if len(stashed) != 1 {
		t.Fatalf("expected 1 stash for untracked file, got %d: %v", len(stashed), stashed)
	}
	if !strings.Contains(stashed[0], "docs/junk.md") {
		t.Errorf("stash should reference docs/junk.md; got %q", stashed[0])
	}
	if gitPorcelain(t, dir) != "" {
		t.Errorf("tree should be clean after stashing untracked file; git status: %q", gitPorcelain(t, dir))
	}
}

func TestNonStateStash_NoopOffMain(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	checkoutBranch(t, dir, "fix/I-999-feature")

	// Dirty non-state files on a feature branch are the agent's OWN legitimate
	// WIP — must never be stashed.
	addTrackedDirtyFile(t, dir, "scripts/foo.py", "print('wip')\n")
	addUntrackedFile(t, dir, "docs/junk.md", "wip\n")

	stashed := NonStateStash(dir, nsItemDir, nsAgent)
	if len(stashed) != 0 {
		t.Errorf("expected no stashes off main (peer-WIP protection); got %v", stashed)
	}
	if gitPorcelain(t, dir) == "" {
		t.Errorf("feature-branch WIP should remain dirty (not stashed)")
	}
}

func TestNonStateStash_LeavesAgentStateAlone(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Dirty agent-state file — handled by OrphanStash, not this function.
	addTrackedDirtyFile(t, dir, filepath.Join(nsItemDir, "tasks", "T-1.md"), "id: T-1\nstatus: coding\n")

	stashed := NonStateStash(dir, nsItemDir, nsAgent)
	if len(stashed) != 0 {
		t.Errorf("agent-state dirt must be left for OrphanStash; got %v", stashed)
	}
}

func TestNonStateStash_LeavesGitignoredAlone(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Machine-regenerated churn (deploy-dashboard.html, dashboard-history.jsonl)
	// is gitignored in the real workspace, so `git status` never surfaces it —
	// and what the gate never sees, this command must never stash. (We mirror
	// the gate exactly: no special churn-name allowlist, just gitignore.)
	addUntrackedFile(t, dir, ".gitignore", "deploy-dashboard.html\n*.jsonl\n")
	addUntrackedFile(t, dir, "deploy-dashboard.html", "<html></html>\n")
	addUntrackedFile(t, dir, "scripts/dashboard-history.jsonl", "{}\n")

	stashed := NonStateStash(dir, nsItemDir, nsAgent)
	for _, s := range stashed {
		if strings.Contains(s, "dashboard") {
			t.Errorf("gitignored churn must not be stashed; got %v", stashed)
		}
	}
	// deploy-dashboard.html / *.jsonl are gitignored, so only .gitignore itself
	// (a non-state tracked-able file) is visible residue.
	if len(stashed) != 1 || !strings.Contains(stashed[0], ".gitignore") {
		t.Errorf("expected only the untracked .gitignore stashed; got %v", stashed)
	}
}

func TestNonStateStash_StagedRenameClearsBothSides(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Commit a tracked non-state file, then stage a rename of it. The staged
	// deletion of the old path must not linger after stashing (finding #3).
	addUntrackedFile(t, dir, "scripts/old.py", "x\n")
	mustGit(t, dir, "add", "scripts/old.py")
	mustGit(t, dir, "commit", "-m", "add old.py")
	mustGit(t, dir, "mv", "scripts/old.py", "scripts/new.py")

	before := gitPorcelain(t, dir)
	if !strings.Contains(before, "old.py") || !strings.Contains(before, "new.py") {
		t.Fatalf("expected a staged rename; got %q", before)
	}

	stashed := NonStateStash(dir, nsItemDir, nsAgent)
	if len(stashed) != 1 {
		t.Fatalf("expected 1 residue entry for the rename; got %v", stashed)
	}
	if got := gitPorcelain(t, dir); got != "" {
		t.Errorf("tree must be fully clean after stashing a rename (no lingering staged deletion); got %q", got)
	}
}

func TestNonStateStash_NoopFlatLayout(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	// Flat layout: items root == git toplevel (Paths.Root "."). Item files live
	// at the repo root (e.g. issues/I-5.md) and must NOT be treated as residue.
	addUntrackedFile(t, dir, "issues/I-5.md", "id: I-5\n")
	addUntrackedFile(t, dir, "scripts/foo.py", "x\n")

	if stashed := NonStateStash(dir, ".", nsAgent); stashed != nil {
		t.Errorf("flat layout must be a strict no-op (gate fail-opens); got %v", stashed)
	}
	if gitPorcelain(t, dir) == "" {
		t.Errorf("flat-layout files must remain in the working tree")
	}
}

func TestNonStateStash_NoopWhenClean(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	stashed := NonStateStash(dir, nsItemDir, nsAgent)
	if stashed != nil {
		t.Errorf("expected nil on clean tree; got %v", stashed)
	}
}

func TestNonStateStash_Idempotent(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)
	_ = currentBranchName(t, dir) // sanity: ensure we are on a real branch

	addUntrackedFile(t, dir, "docs/junk.md", "redundant\n")

	first := NonStateStash(dir, nsItemDir, nsAgent)
	if len(first) != 1 {
		t.Fatalf("first run should stash 1 file; got %v", first)
	}
	second := NonStateStash(dir, nsItemDir, nsAgent)
	if len(second) != 0 {
		t.Errorf("second run should be a no-op (tree already clean); got %v", second)
	}
}

func TestOrphanList_ShowsNonStateResidue(t *testing.T) {
	dir := t.TempDir()
	initGitRepo(t, dir)

	fakeOutput := `stash@{0}: On main: st-orphan: agent-state/tasks/T-001.md owned-by:agent-b dropped-by:agent-i date:2026-06-14
stash@{1}: On main: st-nonstate-residue: scripts/foo.py dropped-by:agent-a date:2026-06-26
stash@{2}: On main: WIP on main: unrelated`
	orig := execGitOrphan
	defer func() { execGitOrphan = orig }()
	execGitOrphan = func(d string, args ...string) ([]byte, error) {
		if len(args) >= 2 && args[0] == "stash" && args[1] == "list" {
			return []byte(fakeOutput), nil
		}
		cmd := exec.Command("git", args...)
		cmd.Dir = d
		return cmd.Output()
	}

	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	OrphanList(dir)
	w.Close()
	os.Stdout = origStdout
	var buf [4096]byte
	n, _ := r.Read(buf[:])
	output := string(buf[:n])

	if !strings.Contains(output, "st-nonstate-residue: scripts/foo.py") {
		t.Errorf("expected non-state residue stash in list output; got:\n%s", output)
	}
	if !strings.Contains(output, "st-orphan: agent-state/tasks/T-001.md") {
		t.Errorf("expected orphan stash still listed; got:\n%s", output)
	}
	if strings.Contains(output, "WIP on main: unrelated") {
		t.Errorf("unrelated stash should be filtered; got:\n%s", output)
	}
}
