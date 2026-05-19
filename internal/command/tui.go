package command

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jfinlinson/agent-state/internal/agent"
	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/model"
	"github.com/jfinlinson/agent-state/internal/store"
)

// tui.go (T-372 + T-373) — `st tui`: the Layout-A orchestration TUI.
//
// T-372 shipped the STATIC frame (`--once`); T-373 extends it into a
// LIVE Bubble Tea event loop driven by fsnotify (see tui_live.go).
// View(), the Lipgloss layout, and the four panel renderers are
// unchanged — only the event loop is new (the §7 invariant: each layer
// is glue on top of the stable primitive below).

// TuiOpts are the `st tui` flags.
type TuiOpts struct {
	Item  string // optional focused item id; default = next queue pick
	Width int    // optional render width; <=0 ⇒ DefaultWidth
	Once  bool   // T-372 static one-shot (no event loop, no watcher)
}

// DefaultWidth is the static fallback when the terminal width is
// unavailable. Live mode replaces it via tea.WindowSizeMsg on resize.
const DefaultWidth = 120

// Panel layout proportions (left composite : right planning), pre-borders.
const (
	compositeWidth = 78
	planningWidth  = 40
)

// tuiModel is the Layout-A model. Static (T-372) renders View once; live
// (T-373) calls View on every debounced refreshMsg.
type tuiModel struct {
	s       *store.Store
	cfg     *config.Config
	item    *model.Item
	agents  []*agent.Registration
	pending int                    // queue entries needing operator approval
	claimed map[string]*model.Item // session → claimed item (rebuilt on refresh)
	width   int

	// Live-mode wiring (zero for static / `--once`).
	refreshCh chan refreshMsg
}

// tea.Model satisfaction. T-373 wires the event loop; T-374 will add the
// §3/§5 navigation keys on top of these handlers.

func (m tuiModel) Init() tea.Cmd {
	if m.refreshCh == nil {
		return nil // static path — no event source to wait on
	}
	return waitForRefresh(m.refreshCh)
}

func (m tuiModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case refreshMsg:
		m = doRefresh(m)
		return m, waitForRefresh(m.refreshCh) // re-arm for the next burst
	case tea.WindowSizeMsg:
		m.width = v.Width
		return m, nil
	case tea.KeyMsg:
		// T-373 ships only the basic event-loop necessities (quit).
		// The §3/§5 navigation model (Space toggles, Enter drills,
		// Esc returns, arrows) is T-374's scope — explicitly out here.
		switch v.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

// waitForRefresh is the tea.Cmd Bubble Tea uses to read the NEXT
// debounced refresh message from the fsnotify goroutine. After each
// refresh, Update re-arms by returning this Cmd again (the standard
// Bubble Tea "stream of messages from a channel" pattern).
func waitForRefresh(ch <-chan refreshMsg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

// doRefresh re-reads the substrate. coordinate.go's freshItem uses the
// same pattern — the store caches on construction, so reopening it is
// the way to pick up out-of-band file changes. value-receiver transform
// to stay with the Bubble Tea idiom.
func doRefresh(m tuiModel) tuiModel {
	if fresh, err := store.New(m.cfg); err == nil {
		m.s = fresh
	}
	m.agents, _ = agent.ListRegistrations(m.cfg)
	m.pending = 0
	for _, e := range LoadQueue(m.cfg) {
		if !e.Approved {
			m.pending++
		}
	}
	m.claimed = buildClaimedIndex(m.s)
	if m.item != nil {
		if fresh, ok := m.s.Get(m.item.ID); ok {
			m.item = fresh
		}
		// If the focused item vanished (closed + archived during the
		// session), leave m.item as-is rather than racing a re-resolve;
		// the composite renders the last-known state until the operator
		// retargets via --item.
	}
	return m
}

func buildClaimedIndex(s *store.Store) map[string]*model.Item {
	out := map[string]*model.Item{}
	for _, it := range s.All() {
		if it.ClaimedBy != "" {
			out[it.ClaimedBy] = it
		}
	}
	return out
}

func (m tuiModel) View() string {
	w := m.width
	if w <= 0 {
		w = DefaultWidth
	}

	agentStrip := m.renderAgentStrip()
	composite := m.renderComposite()
	planning := m.renderPlanning()
	alerts := m.renderAlerts()

	panel := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	top := panel.Width(w - 2).Render(agentStrip)
	left := panel.Width(compositeWidth).Render(composite)
	right := panel.Width(planningWidth).Render(planning)
	mid := lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	bot := panel.Width(w - 2).Render(alerts)
	return lipgloss.JoinVertical(lipgloss.Left, top, mid, bot)
}

// --- panel content (deterministic; no map iteration in render order) ---

func (m tuiModel) renderAgentStrip() string {
	if len(m.agents) == 0 {
		return "agents: (no registered agents in this workspace)"
	}
	regs := append([]*agent.Registration(nil), m.agents...)
	sort.Slice(regs, func(i, j int) bool { return regs[i].AgentID < regs[j].AgentID })

	var b strings.Builder
	b.WriteString("agents:")
	for _, r := range regs {
		focus := "(idle)"
		if it, ok := m.claimed[r.SessionID]; ok {
			focus = it.ID + " " + truncate(it.Title, 40)
		}
		fmt.Fprintf(&b, "\n  %s  pid:%d  %s", r.AgentID, r.PID, focus)
	}
	return b.String()
}

func (m tuiModel) renderComposite() string {
	if m.item == nil {
		return "focused item: (none — workspace has no eligible item)"
	}
	var buf bytes.Buffer
	showFull(&buf, m.s, m.cfg, m.item, false) // §7: REUSES the renderer
	return strings.TrimRight(buf.String(), "\n")
}

func (m tuiModel) renderPlanning() string {
	var buf bytes.Buffer
	buf.WriteString("planning queue (st recommend top 5):\n\n")
	recommendTo(&buf, m.s, m.cfg, RecommendOpts{Top: 5}) // §7: REUSES the engine
	return strings.TrimRight(buf.String(), "\n")
}

func (m tuiModel) renderAlerts() string {
	parts := []string{fmt.Sprintf("%d awaiting approval", m.pending)}
	hint := ""
	if m.refreshCh != nil {
		hint = "  ·  q to quit · live"
	}
	return "alerts: " + strings.Join(parts, "  ·  ") + hint
}

// --- entrypoints ---

// Tui dispatches to the static or live entrypoint. The cobra path uses
// this; tests can call tuiTo / doRefresh / Update directly for headless
// assertions.
func Tui(s *store.Store, cfg *config.Config, opts TuiOpts) int {
	if opts.Once {
		return tuiTo(os.Stdout, s, cfg, opts)
	}
	return tuiLive(s, cfg, opts)
}

// tuiTo renders ONCE to w. The cobra `--once` path and tests use this;
// behaviour is identical to T-372's static frame so no regression.
func tuiTo(w io.Writer, s *store.Store, cfg *config.Config, opts TuiOpts) int {
	it, rc := resolveFocus(s, cfg, opts.Item)
	if rc != 0 {
		return rc
	}
	regs, _ := agent.ListRegistrations(cfg)
	pending := 0
	for _, e := range LoadQueue(cfg) {
		if !e.Approved {
			pending++
		}
	}
	m := tuiModel{
		s: s, cfg: cfg, item: it, agents: regs, pending: pending,
		claimed: buildClaimedIndex(s), width: opts.Width,
	}
	fmt.Fprintln(w, m.View())
	return 0
}

// tuiLive starts the fsnotify watcher and runs the Bubble Tea program.
// The watcher lifecycle is bounded by the program: closed on exit so
// goroutines and file descriptors don't leak. q / Ctrl-C / Esc quits.
func tuiLive(s *store.Store, cfg *config.Config, opts TuiOpts) int {
	it, rc := resolveFocus(s, cfg, opts.Item)
	if rc != 0 {
		return rc
	}
	refreshCh := make(chan refreshMsg, 1)
	done := make(chan struct{})
	w, err := startWatcher(cfg, refreshCh, done)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tui: fsnotify watcher: %v (falling back to --once)\n", err)
		return tuiTo(os.Stdout, s, cfg, opts)
	}
	defer func() {
		close(done)
		_ = w.Close()
	}()

	regs, _ := agent.ListRegistrations(cfg)
	pending := 0
	for _, e := range LoadQueue(cfg) {
		if !e.Approved {
			pending++
		}
	}
	m := tuiModel{
		s: s, cfg: cfg, item: it, agents: regs, pending: pending,
		claimed: buildClaimedIndex(s), width: opts.Width, refreshCh: refreshCh,
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}
	return 0
}

// resolveFocus picks the focused item: explicit --item wins; default is
// the first item in the queue that exists in the store (the same
// dispatch source the coordinator uses — single source of truth,
// contract §4.2).
func resolveFocus(s *store.Store, cfg *config.Config, want string) (*model.Item, int) {
	if want != "" {
		it, ok := s.Get(want)
		if !ok {
			fmt.Fprintf(os.Stderr, "not found: %s\n", want)
			return nil, 1
		}
		return it, 0
	}
	for _, e := range LoadQueue(cfg) {
		if it, ok := s.Get(e.ID); ok {
			return it, 0
		}
	}
	fmt.Fprintln(os.Stderr,
		"no items in queue to focus; use --item <id> (or add to the queue)")
	return nil, 1
}

// (truncate lives in status.go — rune-safe, used here for title clipping.)
