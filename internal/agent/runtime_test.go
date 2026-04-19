package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	exchangenoop "github.com/teslashibe/permafrost/internal/exchange/noop"
	"github.com/teslashibe/permafrost/internal/risk"
	"github.com/teslashibe/permafrost/internal/swap"
	swapnoop "github.com/teslashibe/permafrost/internal/swap/noop"
	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

// risknew is a small helper for runtime tests: builds a risk.Policy with
// just MaxConcurrentPositions set and no breakers (zero drawdown
// disables it).
func risknew(maxConcurrent int) *risk.Policy {
	return risk.NewPolicy(types.RiskLimits{MaxConcurrentPositions: maxConcurrent})
}

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
		Swaps:    map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp},
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
		Swaps:    map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp},
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
	}, nil, nil) // nil success maps = treat all as success
	if len(r.snapshotOpenBasis()) != 0 {
		t.Errorf("reduce-only buy should have closed the basis")
	}
}

// TestReconcileOpenBasis_SkipsHalfOpenAfterFailedSwap covers the new
// per-leg success tracking: if the spot swap failed, the basis must NOT
// be reconciled as open (we'd believe we're hedged when we're actually
// directional via the perp leg only).
func TestReconcileOpenBasis_SkipsHalfOpenAfterFailedSwap(t *testing.T) {
	a := Agent{ID: "a", Strategy: "x", Mode: ModeLive}
	r := NewRuntime(a, Deps{Strategy: nil})

	dec := strategy.Decision{
		Swaps: []types.SwapIntent{{
			Chain:    types.ChainSolana,
			InToken:  types.Asset{Symbol: "USDC"},
			OutToken: types.Asset{Symbol: "WIF", Mint: "wif"},
			InAmount: decimal.NewFromInt(100),
		}},
		Orders: []types.OrderIntent{{
			Venue: "hyperliquid", Symbol: "WIF", Side: types.SideSell,
			Type: types.OrderTypeMarket, Size: decimal.NewFromInt(100),
		}},
	}
	// swap failed (WIF not in swapOK), order succeeded
	r.reconcileOpenBasis(context.Background(), time.Now(), "decX", dec,
		map[string]bool{},                  // swapOK: empty → swap failed
		map[string]bool{"WIF": true})       // orderOK: WIF placed
	if got := len(r.snapshotOpenBasis()); got != 0 {
		t.Errorf("expected NO basis when swap failed, got %d", got)
	}
}

// TestTickOnce_RiskCapBlocksThirdOpenInSameTick is the integration
// proof for the new wiring. Strategy emits 3 paired opens in one tick
// with MaxConcurrentPositions=2 — only the first 2 should reach the
// venue; the 3rd is blocked at the risk gate.
func TestTickOnce_RiskCapBlocksThirdOpenInSameTick(t *testing.T) {
	mk := func(sym string) (types.SwapIntent, types.OrderIntent) {
		return types.SwapIntent{
				Chain:    types.ChainSolana,
				InToken:  types.Asset{Symbol: "USDC", Chain: types.ChainSolana},
				OutToken: types.Asset{Symbol: sym, Chain: types.ChainSolana, Mint: sym + "_mint"},
				InAmount: decimal.NewFromInt(100),
			},
			types.OrderIntent{
				Venue: "hyperliquid", Symbol: sym, Side: types.SideSell,
				Type: types.OrderTypeLimit, Price: decimal.NewFromInt(1), Size: decimal.NewFromInt(100),
			}
	}
	swapWIF, orderWIF := mk("WIF")
	swapBONK, orderBONK := mk("BONK")
	swapPOPCAT, orderPOPCAT := mk("POPCAT")

	dec := strategy.Decision{
		Notes:  "open 3",
		Swaps:  []types.SwapIntent{swapWIF, swapBONK, swapPOPCAT},
		Orders: []types.OrderIntent{orderWIF, orderBONK, orderPOPCAT},
	}

	perp := exchangenoop.New()
	swp := swapnoop.New()
	a := Agent{ID: "a", Strategy: "x", Mode: ModeLive}

	// Cap at 2 concurrent positions. No drawdown breaker (zero MaxFraction).
	policy := risknew(2)

	rt := NewRuntime(a, Deps{
		Strategy: &scriptedStrategy{name: "x", decision: dec},
		Perp:     perp,
		Swaps:    map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp},
		Risk:     policy,
	})

	if _, err := rt.TickOnce(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Two pairs (WIF, BONK) should have hit the venues; POPCAT blocked.
	if got := len(swp.Submitted()); got != 2 {
		t.Errorf("swaps submitted: got %d want 2", got)
	}
	if got := len(perp.Placed()); got != 2 {
		t.Errorf("orders placed: got %d want 2", got)
	}

	// Open basis (after reconcile) should be exactly 2.
	if got := len(rt.snapshotOpenBasis()); got != 2 {
		t.Errorf("open basis after tick: got %d want 2", got)
	}
}

// TestTickOnce_RiskAllowsCloseAtCap: at cap, a reduce-only close MUST
// still pass even though MaxConcurrentPositions is hit.
func TestTickOnce_RiskAllowsCloseAtCap(t *testing.T) {
	perp := exchangenoop.New()
	a := Agent{ID: "a", Strategy: "x", Mode: ModeLive}

	rt := NewRuntime(a, Deps{
		Strategy: &scriptedStrategy{name: "x", decision: strategy.Decision{
			Orders: []types.OrderIntent{{
				Venue: "hyperliquid", Symbol: "WIF", Side: types.SideBuy,
				ReduceOnly: true, Type: types.OrderTypeMarket, Size: decimal.NewFromInt(100),
			}},
		}},
		Perp: perp,
		Risk: risknew(1),
	})
	// Pre-fill openBasis to cap.
	rt.openBasis["WIF"] = types.BasisPosition{Underlying: "WIF", State: types.BasisStateOpen}

	if _, err := rt.TickOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := len(perp.Placed()); got != 1 {
		t.Errorf("close should pass risk; got %d placed", got)
	}
}

func TestReconcileOpenBasis_OpensOnlyWhenBothLegsSucceed(t *testing.T) {
	a := Agent{ID: "a", Strategy: "x", Mode: ModeLive}
	r := NewRuntime(a, Deps{Strategy: nil})
	dec := strategy.Decision{
		Swaps: []types.SwapIntent{{
			Chain:    types.ChainSolana,
			InToken:  types.Asset{Symbol: "USDC"},
			OutToken: types.Asset{Symbol: "WIF", Mint: "wif"},
			InAmount: decimal.NewFromInt(100),
		}},
		Orders: []types.OrderIntent{{
			Venue: "hyperliquid", Symbol: "WIF", Side: types.SideSell,
			Type: types.OrderTypeMarket, Size: decimal.NewFromInt(100),
		}},
	}
	r.reconcileOpenBasis(context.Background(), time.Now(), "decY", dec,
		map[string]bool{"WIF": true},
		map[string]bool{"WIF": true})
	if got := len(r.snapshotOpenBasis()); got != 1 {
		t.Errorf("expected 1 basis when both legs succeed, got %d", got)
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

// recordingStrategy counts Warmup calls and records the WarmupInput it
// received, so tests can assert that the runtime invokes Warmup exactly
// once and populates Services correctly.
type recordingStrategy struct {
	name        string
	warmupCalls int
	warmupIn    strategy.WarmupInput
	warmupErr   error
	decision    strategy.Decision
}

func (s *recordingStrategy) Name() string { return s.name }
func (s *recordingStrategy) Warmup(_ context.Context, in strategy.WarmupInput) error {
	s.warmupCalls++
	s.warmupIn = in
	return s.warmupErr
}
func (s *recordingStrategy) Decide(_ context.Context, _ strategy.DecisionInput) (strategy.Decision, error) {
	return s.decision, nil
}

// TestRuntime_WarmupCalledExactlyOnce_ViaTickOnce verifies the runtime
// invokes Strategy.Warmup before the first decision even when the caller
// drives ticks directly via TickOnce (the foreground `agent run` and
// `agent tick` paths) and never invokes Start.
func TestRuntime_WarmupCalledExactlyOnce_ViaTickOnce(t *testing.T) {
	strat := &recordingStrategy{name: "rec"}
	a := Agent{ID: "ag-warmup", Strategy: "rec", Universe: []string{"WIF", "BONK"}, Mode: ModePaper}
	r := NewRuntime(a, Deps{Strategy: strat})

	for i := 0; i < 3; i++ {
		if _, err := r.TickOnce(context.Background()); err != nil {
			t.Fatalf("tick %d: %v", i, err)
		}
	}
	if strat.warmupCalls != 1 {
		t.Fatalf("Warmup should be called exactly once across multiple ticks, got %d", strat.warmupCalls)
	}
	if strat.warmupIn.AgentID != "ag-warmup" {
		t.Errorf("WarmupInput.AgentID: got %q want ag-warmup", strat.warmupIn.AgentID)
	}
	if got := strat.warmupIn.Universe; len(got) != 2 || got[0] != "WIF" || got[1] != "BONK" {
		t.Errorf("WarmupInput.Universe: got %v want [WIF BONK]", got)
	}
	if strat.warmupIn.Services.Logger == nil {
		t.Error("WarmupInput.Services.Logger should be non-nil (defaulted to slog.Default)")
	}
}

// TestRuntime_WarmupSurfacesError ensures a Warmup that returns an error
// causes every TickOnce to fail with the same wrapped error (sync.Once
// caches the result).
func TestRuntime_WarmupSurfacesError(t *testing.T) {
	want := errors.New("boom")
	strat := &recordingStrategy{name: "rec", warmupErr: want}
	a := Agent{ID: "ag-warmup-err", Strategy: "rec", Mode: ModePaper}
	r := NewRuntime(a, Deps{Strategy: strat})

	_, err1 := r.TickOnce(context.Background())
	_, err2 := r.TickOnce(context.Background())
	if err1 == nil || err2 == nil {
		t.Fatalf("both ticks should fail when Warmup errors; got %v / %v", err1, err2)
	}
	if !errors.Is(err1, want) || !errors.Is(err2, want) {
		t.Errorf("ticks should wrap the original Warmup error; got %v / %v", err1, err2)
	}
	if strat.warmupCalls != 1 {
		t.Errorf("Warmup should still only run once on error path, got %d calls", strat.warmupCalls)
	}
}

// TestRuntime_Services_PopulatedFromDeps verifies Services fields land
// in WarmupInput exactly as Deps was constructed. This is the regression
// test for the audit's H1 finding: an inference Provider supplied to
// Deps must surface as Services.Inference (and similarly for Registry +
// InferenceModel).
func TestRuntime_Services_PopulatedFromDeps(t *testing.T) {
	strat := &recordingStrategy{name: "rec"}
	a := Agent{ID: "ag-svc", Strategy: "rec", Mode: ModePaper}

	wantInf := mockInferenceProvider{name: "mock"}
	wantReg := assets.Registry{Version: 1, Assets: []assets.Asset{{Symbol: "WIF"}}}
	r := NewRuntime(a, Deps{
		Strategy:       strat,
		Inference:      wantInf,
		InferenceModel: "claude-sonnet-4.5",
		Registry:       wantReg,
	})

	if _, err := r.TickOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	svc := strat.warmupIn.Services
	if svc.Inference == nil || svc.Inference.Name() != "mock" {
		t.Errorf("Services.Inference: want non-nil mock, got %+v", svc.Inference)
	}
	if svc.InferenceModel != "claude-sonnet-4.5" {
		t.Errorf("Services.InferenceModel: got %q want claude-sonnet-4.5", svc.InferenceModel)
	}
	if svc.Registry.Version != 1 || len(svc.Registry.Assets) != 1 {
		t.Errorf("Services.Registry not propagated: got %+v", svc.Registry)
	}
}

// mockInferenceProvider satisfies pkg/inference.Provider with no real
// behaviour. Used in TestRuntime_Services_PopulatedFromDeps to verify
// the runtime threads a Provider through to the strategy unchanged.
type mockInferenceProvider struct{ name string }

func (m mockInferenceProvider) Name() string { return m.name }
func (m mockInferenceProvider) Complete(_ context.Context, _ inference.Request) (inference.Response, error) {
	return inference.Response{}, nil
}
func (m mockInferenceProvider) Embed(_ context.Context, _ inference.EmbedRequest) (inference.EmbedResponse, error) {
	return inference.EmbedResponse{}, nil
}
