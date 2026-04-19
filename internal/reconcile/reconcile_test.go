package reconcile

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/pkg/types"
)

func d(s string) decimal.Decimal {
	v, err := decimal.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return v
}

// stubs mirror the pnl package's stubs.

type stubPerp struct {
	positions []types.Position
	err       error
}

func (s *stubPerp) Name() string { return "stub" }
func (s *stubPerp) FundingRates(_ context.Context, _ []string) ([]types.FundingRate, error) {
	return nil, nil
}
func (s *stubPerp) Subscribe(_ context.Context, _ []string) (<-chan types.MarketEvent, error) {
	return nil, nil
}
func (s *stubPerp) Positions(_ context.Context) ([]types.Position, error) {
	return s.positions, s.err
}
func (s *stubPerp) Balances(_ context.Context) ([]types.Balance, error) { return nil, nil }
func (s *stubPerp) Place(_ context.Context, _ types.OrderIntent) (types.OrderAck, error) {
	return types.OrderAck{}, nil
}
func (s *stubPerp) Cancel(_ context.Context, _ types.OrderID) error { return nil }
func (s *stubPerp) OpenOrders(_ context.Context) ([]exchange.OpenOrder, error) {
	return nil, nil
}

type stubSwap struct {
	chain   types.ChainID
	balance decimal.Decimal
	err     error
}

func (s *stubSwap) Name() string         { return "stubswap" }
func (s *stubSwap) Chain() types.ChainID { return s.chain }
func (s *stubSwap) Quote(_ context.Context, req types.QuoteRequest) (types.Quote, error) {
	return types.Quote{InToken: req.InToken, OutToken: req.OutToken, OutAmount: req.Amount}, nil
}
func (s *stubSwap) Swap(_ context.Context, _ types.Quote, _ int) (types.TxHash, error) {
	return "stub", nil
}
func (s *stubSwap) WaitConfirm(_ context.Context, tx types.TxHash) (types.SwapResult, error) {
	return types.SwapResult{TxHash: tx, Status: types.SwapStatusConfirmed}, nil
}
func (s *stubSwap) Balance(_ context.Context, _ types.Asset) (decimal.Decimal, error) {
	return s.balance, s.err
}

func makeBasis(underlying, perpSym string, chain types.ChainID, perpSize decimal.Decimal) types.BasisPosition {
	return types.BasisPosition{
		ID:         "bp:" + underlying,
		AgentID:    "a",
		Underlying: underlying,
		State:      types.BasisStateOpen,
		OpenedAt:   time.Now().Add(-time.Hour),
		Legs: []types.BasisLeg{
			{Kind: types.BasisLegSpot, Asset: types.Asset{Symbol: underlying, Chain: chain, Mint: "m", Decimals: 6}, Qty: d("100")},
			{Kind: types.BasisLegPerp, Symbol: perpSym, Qty: perpSize, AvgPrice: d("1")},
		},
	}
}

// TestReconcile_PerfectMatch: book and venue agree → no drifts.
func TestReconcile_PerfectMatch(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	perp := &stubPerp{positions: []types.Position{
		{Symbol: "WIF", Qty: d("-100"), EntryPrice: d("1")},
	}}
	swp := &stubSwap{chain: types.ChainSolana, balance: d("100")}
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	rep, err := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Drifts) != 0 {
		t.Errorf("expected no drifts, got %+v", rep.Drifts)
	}
	if rep.OK != 1 {
		t.Errorf("OK count: %d want 1", rep.OK)
	}
}

// TestReconcile_PerpMissing: book says we're short 100, venue says we
// have nothing. CRITICAL — we believe we're hedged but we're not.
func TestReconcile_PerpMissing(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	perp := &stubPerp{positions: []types.Position{}} // empty
	swp := &stubSwap{chain: types.ChainSolana, balance: d("100")}
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	rep, _ := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	var found bool
	for _, drift := range rep.Drifts {
		if drift.Leg == "perp" && drift.Severity == SeverityCritical {
			found = true
		}
	}
	if !found {
		t.Errorf("expected critical perp-missing drift, got %+v", rep.Drifts)
	}
}

// TestReconcile_PerpFlippedSide: HL reports a LONG when we expected a
// short — strategy or operator bug. Always Critical.
func TestReconcile_PerpFlippedSide(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	perp := &stubPerp{positions: []types.Position{
		{Symbol: "WIF", Qty: d("100"), EntryPrice: d("1")}, // POSITIVE = long
	}}
	swp := &stubSwap{chain: types.ChainSolana, balance: d("100")}
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	rep, _ := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	var ok bool
	for _, dr := range rep.Drifts {
		if dr.Leg == "perp" && dr.Field == "side" && dr.Severity == SeverityCritical {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected critical side-flip drift, got %+v", rep.Drifts)
	}
}

// TestReconcile_SpotShortfall_BelowTolerance: wallet has 99.99 of 100
// — within the 25bps tolerance → NO drift.
func TestReconcile_SpotShortfall_BelowTolerance(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	perp := &stubPerp{positions: []types.Position{
		{Symbol: "WIF", Qty: d("-100"), EntryPrice: d("1")},
	}}
	swp := &stubSwap{chain: types.ChainSolana, balance: d("99.99")}
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	rep, _ := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	for _, dr := range rep.Drifts {
		if dr.Leg == "spot" && dr.Severity != SeverityInfo {
			t.Errorf("unexpected spot drift within tolerance: %+v", dr)
		}
	}
}

// TestReconcile_SpotShortfall_AboveTolerance: wallet has 95 of 100 (500bps
// = 5%) — above 100bps Critical threshold.
func TestReconcile_SpotShortfall_AboveTolerance(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	perp := &stubPerp{positions: []types.Position{
		{Symbol: "WIF", Qty: d("-100"), EntryPrice: d("1")},
	}}
	swp := &stubSwap{chain: types.ChainSolana, balance: d("95")}
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	rep, _ := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	var ok bool
	for _, dr := range rep.Drifts {
		if dr.Leg == "spot" && dr.Severity == SeverityCritical {
			ok = true
		}
	}
	if !ok {
		t.Errorf("expected critical spot drift, got %+v", rep.Drifts)
	}
}

// TestReconcile_SpotSurplus_NotReported: wallet has more than expected
// (manual deposit, gas top-up). Not flagged — only shortages matter.
func TestReconcile_SpotSurplus_NotReported(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	perp := &stubPerp{positions: []types.Position{
		{Symbol: "WIF", Qty: d("-100"), EntryPrice: d("1")},
	}}
	swp := &stubSwap{chain: types.ChainSolana, balance: d("500")}
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	rep, _ := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	for _, dr := range rep.Drifts {
		if dr.Leg == "spot" && dr.Severity != SeverityInfo {
			t.Errorf("unexpected drift on surplus: %+v", dr)
		}
	}
}

// TestReconcile_NoVenues_DegradesToInfo: nothing to verify against,
// every leg gets an Info entry.
func TestReconcile_NoVenues_DegradesToInfo(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	e := New(nil, nil)

	rep, _ := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	if len(rep.Drifts) != 2 {
		t.Errorf("expected 2 Info drifts (perp + spot), got %d", len(rep.Drifts))
	}
	for _, dr := range rep.Drifts {
		if dr.Severity != SeverityInfo {
			t.Errorf("expected Info severity, got %q", dr.Severity)
		}
	}
	if rep.HasIssues() {
		t.Error("Info-only report should not have issues")
	}
}

// TestReconcile_SpotBalanceError_Warning: a venue error on the balance
// query is a Warning (not Critical) — could be transient RPC glitch.
func TestReconcile_SpotBalanceError_Warning(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"))
	perp := &stubPerp{positions: []types.Position{
		{Symbol: "WIF", Qty: d("-100"), EntryPrice: d("1")},
	}}
	swp := &stubSwap{chain: types.ChainSolana, err: errors.New("rpc")}
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	rep, _ := e.Reconcile(context.Background(), "a", []types.BasisPosition{bp})
	var found bool
	for _, dr := range rep.Drifts {
		if dr.Leg == "spot" && dr.Severity == SeverityWarning {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Warning on RPC error, got %+v", rep.Drifts)
	}
}

func TestRelativeBps(t *testing.T) {
	tests := []struct {
		diff, expected string
		want           int
	}{
		{"0", "100", 0},
		{"1", "100", 100},   // 1% = 100bps
		{"0.5", "100", 50},  // 50bps
		{"5", "100", 500},   // 5%
		{"100", "0", 0},     // div-by-zero guard
	}
	for _, tc := range tests {
		got := relativeBps(d(tc.diff), d(tc.expected))
		if got != tc.want {
			t.Errorf("relativeBps(%s, %s) = %d want %d", tc.diff, tc.expected, got, tc.want)
		}
	}
}

func TestSeverityForRel(t *testing.T) {
	if severityForRel(50) != SeverityWarning {
		t.Error("50bps must be Warning")
	}
	if severityForRel(100) != SeverityWarning {
		t.Error("100bps must still be Warning (boundary)")
	}
	if severityForRel(101) != SeverityCritical {
		t.Error(">100bps must be Critical")
	}
}
