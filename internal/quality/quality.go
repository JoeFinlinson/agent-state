// Package quality holds content-depth checks the st CLI runs at item
// create time and at plan-approval time. The first rule set lands
// SBAR substance validation per I-149 — the gate that prevents the
// I-487 SBAR scaffold (TODO placeholders) from carrying through to
// plan approval unfilled.
//
// Future rule sets (item title/summary thresholds, plan-quality
// rules beyond I-511's AC verifiability check) can be added as
// additional entry points without disturbing the existing surface.
package quality

import (
	"strings"

	"github.com/jfinlinson/agent-state/internal/model"
)

// Severity classifies a Violation. errors block the gate they are
// surfaced at; warnings print but allow the operation to continue.
type Severity int

const (
	SeverityWarn Severity = iota
	SeverityError
)

// String returns a human-readable label suitable for stderr output.
func (s Severity) String() string {
	switch s {
	case SeverityError:
		return "error"
	default:
		return "warning"
	}
}

// Violation describes a single content-quality finding.
type Violation struct {
	Severity Severity
	Field    string // dotted path, e.g. "sbar.situation"
	Message  string
}

// String formats the finding as "<severity>: <field> — <message>".
func (v Violation) String() string {
	return v.Severity.String() + ": " + v.Field + " — " + v.Message
}

// sbarPlaceholders are the literal TODO strings the I-487 scaffold
// (and migrate-sbar / st create) writes for each unfilled section.
// The substance gate treats a body equal to one of these as empty.
var sbarPlaceholders = map[string]string{
	"situation":      "TODO: one-line symptom or trigger that's observable right now",
	"background":     "TODO: prior context — history, code paths, related items",
	"assessment":     "TODO: diagnosis — what's wrong, why, and how confident",
	"recommendation": "TODO: proposed fix — scoped enough to be actionable",
}

// ValidateSBAR reports a Violation per SBAR sub-field that is empty
// or still equal to its TODO placeholder. All four sub-fields are
// checked. Returns nil when the item is fully populated.
//
// This is the I-149 substance gate. I-487 made SBAR a required
// composite field; I-492 / I-493 added scaffold + editor flows;
// I-149 closes the loop by surfacing items where the scaffold was
// never replaced with real content.
func ValidateSBAR(item *model.Item) []Violation {
	var out []Violation
	for _, sec := range []struct {
		key, body string
	}{
		{"situation", item.SBAR.Situation},
		{"background", item.SBAR.Background},
		{"assessment", item.SBAR.Assessment},
		{"recommendation", item.SBAR.Recommendation},
	} {
		body := strings.TrimSpace(sec.body)
		if body == "" {
			out = append(out, Violation{
				Severity: SeverityError,
				Field:    "sbar." + sec.key,
				Message:  "section is empty — fill via `st update " + item.ID + " sbar`",
			})
			continue
		}
		if body == sbarPlaceholders[sec.key] {
			out = append(out, Violation{
				Severity: SeverityError,
				Field:    "sbar." + sec.key,
				Message:  "section still contains the TODO scaffold — replace with real content",
			})
		}
	}
	return out
}

// HasError reports whether any Violation in the slice has
// SeverityError. Useful at gate sites that warn-only by default and
// hard-block under a `--strict` flag.
func HasError(vs []Violation) bool {
	for _, v := range vs {
		if v.Severity == SeverityError {
			return true
		}
	}
	return false
}
