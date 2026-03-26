package command

import (
	"fmt"
	"os"

	"github.com/jfinlinson/agent-state/internal/store"
)

func Sync(s *store.Store, args []string) int {
	msg := "as: sync agent-state"
	if len(args) > 0 {
		msg = args[0]
	}

	if err := s.GitSync(msg); err != nil {
		fmt.Fprintf(os.Stderr, "sync: %v\n", err)
		return 1
	}

	fmt.Println("Synced.")
	return 0
}
