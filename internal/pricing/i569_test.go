package pricing

import (
	"math"
	"testing"
)

// TestEstimateSyntheticCostBreakdown_TableDriven_AllClasses asserts that for
// every model in the rate table, EstimateSyntheticCostBreakdown decomposes a
// known-shape token bundle into the right per-class USD costs and that they
// sum to Total. Catches future rate-table edits that drop or skew a class.
func TestEstimateSyntheticCostBreakdown_TableDriven_AllClasses(t *testing.T) {
	const (
		regIn      = 1_000_000 // 1 MTok
		regOut     = 1_000_000
		cacheRead  = 1_000_000
		cacheOut5m = 1_000_000
		cacheOut1h = 1_000_000
	)
	for _, m := range KnownModels() {
		m := m
		t.Run(m, func(t *testing.T) {
			rate, err := Lookup(m)
			if err != nil {
				t.Fatalf("Lookup(%q): %v", m, err)
			}
			b, err := EstimateSyntheticCostBreakdown(m, regIn, regOut, cacheRead, cacheOut5m, cacheOut1h)
			if err != nil {
				t.Fatalf("breakdown: %v", err)
			}
			eq := func(name string, got, want float64) {
				if math.Abs(got-want) > 1e-9 {
					t.Errorf("%s: got %.9f, want %.9f", name, got, want)
				}
			}
			// 1 MTok of each class costs exactly the per-MTok rate.
			eq("Input", b.Input, rate.Input)
			eq("Output", b.Output, rate.Output)
			eq("CacheRead", b.CacheRead, rate.CacheRead)
			eq("CacheCreation5m", b.CacheCreation5m, rate.CacheWrite5m)
			eq("CacheCreation1h", b.CacheCreation1h, rate.CacheWrite1h)
			// Total is the sum of the parts.
			want := rate.Input + rate.Output + rate.CacheRead + rate.CacheWrite5m + rate.CacheWrite1h
			eq("Total", b.Total, want)
		})
	}
}

// TestEstimateSyntheticCostBreakdown_UnknownModel surfaces the typed error
// without panicking.
func TestEstimateSyntheticCostBreakdown_UnknownModel(t *testing.T) {
	b, err := EstimateSyntheticCostBreakdown("not-a-real-model", 1, 1, 1, 1, 1)
	if err == nil {
		t.Fatalf("expected error for unknown model, got %+v", b)
	}
	if b.Total != 0 {
		t.Errorf("Total = %v, want 0", b.Total)
	}
}

// TestEstimateSyntheticCostUSD_AliasOfBreakdownTotal: the simpler entry point
// must equal the breakdown's Total field — keeps the two API surfaces from
// drifting apart on a future refactor.
func TestEstimateSyntheticCostUSD_AliasOfBreakdownTotal(t *testing.T) {
	for _, m := range KnownModels() {
		c, err := EstimateSyntheticCostUSD(m, 12345, 678, 90000, 1234, 5678)
		if err != nil {
			t.Fatalf("USD(%s): %v", m, err)
		}
		b, err := EstimateSyntheticCostBreakdown(m, 12345, 678, 90000, 1234, 5678)
		if err != nil {
			t.Fatalf("breakdown(%s): %v", m, err)
		}
		if math.Abs(c-b.Total) > 1e-9 {
			t.Errorf("%s: USD=%.9f, breakdown.Total=%.9f", m, c, b.Total)
		}
	}
}
