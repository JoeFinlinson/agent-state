package command

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/deps"
	"github.com/jfinlinson/agent-state/internal/model"
	"github.com/jfinlinson/agent-state/internal/store"
)

func Index(s *store.Store, cfg *config.Config, args []string) int {
	g := deps.Build(s.All(), cfg)

	var b strings.Builder
	b.WriteString("# Agent State Index\n")
	b.WriteString(fmt.Sprintf("last_touched: %s\n\n", nowStr()))

	// Active work
	active := s.List(store.StatusFilter("active"))
	b.WriteString("## Active Work\n")
	if len(active) == 0 {
		b.WriteString("(none)\n")
	}
	for _, item := range active {
		line := fmt.Sprintf("- %s — %s", item.ID, item.Title)
		if item.AssignedTo != "" {
			line += fmt.Sprintf(" [%s]", item.AssignedTo)
		}
		b.WriteString(line + "\n")
	}
	b.WriteString("\n")

	// Ready work
	ready := g.Ready()
	if len(ready) > 0 {
		b.WriteString("## Ready (unblocked, by priority)\n")
		for _, item := range ready {
			p := 2
			if item.Priority != nil {
				p = *item.Priority
			}
			b.WriteString(fmt.Sprintf("- %s — %s (p%d)\n", item.ID, item.Title, p))
		}
		b.WriteString("\n")
	}

	// Queued tasks (grouped by type)
	for typeName := range cfg.Types {
		tc := cfg.Types[typeName]
		queued := s.List(store.TypeFilter(typeName), store.StatusFilter(tc.StartStatus))
		if len(queued) == 0 {
			continue
		}

		// Sort by priority then ID
		sort.Slice(queued, func(i, j int) bool {
			pi, pj := priorityOf(queued[i]), priorityOf(queued[j])
			if pi != pj {
				return pi < pj
			}
			return queued[i].ID < queued[j].ID
		})

		b.WriteString(fmt.Sprintf("## Queued %ss\n", capitalize(typeName)))
		for _, item := range queued {
			line := fmt.Sprintf("- %s — %s", item.ID, item.Title)
			if g.IsBlocked(item.ID) {
				unresolved := g.UnresolvedDeps(item.ID)
				line += fmt.Sprintf(" (blocked by %s)", strings.Join(unresolved, ", "))
			}
			b.WriteString(line + "\n")
		}
		b.WriteString("\n")
	}

	// Open issues
	issues := s.List(store.TypeFilter("issue"), store.StatusFilter("open"))
	if len(issues) > 0 {
		b.WriteString("## Open Issues\n")
		for _, item := range issues {
			sev := item.Severity
			if sev == "" {
				sev = "medium"
			}
			b.WriteString(fmt.Sprintf("- %s [%s] — %s\n", item.ID, sev, item.Title))
		}
		b.WriteString("\n")
	}

	// Archived summary
	var archivedCount int
	for _, item := range s.All() {
		for _, ts := range []string{"completed", "resolved", "archived", "abandoned", "wontfix"} {
			if item.Status == ts {
				archivedCount++
				break
			}
		}
	}
	b.WriteString(fmt.Sprintf("## Archived\n%d items\n", archivedCount))

	// Write to index path
	indexPath := cfg.IndexPath()
	if err := os.WriteFile(indexPath, []byte(b.String()), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "writing index: %v\n", err)
		return 1
	}

	fmt.Printf("Generated %s (%d items)\n", indexPath, len(s.All()))
	return 0
}

func priorityOf(item *model.Item) int {
	if item.Priority != nil {
		return *item.Priority
	}
	return 2
}

func nowStr() string {
	return "auto-generated"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
