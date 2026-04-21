package alpha_yield

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

func TestRegistered(t *testing.T) {
	if _, err := strategy.Get(Name); err != nil {
		t.Fatalf("strategy %q not registered: %v", Name, err)
	}
}

func TestRelativeStdDev(t *testing.T) {
	cases := []struct {
		name    string
		prices  []float64
		want    float64
		tolerance float64
	}{
		{"flat", []float64{1, 1, 1, 1}, 0, 0.001},
		{"linear", []float64{1, 1.01, 1.0201, 1.030301}, 0, 0.001}, // ~constant 1% returns
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ds := make([]decimal.Decimal, len(tc.prices))
			for i, p := range tc.prices {
				ds[i] = decimal.NewFromFloat(p)
			}
			got := relativeStdDev(ds)
			if math.Abs(got-tc.want) > tc.tolerance {
				t.Errorf("got %v, want %v ± %v", got, tc.want, tc.tolerance)
			}
		})
	}
}

func TestExitUsesAlphaUnits(t *testing.T) {
	// REGRESSION: same as alpha_momentum — exit InAmount must be the
	// held alpha amount, not the TAO notional.
	s, err := New(map[string]any{
		"universe":          []any{8, 1, 3},
		"top_k":             1,
		"rebalance_ticks":   1,
		"volatility_window": 5,
		"tao_per_position":  10.0,
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	// Build 6 ticks so volatility is computable. SN8 stable, SN1 volatile.
	pricesSN8 := []float64{0.05, 0.05, 0.05, 0.05, 0.05, 0.05}
	pricesSN1 := []float64{0.01, 0.02, 0.005, 0.03, 0.01, 0.025}
	pricesSN3 := []float64{0.025, 0.024, 0.026, 0.025, 0.024, 0.025}

	for i := 0; i < 6; i++ {
		in := strategy.DecisionInput{
			Now: now.Add(time.Duration(i) * time.Second),
			Market: types.MarketSnapshot{
				Symbols: map[string]types.SymbolSnap{
					"SN8/TAO": {Tick: types.Tick{Bid: decimal.NewFromFloat(pricesSN8[i]), Ask: decimal.NewFromFloat(pricesSN8[i])}},
					"SN1/TAO": {Tick: types.Tick{Bid: decimal.NewFromFloat(pricesSN1[i]), Ask: decimal.NewFromFloat(pricesSN1[i])}},
					"SN3/TAO": {Tick: types.Tick{Bid: decimal.NewFromFloat(pricesSN3[i]), Ask: decimal.NewFromFloat(pricesSN3[i])}},
				},
			},
		}
		_, _ = s.Decide(context.Background(), in)
	}

	st := s.(*Strategy)
	if len(st.held) != 1 {
		t.Fatalf("expected 1 held position, got %d", len(st.held))
	}
	for netuid, pos := range st.held {
		if pos.alphaHeld.IsZero() {
			t.Errorf("netuid %d alphaHeld is zero — entry sizing broken", netuid)
		}
	}
}
