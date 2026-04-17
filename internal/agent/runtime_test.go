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

// TestTickOnce_ReconcilesOpenBasis exercises the runtime's in-memory
// position bookkeeping: after a Decision opens a basis, the next tick's
// DecisionInput.BasisPositions reflects it (so the strategy doesn't
// re-emit the same open).
func TestTickOnce_ReconcilesOpenBasis(t *testing.T) {
	openIntent := strategy.Decision{
		Notes: "open WIF",
		Swaps: []types.SwapIntent{{
			Chain:    types.ChainSolana,
			InToken:  types.Asset{Symbol: "USDC", Chain: types.ChainSolana},
			OutToken: types.Asset{Symbol: "WIF", Chain: types.ChainSolana, Mint: "wifmint"},
			InAmount: decimal.NewFromInt(100),
		}},
		Orders: []types.OrderIntent{{
			Venue: "hyperliquid", Symbol: "WIF", Side: types.SideSell,
			Type: types.OrderTypeMarket, Size: decimal.NewFromInt(100),
		}},
	}

	captured := []strategy.DecisionInput{}
	strat := &captureStrategy{
		decisions: []strategy.Decision{openIntent, openIntent}, // same intent twice
		captured:  &captured,
	}

	a := Agent{ID: "a1", Strategy: "test", Mode: ModePaper}
	r := NewRuntime(a, Deps{Strategy: strat})

	if _, err := r.TickOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(r.snapshotOpenBasis()); got != 1 {
		t.Fatalf("after first tick, expected 1 open basis, got %d", got)
	}

	if _, err := r.TickOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(captured); got != 2 {
		t.Fatalf("strategy should have been called twice, got %d", got)
	}
	// Second tick must have seen the open basis.
	if got := len(captured[1].BasisPositions); got != 1 {
		t.Errorf("second tick BasisPositions: got %d want 1", got)
	}
	if captured[1].BasisPositions[0].Underlying != "WIF" {
		t.Errorf("expected WIF, got %s", captured[1].BasisPositions[0].Underlying)
	}
}

// captureStrategy records the DecisionInput it was called with and replays
// scripted Decisions in order.
type captureStrategy struct {
	decisions []strategy.Decision
	captured  *[]strategy.DecisionInput
	calls     int
}

func (s *captureStrategy) Name() string                                          { return "capture" }
func (s *captureStrategy) Warmup(_ context.Context, _ strategy.WarmupInput) error { return nil }
func (s *captureStrategy) Decide(_ context.Context, in strategy.DecisionInput) (strategy.Decision, error) {
	*s.captured = append(*s.captured, in)
	d := s.decisions[s.calls%len(s.decisions)]
	s.calls++
	return d, nil
}

func TestReconcileOpenBasis_ClosesOnReduceOnly(t *testing.T) {
	a := Agent{ID: "a", Strategy: "x", Mode: ModePaper}
	r := NewRuntime(a, Deps{Strategy: nil})
	r.openBasis["WIF"] = types.BasisPosition{
		Underlying: "WIF",
		State:      types.BasisStateOpen,
		Legs: []types.BasisLeg{
			{Kind: types.BasisLegPerp, Symbol: "WIF", Qty: decimal.NewFromInt(100)},
		},
	}
	r.reconcileOpenBasis(context.Background(), time.Now(), "dec1", strategy.Decision{
		Orders: []types.OrderIntent{{
			Venue: "hyperliquid", Symbol: "WIF", Side: types.SideBuy,
			ReduceOnly: true, Type: types.OrderTypeMarket, Size: decimal.NewFromInt(100),
		}},
	})
	if len(r.snapshotOpenBasis()) != 0 {
		t.Errorf("reduce-only buy should have closed the basis")
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
