package command

import (
	"fmt"
	"os"
	"time"

	"github.com/jfinlinson/agent-state/internal/changelog"
	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/model"
	"github.com/jfinlinson/agent-state/internal/plan"
	"github.com/jfinlinson/agent-state/internal/store"
)

// PlanApproveOpts holds flags for `st plan approve`. I-511 added
// Strict, which refuses approval if any linked plan sidecar contains
// an un-verifiable acceptance criterion (per plan.ValidateACs).
type PlanApproveOpts struct {
	Strict bool
}

// PlanApprove marks an item's plan as approved. Sets PlanApproved=true,
// PlanApprovedAt=now, PlanApprovedBy=cfg.AgentID() (or "user" if empty).
// Refuses re-approval — the operator must `st plan reset` first if a
// previously-approved plan needs re-validation. Writes a changelog entry
// so the approval is auditable.
//
// I-178 Phase A: this is the as-side primitive that the
// `plan-before-code-guard.sh` hook (Phase B, separate per-agent install)
// will gate Edit/Write tool use against. Items not yet approved cannot
// have application code written for them.
func PlanApprove(s *store.Store, cfg *config.Config, id string, opts PlanApproveOpts) int {
	item, ok := s.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "not found: %s\n", id)
		return 1
	}

	if item.PlanApproved {
		fmt.Fprintf(os.Stderr,
			"%s plan is already approved (by %s at %s) — run `st plan reset %s` first if it needs re-validation\n",
			id, fallback(item.PlanApprovedBy, "?"), fallback(item.PlanApprovedAt, "?"), id)
		return 1
	}

	// I-511: --strict refuses approval when the plan sidecar's
	// acceptance criteria fail the verifiability check. The default
	// (non-strict) approve path stays lenient — operators opt in by
	// passing --strict when they want the gate to be hard.
	if opts.Strict {
		findings := loadStrictACFindings(cfg, id)
		if len(findings) > 0 {
			fmt.Fprintf(os.Stderr,
				"%s --strict: %d acceptance criterion/criteria not obviously verifiable; refusing approval:\n",
				id, len(findings))
			for _, f := range findings {
				fmt.Fprintf(os.Stderr, "  %s\n", f.String())
			}
			return 2
		}
	}

	approver := cfg.AgentID()
	if approver == "" {
		approver = "user"
	}
	approvedAt := time.Now().Format(time.RFC3339)

	if err := s.Mutate(id, func(it *model.Item) error {
		it.PlanApproved = true
		it.PlanApprovedAt = approvedAt
		it.PlanApprovedBy = approver
		it.Doc.SetField("plan_approved", "true")
		it.Doc.SetField("plan_approved_at", approvedAt)
		it.Doc.SetField("plan_approved_by", approver)
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", id, err)
		return 1
	}

	_ = changelog.Append(cfg, id, changelog.Entry{
		Op:       "plan_approve",
		NewValue: approver,
		Reason:   "I-178 plan-before-code gate: plan approved",
	})

	fmt.Printf("Approved plan for %s (by %s at %s)\n", id, approver, approvedAt)
	autoSync(s, fmt.Sprintf("st plan approve: %s", id))
	return 0
}

// PlanReset reverts an item's plan-approval state. Used when the plan
// is rejected on review and needs to be regenerated, or when the
// approach changes mid-stream and the operator wants to re-validate.
// Writes a changelog entry so the reset is auditable.
func PlanReset(s *store.Store, cfg *config.Config, id string) int {
	item, ok := s.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "not found: %s\n", id)
		return 1
	}

	if !item.PlanApproved {
		fmt.Fprintf(os.Stderr, "%s plan is not currently approved — nothing to reset\n", id)
		return 1
	}

	priorBy := item.PlanApprovedBy
	priorAt := item.PlanApprovedAt

	if err := s.Mutate(id, func(it *model.Item) error {
		it.PlanApproved = false
		it.PlanApprovedAt = ""
		it.PlanApprovedBy = ""
		it.Doc.SetField("plan_approved", "false")
		it.Doc.SetField("plan_approved_at", "")
		it.Doc.SetField("plan_approved_by", "")
		return nil
	}); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", id, err)
		return 1
	}

	_ = changelog.Append(cfg, id, changelog.Entry{
		Op:       "plan_reset",
		OldValue: priorBy,
		Reason:   fmt.Sprintf("I-178 plan reset (was approved by %s at %s)", priorBy, priorAt),
	})

	fmt.Printf("Reset plan approval for %s (was approved by %s at %s)\n", id, fallback(priorBy, "?"), fallback(priorAt, "?"))
	autoSync(s, fmt.Sprintf("st plan reset: %s", id))
	return 0
}

// PlanCheck prints the approval state for `id` and exits 0 if approved,
// 1 if not. Designed for the `plan-before-code-guard.sh` hook to call as
// `st plan check $ITEM_ID > /dev/null` so the hook can deny Edit/Write
// when the gate is closed.
func PlanCheck(s *store.Store, cfg *config.Config, id string) int {
	item, ok := s.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "not found: %s\n", id)
		return 1
	}
	if item.PlanApproved {
		fmt.Printf("approved by %s at %s\n", fallback(item.PlanApprovedBy, "?"), fallback(item.PlanApprovedAt, "?"))
		return 0
	}
	fmt.Printf("not approved\n")
	return 1
}

// PlanShow renders a detailed view of an item's plan-approval state plus
// any linked plan-file paths.
func PlanShow(s *store.Store, cfg *config.Config, id string) int {
	item, ok := s.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "not found: %s\n", id)
		return 1
	}
	fmt.Printf("Plan for %s — %s\n", id, item.Title)
	if item.PlanApproved {
		fmt.Printf("  Status:      approved\n")
		fmt.Printf("  Approved by: %s\n", fallback(item.PlanApprovedBy, "?"))
		fmt.Printf("  Approved at: %s\n", fallback(item.PlanApprovedAt, "?"))
	} else {
		fmt.Printf("  Status:      not approved\n")
	}
	if len(item.LinkedPlans) == 0 {
		fmt.Printf("  Linked:      (none)\n")
	} else {
		fmt.Printf("  Linked plans:\n")
		for _, p := range item.LinkedPlans {
			fmt.Printf("    - %s\n", p)
		}
	}
	return 0
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// loadStrictACFindings loads the per-item plan sidecar (if present)
// and returns ValidateACs findings against its acceptance criteria.
// Returns nil if no sidecar exists or parsing fails — strict mode
// gates only against findings, never against missing data.
//
// I-511: used by `st plan approve --strict` to refuse approval when
// the plan content has un-verifiable ACs. Falls back gracefully when
// no sidecar exists (e.g. plan was approved via `st plan approve`
// directly without `st prep`) — strict mode then has no AC content
// to inspect, and approval proceeds.
func loadStrictACFindings(cfg *config.Config, id string) []plan.ACFinding {
	if cfg == nil {
		return nil
	}
	p, err := plan.Load(cfg.PlansDir(), id)
	if err != nil || p == nil {
		return nil
	}
	return plan.ValidateACs(p.ACs)
}
