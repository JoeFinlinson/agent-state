package command

import (
	"fmt"
	"os"
	"strings"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/deps"
	"github.com/jfinlinson/agent-state/internal/registry"
	"github.com/jfinlinson/agent-state/internal/store"
)

// SprintShow displays detailed information about a sprint and its items.
func SprintShow(s *store.Store, cfg *config.Config, sprintID string) int {
	r, err := registry.Load(cfg.EpicsPath())
	if err != nil {
		fmt.Fprintf(os.Stderr, "loading registry: %v\n", err)
		return 1
	}

	sp, err := r.SprintByID(sprintID)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}

	// Get epic info
	epicTitle := ""
	if ep, ok := r.GetEpic(sp.Epic); ok {
		epicTitle = ep.Title
	}

	// Plan status
	planStatus := "not approved"
	planDate := ""
	if sp.PlanApproved {
		planStatus = "approved"
		if sp.PlanApprovedAt != "" {
			planDate = " (" + sp.PlanApprovedAt + ")"
		}
	}

	// Header
	fmt.Printf("Sprint: %s — %s\n", sp.ID, sp.Title)
	if epicTitle != "" {
		fmt.Printf("Epic:   %s — %s\n", sp.Epic, epicTitle)
	} else {
		fmt.Printf("Epic:   %s\n", sp.Epic)
	}
	fmt.Printf("Status: %s   Plan: %s%s\n", sp.Status, planStatus, planDate)
	fmt.Println()

	if len(sp.Items) == 0 {
		fmt.Println("  (no items)")
		return 0
	}

	// Build dep graph for blocking info
	g := deps.Build(s.All(), cfg)

	// Table header
	fmt.Printf("  %-3s %-8s %-35s %-12s %-8s\n", "#", "ID", "Title", "Status", "Priority")

	complete := 0
	inProgress := 0
	blocked := 0

	for i, itemID := range sp.Items {
		item, ok := s.Get(itemID)
		if !ok {
			fmt.Printf("  %-3d %-8s (not found)\n", i+1, itemID)
			continue
		}

		title := item.Title
		if len(title) > 35 {
			title = title[:32] + "..."
		}

		prio := "p2"
		if item.Priority != nil {
			prio = fmt.Sprintf("p%d", *item.Priority)
		}

		fmt.Printf("  %-3d %-8s %-35s %-12s %-8s\n", i+1, item.ID, title, item.Status, prio)

		// Show blocking info
		blocksIDs := g.BlocksItems(itemID)
		if len(blocksIDs) > 0 {
			fmt.Printf("  %s blocks %s\n", strings.Repeat(" ", 13), strings.Join(blocksIDs, ", "))
		}

		// Count status categories
		if cfg.IsTerminalStatus(item.Type, item.Status) {
			complete++
		} else {
			tc, ok := cfg.Types[item.Type]
			if ok && item.Status == tc.ActiveStatus {
				inProgress++
			}
			if g.IsBlocked(itemID) {
				blocked++
			}
		}
	}

	fmt.Println()
	fmt.Printf("Progress: %d/%d complete, %d in-progress, %d blocked\n",
		complete, len(sp.Items), inProgress, blocked)

	return 0
}
