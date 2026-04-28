package command

import (
	"fmt"
	"os"
	"regexp"

	"github.com/jfinlinson/agent-state/internal/agent"
	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/store"
)

// SpawnChildOpts holds inputs for `st spawn child <item>`.
type SpawnChildOpts struct {
	// Item is the item id the child will work on. v1 supports only
	// items the parent already claims (same-item spawn shares the
	// parent's worktree per T-312). Different-item spawn is filed as
	// I-452 follow-up.
	Item string
}

// childSuffixRE matches `<base>-<N>` agent ids that the nextSuffix
// scheme produces for child agents — used to infer "caller is already
// a child" when the env-var heritage signal is missing (path-derived
// or local-config-derived identities don't populate ParentID, so the
// id pattern is the only depth signal available).
var childSuffixRE = regexp.MustCompile(`^[A-Za-z0-9._-]+-\d+$`)

// SpawnChild materializes a child agent registration under the
// caller's identity. T-326 / T-312.
//
// Behavior:
//   - Resolves parent identity via cfg.Identity(). Refuses if no
//     identity is bound or if AS_SESSION_ID is empty (a session id
//     is required so the registration's claim guard isn't a no-op).
//   - Enforces the depth-2 cap. The caller is "already a child" when
//     EITHER Identity.ParentID is set (env-var heritage) OR the
//     caller's id matches the `<base>-<N>` suffix pattern (path or
//     local-config heritage that doesn't populate ParentID).
//   - Calls agent.Register with ParentAgentID + RootAgentID set so
//     the child's session events roll up to the root for cost
//     attribution (I-369).
//
// Output: prints `<child-id><TAB><pid>` on stdout so the caller can
// pipe into `cut` / `read` and exec a downstream subprocess with
// AS_AGENT_ID=<child-id>.
//
// IMPORTANT — registration lifetime: the registration's PID is
// os.Getpid() of THIS spawn-child invocation. The process exits
// immediately after printing, so by the time a subsequent agent.Sweep
// runs the PID is dead and the registration gets reaped. Callers must
// adopt the registration promptly via AS_AGENT_ID=<id> in their next
// command. A future enhancement would let callers pass a `--pid <N>`
// to bind the registration to an already-forked child process.
//
// V1 supports SAME-ITEM spawn only (parent's claim covers the child).
// Different-item spawn with a new worktree is filed as I-452.
func SpawnChild(s *store.Store, cfg *config.Config, opts SpawnChildOpts) int {
	if opts.Item == "" {
		fmt.Fprintln(os.Stderr, "spawn child: item id is required")
		return 2
	}

	parent := cfg.Identity()
	if parent.ID == "" {
		fmt.Fprintln(os.Stderr,
			"spawn child: no agent identity in this shell. "+
				"Set AS_AGENT_ID, run from a per-agent dir, or write "+
				".as/local-agent.yaml.")
		return 1
	}

	// Refuse without a session id. parentSession ends up in both the
	// claim guard below and the SpawnedBySession field of the new
	// registration; an empty session would silently bypass scope
	// collision detection between zero-session agents.
	parentSession := cfg.SessionID()
	if parentSession == "" {
		fmt.Fprintln(os.Stderr,
			"spawn child: no AS_SESSION_ID set. A session id is required "+
				"so the parent's claim is unambiguous.")
		return 1
	}

	// Depth-2 policy. Two signals:
	//  - ParentID set via AS_AGENT_PARENT_ID env var (explicit
	//    spawn-from-spawn).
	//  - Id pattern `<base>-<N>` (caller's identity came from a path
	//    like `theraprac-agent-a-1`, where Identity() doesn't
	//    populate ParentID but the suffix already encodes child-ness).
	if parent.ParentID != "" || childSuffixRE.MatchString(parent.ID) {
		stated := parent.ParentID
		if stated == "" {
			stated = "<inferred from id>"
		}
		fmt.Fprintf(os.Stderr,
			"spawn child: %s is already a child (parent=%s) — depth-2 cap reached. "+
				"Spawn from the root agent instead.\n",
			parent.ID, stated)
		return 1
	}

	item, ok := s.Get(opts.Item)
	if !ok {
		fmt.Fprintf(os.Stderr, "spawn child: item %s not found\n", opts.Item)
		return 1
	}
	if item.ClaimedBy != "" && item.ClaimedBy != parentSession {
		fmt.Fprintf(os.Stderr,
			"spawn child: %s is claimed by session %s, not by parent session %s\n",
			opts.Item, item.ClaimedBy, parentSession)
		return 1
	}

	rootID := parent.RootID
	if rootID == "" {
		rootID = parent.ID
	}

	// I-326: deliberately do NOT defer the cleanup. The registration
	// must outlive this short-lived spawn-child invocation so the
	// caller's downstream subprocess can adopt the chain via
	// AS_AGENT_ID=<reg.AgentID>. agent.Sweep reclaims the file when
	// the registered PID is no longer alive — that's expected
	// turnover, not a leak. (Diverges from start.go/run.go where the
	// registration's lifecycle matches the long-running process.)
	reg, _, err := agent.Register(cfg, agent.Options{
		BaseAgentID:      parent.ID,
		ParentAgentID:    parent.ID,
		RootAgentID:      rootID,
		Role:             "child",
		SessionID:        parentSession,
		SpawnedBySession: parentSession,
		Scope:            "item:" + opts.Item,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "spawn child: register: %v\n", err)
		return 1
	}

	fmt.Printf("%s\t%d\n", reg.AgentID, reg.PID)
	return 0
}
