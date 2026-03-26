package command

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jfinlinson/agent-state/internal/store"
)

func Update(s *store.Store, args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	stdin := fs.Bool("stdin", false, "read value from stdin (for multiline)")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	if fs.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "usage: as update <id> <field> [<value>] [--stdin]")
		return 2
	}

	id := fs.Arg(0)
	field := fs.Arg(1)

	var value string
	if *stdin {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "reading stdin: %v\n", err)
			return 1
		}
		value = strings.TrimRight(string(data), "\n")
	} else if fs.NArg() >= 3 {
		value = fs.Arg(2)
	} else {
		fmt.Fprintln(os.Stderr, "usage: as update <id> <field> <value> or --stdin")
		return 2
	}

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
