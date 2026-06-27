package command

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/theraprac/agent-state/internal/store"
)

// nonStateResidue is one parkable unit. specs holds every pathspec that must be
// stashed together to leave the tree clean — for a rename that is BOTH the new
// path and the old (staged-deleted) path, so the index deletion does not linger
// and re-block the gate (I-1594 review finding #3).
type nonStateResidue struct {
	path   string // primary path, used in the stash label
	specs  []string
	rename bool // staged rename — unstage before stashing (see below)
}

// NonStateStash parks uncommitted NON-state residue (scripts/, docs/, etc.)
// left in the SHARED theraprac-workspace main checkout by a peer, so it cannot
// silently block the next agent's state sync (I-1594). Two failure modes it
// clears: (A) staged non-state edits that trip checkNonStateGate and refuse
// `st sync`; (B) untracked non-state files that block the session-start
// `git pull --ff-only` ("untracked working tree files would be overwritten").
//
// Mirrors OrphanStash, with two deliberate differences:
//
//   - It is a STRICT NO-OP unless the checkout is on main/master. Feature-branch
//     worktrees carry the agent's own legitimate uncommitted non-state WIP; only
//     the shared main checkout should never hold non-state dirt. This branch
//     guard — NOT a per-file ownership check — is the peer-WIP-protection
//     boundary. It is why this command intentionally captures untracked (`??`)
//     and unstaged (` M`) files that checkNonStateGate deliberately skips
//     (I-442/I-1472): on the shared main checkout those are residue, and the
//     untracked ones are exactly failure-mode B.
//   - It captures untracked files too (git stash push -u).
//
// Path classification reuses the gate's own store.IsManagedStatePath so the set
// of "non-state" paths stashed here is identical to the set checkNonStateGate
// blocks on — no classifier drift (I-1594 review findings #2/#5/#6/#8).
//
// Nothing is deleted — every parked file is recoverable via `git stash` /
// `st orphan list`. Best-effort: any git error logs to stderr and processing
// continues; it never aborts startup.
func NonStateStash(workspaceRoot, itemsPrefix, agentID string) []string {
	// Branch guard: only the shared main checkout. symbolic-ref returns
	// refs/heads/<branch> on a branch, non-zero on detached HEAD. A detached
	// HEAD (mid-rebase/merge) deliberately no-ops — never mutate a checkout
	// that is mid-operation (I-1594 review finding #7: fail-safe).
	refOut, refErr := execGitOrphan(workspaceRoot, "symbolic-ref", "-q", "HEAD")
	if refErr != nil {
		return nil
	}
	branch := strings.TrimPrefix(strings.TrimSpace(string(refOut)), "refs/heads/")
	if branch != "main" && branch != "master" {
		return nil // feature branch — legitimate non-state WIP, leave it
	}

	// Flat layout (items root == git toplevel, Paths.Root "." or ""): the gate
	// fail-opens (no items-vs-non-items surface to enforce), so there is no
	// non-state residue to clear — mirror that and no-op, rather than treating
	// agent-state item files as residue (I-1594 review finding #5).
	itemsPrefix = strings.TrimSpace(itemsPrefix)
	if itemsPrefix == "" || itemsPrefix == "." || itemsPrefix == "./" {
		return nil
	}
	if !strings.HasSuffix(itemsPrefix, "/") {
		itemsPrefix += "/"
	}

	// -z: NUL-terminated, raw bytes, no path quoting. Rename/copy entries
	// arrive as two NUL tokens: "<XY> <new>\0<old>\0".
	// --untracked-files=all: list individual untracked FILES, not collapsed
	// directories, so per-file classification (and per-file stashing) works for
	// wholly-untracked dirs like docs/ or scripts/.
	out, err := execGitOrphan(workspaceRoot, "status", "--porcelain", "-z", "--untracked-files=all")
	if err != nil || len(out) == 0 {
		return nil
	}

	var items []nonStateResidue
	seen := make(map[string]bool)
	tokens := strings.Split(string(out), "\x00")
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i]
		if len(tok) < 4 {
			continue
		}
		code := tok[:2]
		path := tok[3:]
		// Rename/copy: the OLD path is the next token (no XY prefix).
		oldPath := ""
		if code[0] == 'R' || code[0] == 'C' {
			if i+1 < len(tokens) {
				oldPath = tokens[i+1]
				i++ // consume the old-path token
			}
		}
		if path == "" || seen[path] {
			continue
		}
		// Leave agent-state (.as/ + itemsPrefix) for OrphanStash's ownership-
		// aware handling — identical rule to the gate.
		if store.IsManagedStatePath(path, itemsPrefix) {
			continue
		}
		seen[path] = true
		specs := []string{path}
		isRename := false
		// For a rename, also stash the old path so its staged deletion does not
		// linger in the index and re-block the gate (finding #3). Skip when the
		// old path is itself managed-state (a rename OUT of agent-state) —
		// stashing only the new side still clears the non-state mutation.
		if oldPath != "" && !seen[oldPath] && !store.IsManagedStatePath(oldPath, itemsPrefix) {
			specs = append(specs, oldPath)
			seen[oldPath] = true
			isRename = true
		}
		items = append(items, nonStateResidue{path: path, specs: specs, rename: isRename})
	}

	today := time.Now().UTC().Format("2006-01-02")
	var stashed []string
	for _, r := range items {
		label := fmt.Sprintf("st-nonstate-residue: %s dropped-by:%s date:%s",
			r.path, agentID, today)
		// A staged rename cannot be stashed by naming the deleted old path as a
		// pathspec ("did not match any files"). Unstage the rename first so it
		// decomposes into a worktree deletion (old) + untracked file (new),
		// which `git stash push -u -- <paths>` then captures uniformly, leaving
		// the index clean (finding #3).
		if r.rename {
			if _, rErr := execGitOrphanCapture(workspaceRoot, append([]string{"reset", "-q", "--"}, r.specs...)...); rErr != nil {
				fmt.Fprintf(os.Stderr, "nonstate-stash: failed to unstage rename %s: %v\n", r.path, rErr)
				continue
			}
		}
		// -u captures untracked paths (failure-mode B); harmless for tracked.
		args := append([]string{"stash", "push", "-u", "-m", label, "--"}, r.specs...)
		if _, stashErr := execGitOrphanCapture(workspaceRoot, args...); stashErr != nil {
			fmt.Fprintf(os.Stderr, "nonstate-stash: failed to stash %s: %v\n", r.path, stashErr)
			continue
		}
		stashed = append(stashed, r.path)
	}

	if len(stashed) > 0 {
		fmt.Printf("nonstate-stash: parked %d non-state file(s) from the shared main checkout (dropped-by %s):\n", len(stashed), agentID)
		for _, p := range stashed {
			fmt.Printf("  %s\n", p)
		}
		// Each file is parked in its OWN git stash. Do NOT print stash@{N} refs
		// here — every push shifts earlier stashes down, so a ref captured at
		// push time is stale by the end (finding #4). `st orphan list` reads the
		// live stash list and prints the authoritative ref per file.
		fmt.Printf("  each is parked in its own git stash labeled 'st-nonstate-residue: <path>'\n")
		fmt.Printf("  recover: st orphan list --workspace %q   (then git -C %q stash apply <ref>)\n", workspaceRoot, workspaceRoot)
	}
	return stashed
}
