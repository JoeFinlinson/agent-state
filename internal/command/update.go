package command

import (
	"fmt"
	"os"

	"github.com/jfinlinson/agent-state/internal/store"
)

func Update(s *store.Store, id, field, value string) int {
	item, ok := s.Get(id)
	if !ok {
		fmt.Fprintf(os.Stderr, "not found: %s\n", id)
		return 1
	}

	if item.Doc == nil {
		fmt.Fprintf(os.Stderr, "%s has no document\n", id)
		return 1
	}

	item.Doc.SetField(field, value)

	if err := s.Write(item); err != nil {
		fmt.Fprintf(os.Stderr, "writing %s: %v\n", id, err)
		return 1
	}

	fmt.Printf("Updated %s.%s\n", id, field)
	return 0
}
