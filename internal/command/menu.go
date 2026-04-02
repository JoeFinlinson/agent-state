package command

import (
	"fmt"
	"os"
	"strings"
	"time"

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

		// Ctrl+C — return sentinel so callers can distinguish interrupt from selection
		if n == 1 && buf[0] == 3 {
			fmt.Fprintln(os.Stderr)
			fmt.Fprintf(os.Stderr, "  [Ctrl+C] interrupted\n")
			return "^C"
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

	// EOF or read error — treat as interrupt
	return "^C"
}

// selectMenuTimed is like selectMenu but auto-selects the default option after
// the given timeout. A countdown is displayed below the menu. If timeout <= 0,
// it falls through to the normal (blocking) selectMenu.
func selectMenuTimed(prompt string, options []menuOption, defaultIdx int, timeout time.Duration) string {
	if timeout <= 0 {
		return selectMenu(prompt, options, defaultIdx)
	}

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return selectMenuFallback(prompt, options)
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return selectMenuFallback(prompt, options)
	}
	defer term.Restore(fd, oldState)

	selected := defaultIdx
	if selected < 0 || selected >= len(options) {
		selected = 0
	}

	remaining := int(timeout.Seconds())

	renderMenu := func() {
		for i, opt := range options {
			if i == selected {
				fmt.Fprintf(os.Stderr, "\r\033[K  \033[1;7m ▸ %s  %s \033[0m\n", opt.Key, opt.Label)
			} else {
				fmt.Fprintf(os.Stderr, "\r\033[K    %s  %s\n", opt.Key, opt.Label)
			}
		}
	}

	renderCountdown := func() {
		fmt.Fprintf(os.Stderr, "\r\033[K  \033[33mAuto-selecting [%s] in %ds…\033[0m", options[selected].Key, remaining)
	}

	if prompt != "" {
		fmt.Fprintf(os.Stderr, "\n  %s\n\n", prompt)
	}
	renderMenu()
	renderCountdown()

	fmt.Fprint(os.Stderr, "\033[?25l")
	defer fmt.Fprint(os.Stderr, "\033[?25h")

	// Read input in a goroutine so we can race against the timer.
	type keyEvent struct {
		buf [3]byte
		n   int
		err error
	}
	keyCh := make(chan keyEvent, 1)
	go func() {
		for {
			var ev keyEvent
			ev.n, ev.err = os.Stdin.Read(ev.buf[:])
			keyCh <- ev
			if ev.err != nil {
				return
			}
		}
	}()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	deadline := time.After(timeout)

	for {
		select {
		case <-deadline:
			fmt.Fprintf(os.Stderr, "\r\033[K  \033[1mAuto-selected [%s]\033[0m\n", options[selected].Key)
			return options[selected].Key

		case <-ticker.C:
			remaining--
			if remaining < 0 {
				remaining = 0
			}
			renderCountdown()

		case ev := <-keyCh:
			if ev.err != nil {
				fmt.Fprintf(os.Stderr, "\r\033[K\n")
				return "^C"
			}
			if ev.n == 1 && ev.buf[0] == 3 { // Ctrl+C
				fmt.Fprintf(os.Stderr, "\r\033[K\n")
				fmt.Fprintf(os.Stderr, "  [Ctrl+C] interrupted\n")
				return "^C"
			}
			if ev.n == 1 && (ev.buf[0] == '\r' || ev.buf[0] == '\n') { // Enter
				fmt.Fprintf(os.Stderr, "\r\033[K\n")
				return options[selected].Key
			}
			if ev.n == 3 && ev.buf[0] == 0x1b && ev.buf[1] == '[' { // Arrow keys
				switch ev.buf[2] {
				case 'A':
					if selected > 0 {
						selected--
					}
				case 'B':
					if selected < len(options)-1 {
						selected++
					}
				}
				fmt.Fprintf(os.Stderr, "\r")
				fmt.Fprintf(os.Stderr, "\033[%dA", len(options))
				renderMenu()
				renderCountdown()
				continue
			}
			if ev.n == 1 { // Hotkey
				key := strings.ToLower(string(ev.buf[0]))
				for _, opt := range options {
					if strings.ToLower(opt.Key) == key {
						fmt.Fprintf(os.Stderr, "\r\033[K\n")
						return opt.Key
					}
				}
			}
		}
	}
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
