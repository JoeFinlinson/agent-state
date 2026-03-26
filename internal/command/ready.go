package command

import (
	"fmt"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/deps"
	"github.com/jfinlinson/agent-state/internal/model"
	"github.com/jfinlinson/agent-state/internal/store"
)

// ReadyOpts holds flags for the ready command.
type ReadyOpts struct {
	Type  string
	Tag   string
	Limit int
}

func Ready(s *store.Store, cfg *config.Config, opts ReadyOpts) int {
	g := deps.Build(s.All(), cfg)
	items := g.Ready()

	// Apply additional filters
	var filtered []*model.Item
	for _, item := range items {
		if opts.Type != "" && item.Type != opts.Type {
			continue
		}
		if opts.Tag != "" && !hasTag(item, opts.Tag) {
			continue
		}
		filtered = append(filtered, item)
	}

	if opts.Limit > 0 && len(filtered) > opts.Limit {
		filtered = filtered[:opts.Limit]
	}

	if len(filtered) == 0 {
		fmt.Println("No ready items.")
		return 0
	}

	for _, item := range filtered {
		p := 2
		if item.Priority != nil {
			p = *item.Priority
		}
		fmt.Printf("%-8s p%d  %s\n", item.ID, p, item.Title)
	}

	return 0
}

func hasTag(item *model.Item, tag string) bool {
	for _, t := range item.Tags {
		if t == tag {
			return true
		}
	}
	return false
}
