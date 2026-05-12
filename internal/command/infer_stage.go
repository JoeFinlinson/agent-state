package command

import (
	"fmt"
	"os"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/model"
	"github.com/jfinlinson/agent-state/internal/store"
)

// InferStageOpts configures the infer-stage command. Mirrors ReconcileOpts'
// injection pattern so tests can stub branch / PR signals without exec'ing
// git or gh.
type InferStageOpts struct {
	BranchCheck func(*config.Config, string) bool
	PRFetch     func(*config.Config, string) (string, []string)
}

func (o *InferStageOpts) branchCheck() func(*config.Config, string) bool {
	if o.BranchCheck != nil {
		return o.BranchCheck
	}
	return branchExistsOnRemote
}

func (o *InferStageOpts) prFetch() func(*config.Config, string) (string, []string) {
	if o.PRFetch != nil {
		return o.PRFetch
	}
	return getPRState
}

// InferStage probes filesystem / GitHub state for one item and forward-only
// advances delivery.stage. Resolution: explicit id arg, else stack-top.
//
// Probe order (cheapest first):
//
//	1. branch-on-remote                  => ensure >= "pushed"
//	2. gh pr list --state OPEN/MERGED    => "pr_open" / "merged"
//
// Leaves deployed_dev / uat_approved / closed alone — those require AWS
// state or explicit operator action and remain in `st reconcile`'s scope.
//
// Returns 0 on every "nothing to do" path so the stop hook never blocks.
func InferStage(s *store.Store, cfg *config.Config, id string, opts InferStageOpts) int {
	if id == "" {
		entries := LoadStack(cfg)
		if len(entries) == 0 {
			return 0
		}
		id = entries[0].ID
	}

	item, ok := s.Get(id)
	if !ok {
		return 0
	}

	branch, _ := getNestedField(item, "work_tracking", "branch")
	if branch == "" || branch == "null" {
		return 0
	}

	target := ""
	if opts.branchCheck()(cfg, branch) {
		target = "pushed"
	}
	state, _ := opts.prFetch()(cfg, branch)
	switch state {
	case "OPEN":
		target = "pr_open"
	case "MERGED":
		target = "merged"
	}

	if target == "" {
		return 0
	}

	if err := s.Mutate(id, func(it *model.Item) error {
		advanceDeliveryStage(it, target)
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "infer-stage: %v\n", err)
		return 1
	}
	return 0
}
