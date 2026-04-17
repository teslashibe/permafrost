package agent

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	exchangenoop "github.com/teslashibe/permafrost/internal/exchange/noop"
	"github.com/teslashibe/permafrost/internal/strategy"
	swapnoop "github.com/teslashibe/permafrost/internal/swap/noop"
	"github.com/teslashibe/permafrost/internal/types"
)

// scriptedStrategy returns the same Decision every tick.
type scriptedStrategy struct {
	name     string
	decision strategy.Decision
}

func (s *scriptedStrategy) Name() string                                       { return s.name }
func (s *scriptedStrategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }
func (s *scriptedStrategy) Decide(_ context.Context, _ strategy.DecisionInput) (strategy.Decision, error) {
	return s.decision, nil
}

func TestTickOnce_PaperMode_RecordsButDoesNotCallVenues(t *testing.T) {
	perp := exchangenoop.New()
	swp := swapnoop.New()
	strat := &scriptedStrategy{
		name: "test",
		decision: strategy.Decision{
			Notes: "open WIF basis",
			Swaps: []types.SwapIntent{{
				Chain:    types.ChainSolana,
				InToken:  types.Asset{Symbol: "USDC", Chain: types.ChainSolana, Mint: "USDCmint", Decimals: 6},
				OutToken: types.Asset{Symbol: "WIF", Chain: types.ChainSolana, Mint: "WIFmint", Decimals: 6},
				InAmount: decimal.NewFromInt(100),
			}},
			Orders: []types.OrderIntent{{
				Venue: "hyperliquid", Symbol: "WIF", Side: types.SideSell,
				Type: types.OrderTypeLimit, Price: decimal.NewFromInt(1), Size: decimal.NewFromInt(100),
			}},
		},
	}
	a := Agent{ID: "a1", Strategy: "test", Mode: ModePaper, PerpVenue: "hyperliquid", SpotVenue: "jupiter"}
	r := NewRuntime(a, Deps{
		Strategy: strat,
		Perp:     perp,
		Swap:     swp,
	})
	r.SetClock(func() time.Time { return time.Unix(1_700_000_000, 0).UTC() })

	dec, err := r.TickOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if dec.Notes != "open WIF basis" {
		t.Errorf("decision passed through: %+v", dec)
	}
	if got := perp.Placed(); len(got) != 0 {
		t.Errorf("paper mode should not call venue Place, got %d", len(got))
	}
	if got := swp.Submitted(); len(got) != 0 {
		t.Errorf("paper mode should not call swap Swap, got %d", len(got))
	}
}

func TestTickOnce_LiveMode_CallsVenues(t *testing.T) {
	perp := exchangenoop.New()
	swp := swapnoop.New()
	strat := &scriptedStrategy{
		name: "test",
		decision: strategy.Decision{
			Swaps: []types.SwapIntent{{
				Chain:    types.ChainSolana,
				InToken:  types.Asset{Symbol: "USDC", Chain: types.ChainSolana, Mint: "USDCmint", Decimals: 6},
				OutToken: types.Asset{Symbol: "WIF", Chain: types.ChainSolana, Mint: "WIFmint", Decimals: 6},
				InAmount: decimal.NewFromInt(100),
			}},
			Orders: []types.OrderIntent{{
				Venue: "hyperliquid", Symbol: "WIF", Side: types.SideSell,
				Type: types.OrderTypeLimit, Price: decimal.NewFromInt(1), Size: decimal.NewFromInt(100),
			}},
		},
	}
	a := Agent{ID: "a1", Strategy: "test", Mode: ModeLive, PerpVenue: "hyperliquid", SpotVenue: "noop"}
	r := NewRuntime(a, Deps{
		Strategy: strat,
		Perp:     perp,
		Swap:     swp,
	})

	if _, err := r.TickOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := swp.Submitted(); len(got) != 1 {
		t.Errorf("expected 1 swap submitted, got %d", len(got))
	}
	if got := perp.Placed(); len(got) != 1 {
		t.Errorf("expected 1 order placed, got %d", len(got))
	}
}

func TestRuntime_StartStop(t *testing.T) {
	strat := &scriptedStrategy{name: "noop"}
	a := Agent{ID: "a1", Strategy: "noop", Mode: ModePaper, TickSecs: 1}
	r := NewRuntime(a, Deps{Strategy: strat})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	if !r.IsRunning() {
		t.Fatal("expected running")
	}
	if err := r.Start(ctx); err == nil {
		t.Fatal("double-start should fail")
	}

	stopCtx, stopCancel := context.WithTimeout(context.Background(), time.Second)
	defer stopCancel()
	if err := r.Stop(stopCtx, "test"); err != nil {
		t.Fatal(err)
	}
	if r.IsRunning() {
		t.Fatal("expected stopped")
	}
}
