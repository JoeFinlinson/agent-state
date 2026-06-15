package pricing

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// AnthropicPricingURL is the canonical source for Claude model rates.
const AnthropicPricingURL = "https://docs.anthropic.com/en/docs/about-claude/pricing"

// RateDiff describes a single per-field change between old and new rate tables.
type RateDiff struct {
	Model     string
	Field     string
	Old       float64
	New       float64
	PctChange float64 // positive = price increase, negative = decrease
}

// FetchAnthropicRates fetches and parses Anthropic's pricing page.
// Pass nil to use http.DefaultClient.
func FetchAnthropicRates(client *http.Client) (map[string]Rate, error) {
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Get(AnthropicPricingURL)
	if err != nil {
		return nil, fmt.Errorf("pricing: fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("pricing: fetch returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("pricing: read body: %w", err)
	}
	return parseAnthropicHTML(string(body))
}

var (
	rowRe    = regexp.MustCompile(`(?s)<tr[^>]*>(.*?)</tr>`)
	cellRe   = regexp.MustCompile(`(?s)<t[dh][^>]*>(.*?)</t[dh]>`)
	tagRe    = regexp.MustCompile(`<[^>]+>`)
	dollarRe = regexp.MustCompile(`\$([\d.]+)`)
)

// parseAnthropicHTML extracts model rates from the pricing page HTML using
// regexp-based table scanning (no golang.org/x/net dependency).
// Returns an error when fewer than 2 distinct model families are found.
func parseAnthropicHTML(body string) (map[string]Rate, error) {
	rates := make(map[string]Rate)

	for _, rowMatch := range rowRe.FindAllStringSubmatch(body, -1) {
		cells := cellRe.FindAllStringSubmatch(rowMatch[1], -1)
		if len(cells) < 3 {
			continue
		}

		var texts []string
		for _, c := range cells {
			raw := tagRe.ReplaceAllString(c[1], " ")
			texts = append(texts, strings.TrimSpace(strings.Join(strings.Fields(raw), " ")))
		}

		modelID := anthropicNameToID(texts[0])
		if modelID == "" {
			continue
		}

		var prices []float64
		for _, t := range texts[1:] {
			m := dollarRe.FindStringSubmatch(t)
			if m == nil {
				continue
			}
			v, err := strconv.ParseFloat(m[1], 64)
			if err != nil {
				continue
			}
			prices = append(prices, v)
		}

		if len(prices) < 2 {
			continue
		}

		input := prices[0]
		output := prices[1]
		var cw5m, cw1h, cr float64
		if len(prices) >= 5 {
			// Page lists all cache tiers explicitly
			cw5m = prices[2]
			cw1h = prices[3]
			cr = prices[4]
		} else {
			// Derive cache prices from the standard Anthropic ratios
			cw5m = input * 1.25
			cw1h = input * 2.0
			cr = input * 0.1
		}

		rates[modelID] = Rate{
			Input:        input,
			Output:       output,
			CacheWrite5m: cw5m,
			CacheWrite1h: cw1h,
			CacheRead:    cr,
		}
	}

	families := map[string]bool{}
	for k := range rates {
		parts := strings.SplitN(k, "-", 3)
		if len(parts) >= 2 {
			families[parts[1]] = true
		}
	}
	if len(families) < 2 {
		return nil, fmt.Errorf("pricing: parsed %d model(s) across %d family(ies) — page structure may have changed", len(rates), len(families))
	}

	return rates, nil
}

// anthropicNameToID converts Anthropic display names to internal model IDs.
// "Claude Opus 4.7" → "claude-opus-4-7", "Claude Haiku 3.5" → "claude-haiku-3-5"
func anthropicNameToID(name string) string {
	lower := strings.ToLower(strings.TrimSpace(name))
	if !strings.HasPrefix(lower, "claude") {
		return ""
	}
	id := strings.ReplaceAll(lower, ".", "-")
	id = strings.ReplaceAll(id, " ", "-")
	for strings.Contains(id, "--") {
		id = strings.ReplaceAll(id, "--", "-")
	}
	return strings.TrimRight(id, "-")
}

// DiffRates returns per-model per-field deltas between old and new rate tables,
// sorted by model then field. New-model entries have Old=0 and PctChange=100.
// Removed-model entries have New=0.
func DiffRates(old, new map[string]Rate) []RateDiff {
	allModels := make(map[string]bool, len(old)+len(new))
	for k := range old {
		allModels[k] = true
	}
	for k := range new {
		allModels[k] = true
	}

	models := make([]string, 0, len(allModels))
	for m := range allModels {
		models = append(models, m)
	}
	sort.Strings(models)

	type fieldDesc struct {
		name string
		get  func(Rate) float64
	}
	fields := []fieldDesc{
		{"input", func(r Rate) float64 { return r.Input }},
		{"output", func(r Rate) float64 { return r.Output }},
		{"cache_write_5m", func(r Rate) float64 { return r.CacheWrite5m }},
		{"cache_write_1h", func(r Rate) float64 { return r.CacheWrite1h }},
		{"cache_read", func(r Rate) float64 { return r.CacheRead }},
	}

	var diffs []RateDiff
	for _, m := range models {
		oldR := old[m]
		newR := new[m]
		for _, f := range fields {
			ov := f.get(oldR)
			nv := f.get(newR)
			if ov == nv {
				continue
			}
			var pct float64
			if ov != 0 {
				pct = (nv - ov) / ov * 100
			} else {
				pct = 100
			}
			diffs = append(diffs, RateDiff{
				Model: m, Field: f.name,
				Old: ov, New: nv, PctChange: pct,
			})
		}
	}
	return diffs
}

// SanityCheck returns true when no single field change exceeds maxPct percent.
func SanityCheck(diffs []RateDiff, maxPct float64) bool {
	for _, d := range diffs {
		if math.Abs(d.PctChange) > maxPct {
			return false
		}
	}
	return true
}

// FormatDiff returns a human-readable summary of rate changes.
// Returns a short "up to date" message when diffs is empty.
func FormatDiff(diffs []RateDiff) string {
	if len(diffs) == 0 {
		return "pricing table is up to date — no changes detected\n"
	}
	var b strings.Builder
	for _, d := range diffs {
		fmt.Fprintf(&b, "  %-28s %-15s %8.4g → %8.4g  (%+.1f%%)\n",
			d.Model, d.Field, d.Old, d.New, d.PctChange)
	}
	return b.String()
}
