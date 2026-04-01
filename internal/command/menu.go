package command

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// menuOption represents a single choice in an interactive menu.
type menuOption struct {
	Key   string // single-char hotkey (e.g., "c", "1")
	Label string // display text
}

// selectMenu renders an interactive menu with arrow-key navigation and
// single-keypress selection. Returns the Key of the selected option.
//
// Features:
//   - Arrow keys (↑/↓) move the highlight
//   - Pressing a hotkey selects immediately (no Enter needed)
//   - Enter confirms the highlighted option
//
// Falls back to simple line-based input if terminal is not available.
func selectMenu(prompt string, options []menuOption, defaultIdx int) string {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return selectMenuFallback(prompt, options)
	}

	// Switch to raw mode for single-keypress input
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return selectMenuFallback(prompt, options)
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	if selected < 0 || selected >= len(options) {
		selected = 0
	}

	renderMenu := func() {
		// Move cursor up to overwrite previous render (except first time)
		for i, opt := range options {
			if i == selected {
				// Highlighted: bold + reverse video
				fmt.Fprintf(os.Stderr, "\r\033[K  \033[1;7m ▸ %s  %s \033[0m\n", opt.Key, opt.Label)
			} else {
				fmt.Fprintf(os.Stderr, "\r\033[K    %s  %s\n", opt.Key, opt.Label)
			}
		}
	}

	// Initial render
	if prompt != "" {
		fmt.Fprintf(os.Stderr, "\n  %s\n\n", prompt)
	}
	renderMenu()

	// Hide cursor
	fmt.Fprint(os.Stderr, "\033[?25l")
	defer fmt.Fprint(os.Stderr, "\033[?25h") // restore cursor on exit

	buf := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(buf)
		if err != nil {
			break
		}

		// Ctrl+C
		if n == 1 && buf[0] == 3 {
			return options[selected].Key
		}

		// Enter — confirm current selection
		if n == 1 && (buf[0] == '\r' || buf[0] == '\n') {
			// Move below menu
			fmt.Fprintln(os.Stderr)
			return options[selected].Key
		}

		// Arrow keys: ESC [ A (up), ESC [ B (down)
		if n == 3 && buf[0] == 0x1b && buf[1] == '[' {
			switch buf[2] {
			case 'A': // up
				if selected > 0 {
					selected--
				}
			case 'B': // down
				if selected < len(options)-1 {
					selected++
				}
			}
			// Move cursor up to re-render
			fmt.Fprintf(os.Stderr, "\033[%dA", len(options))
			renderMenu()
			continue
		}

		// Single keypress — check hotkeys
		if n == 1 {
			key := strings.ToLower(string(buf[0]))
			for _, opt := range options {
				if strings.ToLower(opt.Key) == key {
					fmt.Fprintln(os.Stderr)
					return opt.Key
				}
			}
		}
	}

	return options[selected].Key
}

// selectMenuFallback is a simple line-based fallback for non-terminal input.
func selectMenuFallback(prompt string, options []menuOption) string {
	if prompt != "" {
		fmt.Fprintf(os.Stderr, "\n  %s\n", prompt)
	}
	for _, opt := range options {
		fmt.Fprintf(os.Stderr, "    %s  %s\n", opt.Key, opt.Label)
	}
	fmt.Fprintf(os.Stderr, "\n  > ")

	var input string
	fmt.Scanln(&input)
	input = strings.TrimSpace(strings.ToLower(input))

	for _, opt := range options {
		if strings.ToLower(opt.Key) == input {
			return opt.Key
		}
	}
	if len(options) > 0 {
		return options[0].Key
	}
	return ""
}

// confirmPrompt shows a y/N prompt with single-keypress input.
// Returns true for "y", false for anything else.
func confirmPrompt(prompt string) bool {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)
		var input string
		fmt.Scanln(&input)
		return strings.ToLower(strings.TrimSpace(input)) == "y"
	}

	fmt.Fprintf(os.Stderr, "%s [y/N]: ", prompt)

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		var input string
		fmt.Scanln(&input)
		return strings.ToLower(strings.TrimSpace(input)) == "y"
	}
	defer term.Restore(fd, oldState)

	buf := make([]byte, 1)
	n, err := os.Stdin.Read(buf)
	if err != nil || n == 0 {
		fmt.Fprintln(os.Stderr, "n")
		return false
	}

	ch := strings.ToLower(string(buf[0]))
	if ch == "y" {
		fmt.Fprintln(os.Stderr, "y")
		return true
	}
	fmt.Fprintln(os.Stderr, "n")
	return false
}
