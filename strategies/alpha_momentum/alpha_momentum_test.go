package alpha_momentum

import (
	"context"
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

func TestExitUsesAlphaUnits_NotTAO(t *testing.T) {
	// REGRESSION: previously, exits used TAOPerPosition for InAmount
	// which is wrong because the input token is alpha, not TAO.
	// This test pins the correct behaviour: InAmount is the held alpha
	// amount, computed as TAOPerPosition / entryPrice.
	s, err := New(map[string]any{
		"universe":         []any{8},
		"window_ticks":     2,
		"top_k":            1,
		"exit_threshold":   -0.001,
		"tao_per_position": 5.0,
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()

	// Tick 1: positive momentum (price 0.05). Strategy should buy.
	in1 := strategy.DecisionInput{
		Now: now,
		Market: types.MarketSnapshot{
			Time: now,
			Symbols: map[string]types.SymbolSnap{
				"SN8/TAO": {Tick: types.Tick{
					Bid: decimal.NewFromFloat(0.04),
					Ask: decimal.NewFromFloat(0.04),
				}},
			},
		},
	}
	d1, err := s.Decide(context.Background(), in1)
	if err != nil {
		t.Fatal(err)
	}
	// Tick 1: only one price point — momentum can't be computed. No swaps.
	if len(d1.Swaps) != 0 {
		t.Fatalf("tick 1: expected 0 swaps (insufficient history), got %d", len(d1.Swaps))
	}

	// Tick 2: price up. With 2 points we have positive momentum → enter.
	in2 := strategy.DecisionInput{
		Now: now.Add(time.Second),
		Market: types.MarketSnapshot{
			Symbols: map[string]types.SymbolSnap{
				"SN8/TAO": {Tick: types.Tick{
					Bid: decimal.NewFromFloat(0.05),
					Ask: decimal.NewFromFloat(0.05),
				}},
			},
		},
	}
	d2, _ := s.Decide(context.Background(), in2)
	if len(d2.Swaps) != 1 {
		t.Fatalf("tick 2: expected 1 entry, got %d swaps: %+v", len(d2.Swaps), d2.Swaps)
	}
	if d2.Swaps[0].Tag != "alpha_momentum_enter" {
		t.Errorf("entry tag: got %q", d2.Swaps[0].Tag)
	}
	if d2.Swaps[0].InToken.Mint != "TAO" {
		t.Errorf("entry InToken should be TAO, got %q", d2.Swaps[0].InToken.Mint)
	}

	// Tick 3: big price drop → momentum negative → exit.
	in3 := strategy.DecisionInput{
		Now: now.Add(2 * time.Second),
		Market: types.MarketSnapshot{
			Symbols: map[string]types.SymbolSnap{
				"SN8/TAO": {Tick: types.Tick{
					Bid: decimal.NewFromFloat(0.01),
					Ask: decimal.NewFromFloat(0.01),
				}},
			},
		},
	}
	d3, _ := s.Decide(context.Background(), in3)
	if len(d3.Swaps) != 1 {
		t.Fatalf("tick 3: expected 1 exit, got %d", len(d3.Swaps))
	}
	exit := d3.Swaps[0]
	if exit.Tag != "alpha_momentum_exit" {
		t.Errorf("exit tag: got %q", exit.Tag)
	}
	if exit.InToken.Mint != "SN8" {
		t.Errorf("exit InToken should be SN8 (alpha), got %q", exit.InToken.Mint)
	}
	if exit.OutToken.Mint != "TAO" {
		t.Errorf("exit OutToken should be TAO, got %q", exit.OutToken.Mint)
	}

	// THE BUG FIX: InAmount should be in alpha units, not TAO.
	// At entry price 0.05, 5 TAO buys ~100 alpha. Exit must sell ~100,
	// not 5 (the old bug).
	if exit.InAmount.LessThan(decimal.NewFromInt(50)) {
		t.Errorf("exit InAmount should be ~100 alpha (5 TAO / 0.05), got %s — BUG: still using TAO units?",
			exit.InAmount)
	}
	if exit.InAmount.GreaterThan(decimal.NewFromInt(200)) {
		t.Errorf("exit InAmount unreasonably high: %s", exit.InAmount)
	}
}
