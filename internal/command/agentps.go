package command

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/jfinlinson/agent-state/internal/agent"
	"github.com/jfinlinson/agent-state/internal/agentps"
	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/store"
	"github.com/jfinlinson/agent-state/internal/transcript"
)

// AgentPSOpts are the `st agent ps` flags.
type AgentPSOpts struct {
	Workspace string // substring filter on workspace path; "" = all
	JSON      bool   // emit the joined []Row as JSON (pre-render)
}

// AgentPS prints the global agent process-table (T-354). Read-only.
// A missing/empty roster is reported to stderr with a non-zero exit
// (absence surfaced, never a silent blank table — operator
// silent-failure principle).
func AgentPS(s *store.Store, cfg *config.Config, opts AgentPSOpts) int {
	dir := agentps.AgentWorkspacesDir(cfg)
	if dir == "" {
		fmt.Fprintln(os.Stderr, "agent ps: no agent roster found (set $ST_AGENT_WORKSPACES_DIR or run from inside an agent workspace tree)")
		return 1
	}
	roster, err := agentps.LoadRoster(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "agent ps: cannot read agent roster at %s: %v\n", dir, err)
		return 1
	}
	if len(roster) == 0 {
		fmt.Fprintf(os.Stderr, "agent ps: no agent roster entries in %s\n", dir)
		return 1
	}

	// Live registrations → agentps.Reg keyed by AgentID (+ liveness).
	regs := map[string]agentps.Reg{}
	if list, err := agent.ListRegistrations(cfg); err != nil {
		// Degrade (the roster still renders) but never swallow.
		fmt.Fprintf(os.Stderr, "agent ps: warning: cannot list registrations: %v\n", err)
	} else {
		for _, r := range list {
			if r == nil || r.AgentID == "" {
				continue
			}
			regs[r.AgentID] = agentps.Reg{
				PID:       r.PID,
				Started:   parseRFC3339(r.Started),
				SessionID: r.SessionID,
				Role:      r.Role,
				Alive:     agent.IsPIDLive(r.PID),
			}
		}
	}

	// Current active item per agent (lowest item id wins, for a stable
	// deterministic pick when an agent has several active items).
	active := map[string]agentps.ItemRef{}
	if s != nil {
		items := s.List(store.StatusFilter("active"))
		sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
		for _, it := range items {
			if it.AssignedTo == "" {
				continue
			}
			if _, seen := active[it.AssignedTo]; seen {
				continue
			}
			stage := ""
			if v, ok := it.Delivery["stage"]; ok {
				stage = fmt.Sprintf("%v", v)
			}
			active[it.AssignedTo] = agentps.ItemRef{ID: it.ID, Stage: stage}
		}
	}

	// "Last updated" = newest session-JSONL mtime (T-353 substrate;
	// §13 finding-3 freshness signal). Only agents with a live
	// registration have a session id to resolve.
	mtime := map[string]time.Time{}
	for id, r := range regs {
		if r.SessionID == "" {
			continue
		}
		var newest time.Time
		for _, p := range transcript.ResolveSessionByID(r.SessionID) {
			if fi, err := os.Stat(p); err == nil && fi.ModTime().After(newest) {
				newest = fi.ModTime()
			}
		}
		if !newest.IsZero() {
			mtime[id] = newest
		}
	}

	rows := agentps.Join(roster, regs, active, mtime)

	if opts.JSON {
		b, err := json.MarshalIndent(rows, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "agent ps: json encode: %v\n", err)
			return 1
		}
		fmt.Println(string(b))
		return 0
	}

	for _, line := range agentps.Render(rows, time.Now(), agentps.Opts{Workspace: opts.Workspace}) {
		fmt.Println(line)
	}
	return 0
}
