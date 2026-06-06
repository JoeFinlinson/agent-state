package command

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jfinlinson/agent-state/internal/config"
	"github.com/jfinlinson/agent-state/internal/model"
	"github.com/jfinlinson/agent-state/internal/store"
)

// classifyVerdict is the JSON shape returned by the goal-classifier LLM.
type classifyVerdict struct {
	GoalIDs []string `json:"goal_ids"`
	Reason  string   `json:"reason"`
}

// classifyGoals returns the IDs of active goals that best match the new item.
//
// When engine.RunClaude is nil (tests, migrations, in-process callers) or the
// item type is not task/issue, the function returns nil, nil — a silent no-op.
// On LLM error the function also returns nil, nil (graceful degradation).
func classifyGoals(s *store.Store, cfg *config.Config, itemType, title, situation string, engine RunEngine) ([]string, error) {
	if engine.RunClaude == nil {
		return nil, nil
	}
	if os.Getenv("AS_INTERNAL_NO_CLASSIFY") == "1" {
		return nil, nil
	}
	if itemType != "task" && itemType != "issue" {
		return nil, nil
	}

	goals := s.List(store.TypeFilter("goal"), store.StatusFilter("active"))
	if len(goals) == 0 {
		return nil, nil
	}

	prompt := buildClassifyPrompt(title, situation, goals)
	permMode := cfg.RunPermissionMode()
	var permArgs []string
	if permMode == "dangerously-skip-permissions" || permMode == "" {
		permArgs = []string{"--dangerously-skip-permissions"}
	} else {
		permArgs = []string{"--permission-mode", permMode}
	}
	args := append([]string{"-p", prompt, "--output-format", "json"}, permArgs...)
	env := []string{"AS_CLAUDE_WALL_TIMEOUT=60s"}

	out, exitCode, runErr := engine.RunClaude(cfg.Root(), args, env)
	if runErr != nil || exitCode != 0 {
		fmt.Fprintf(os.Stderr, "warning: goal auto-classify skipped (subprocess exit %d: %v)\n", exitCode, runErr)
		return nil, nil
	}

	var v classifyVerdict
	if parseErr := json.Unmarshal(out, &v); parseErr != nil {
		fmt.Fprintf(os.Stderr, "warning: goal auto-classify skipped (could not parse response: %v)\n", parseErr)
		return nil, nil
	}

	// Validate — filter out any IDs the LLM hallucinated or that are no
	// longer active goals in the store.
	goalSet := make(map[string]*model.Item, len(goals))
	for _, g := range goals {
		goalSet[g.ID] = g
	}
	var matched []string
	for _, gid := range v.GoalIDs {
		gid = strings.TrimSpace(gid)
		if g, ok := goalSet[gid]; ok && g.Status == "active" {
			matched = append(matched, gid)
		}
	}
	return matched, nil
}

func buildClassifyPrompt(title, situation string, goals []*model.Item) string {
	var sb strings.Builder
	sb.WriteString("You are classifying a new work item into one or more strategic goals.\n\n")
	sb.WriteString("New item:\n")
	sb.WriteString("  title: " + title + "\n")
	if situation != "" {
		situation = strings.TrimSpace(situation)
		if len(situation) > 300 {
			situation = situation[:300] + "…"
		}
		sb.WriteString("  situation: " + situation + "\n")
	}
	sb.WriteString("\nActive goals:\n")
	for _, g := range goals {
		line := fmt.Sprintf("  %s: %s", g.ID, g.Title)
		if g.SBAR.Situation != "" {
			sit := strings.TrimSpace(g.SBAR.Situation)
			if len(sit) > 150 {
				sit = sit[:150] + "…"
			}
			line += " — " + sit
		}
		sb.WriteString(line + "\n")
	}
	sb.WriteString(`
Return a JSON object with exactly this shape:
{"goal_ids": ["G-XXX"], "reason": "one-sentence explanation"}

Rules:
- goal_ids must be a subset of the goal IDs listed above (empty array if none match).
- Assign only goals that clearly match — when in doubt, return [].
- Multiple goals are allowed only when the item genuinely spans both domains.
- Do NOT invent goal IDs not listed above.
`)
	return sb.String()
}
