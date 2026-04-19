package pnl

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

// stubPerp implements just enough of exchange.Venue for ValueAgent's
// Positions() call. The other methods aren't exercised by the engine.
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

// stubSwap implements just enough of swap.SwapVenue for the spot leg
// quote in valueOne. quote is the OUT amount returned for any input.
type stubSwap struct {
	chain     types.ChainID
	outAmount decimal.Decimal
	err       error
}

func (s *stubSwap) Name() string         { return "stubswap" }
func (s *stubSwap) Chain() types.ChainID { return s.chain }
func (s *stubSwap) Quote(_ context.Context, req types.QuoteRequest) (types.Quote, error) {
	if s.err != nil {
		return types.Quote{}, s.err
	}
	return types.Quote{
		InToken: req.InToken, OutToken: req.OutToken,
		InAmount: req.Amount, OutAmount: s.outAmount,
		ExpiresAt: time.Now().Add(20 * time.Second),
	}, nil
}
func (s *stubSwap) Swap(_ context.Context, _ types.Quote, _ int) (types.TxHash, error) {
	return "stub-tx", nil
}
func (s *stubSwap) WaitConfirm(_ context.Context, tx types.TxHash) (types.SwapResult, error) {
	return types.SwapResult{TxHash: tx, Status: types.SwapStatusConfirmed}, nil
}
func (s *stubSwap) Balance(_ context.Context, _ types.Asset) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

// makeBasis builds a synthetic open basis position. spotCost is what
// we paid in USDC; perpSize is the perp short size; entryPx is the
// perp entry price.
func makeBasis(underlying, perpSym string, chain types.ChainID, spotCostUSDC, perpSize, entryPx decimal.Decimal) types.BasisPosition {
	return types.BasisPosition{
		ID:         "bp:test:" + underlying,
		AgentID:    "test-agent",
		Underlying: underlying,
		State:      types.BasisStateOpen,
		OpenedAt:   time.Now().Add(-time.Hour).UTC(),
		Legs: []types.BasisLeg{
			{Kind: types.BasisLegSpot, Asset: types.Asset{
				Symbol: underlying, Chain: chain, Mint: "stub-mint", Decimals: 6,
			}, Qty: spotCostUSDC},
			{Kind: types.BasisLegPerp, Symbol: perpSym, Qty: perpSize, AvgPrice: entryPx},
		},
	}
}

func TestValueAgent_NoVenues_DegradedBoth(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"), d("100"), d("1.0"))
	e := New(nil, nil)
	nav, err := e.ValueAgent(context.Background(), "a", History{
		OpenPositions: []types.BasisPosition{bp},
	})
	if err != nil {
		t.Fatal(err)
	}
	if nav.OpenPositions != 1 {
		t.Errorf("open positions: %d", nav.OpenPositions)
	}
	got := nav.Positions[0]
	if got.Status != ValuationDegradedBoth {
		t.Errorf("status: %q", got.Status)
	}
	if !got.SpotValueUSDC.Equal(d("100")) {
		t.Errorf("spot value: %s want 100", got.SpotValueUSDC)
	}
	if !got.PerpUnrealizedUSDC.IsZero() {
		t.Errorf("perp unrealized should be 0 without HL: %s", got.PerpUnrealizedUSDC)
	}
	if !got.NetUnrealizedUSDC.IsZero() {
		t.Errorf("net unrealized should be 0 without venue marks: %s", got.NetUnrealizedUSDC)
	}
}

// TestValueAgent_PerpUnrealized: HL reports +5 USDC unrealized → flows
// straight through to NetUnrealized (assuming no spot drift).
func TestValueAgent_PerpUnrealized(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"), d("100"), d("1.0"))
	perp := &stubPerp{positions: []types.Position{
		{
			Venue:        "stub",
			Symbol:       "WIF",
			Qty:          d("-100"), // SHORT
			EntryPrice:   d("1.0"),
			UnrealizedPx: d("5.0"), // funding rate moved in our favour
		},
	}}
	e := New(perp, nil)
	nav, err := e.ValueAgent(context.Background(), "a", History{
		OpenPositions: []types.BasisPosition{bp},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := nav.Positions[0]
	if !got.PerpUnrealizedUSDC.Equal(d("5")) {
		t.Errorf("perp unrealized: %s want 5", got.PerpUnrealizedUSDC)
	}
	if got.Status != ValuationDegradedSpot {
		t.Errorf("status: %q want spot_at_cost", got.Status)
	}
	// Spot at cost (no swap venue), perp +5, no funding/gas → net = 5
	if !got.NetUnrealizedUSDC.Equal(d("5")) {
		t.Errorf("net unrealized: %s want 5", got.NetUnrealizedUSDC)
	}
	if !nav.NAVUSDC.Equal(d("5")) {
		t.Errorf("nav: %s want 5", nav.NAVUSDC)
	}
}

// TestValueAgent_SpotMarkUp: spot leg appreciated +2 USDC, perp moved
// against us −2 USDC → net delta-neutral by design.
func TestValueAgent_SpotMarkUp(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"), d("100"), d("1.0"))
	perp := &stubPerp{positions: []types.Position{
		{Venue: "s", Symbol: "WIF", Qty: d("-100"), EntryPrice: d("1.0"),
			UnrealizedPx: d("-2.0")},
	}}
	swp := &stubSwap{chain: types.ChainSolana, outAmount: d("102")} // 100 tokens worth $102 now
	e := New(perp, map[types.ChainID]swap.SwapVenue{types.ChainSolana: swp})

	nav, err := e.ValueAgent(context.Background(), "a", History{
		OpenPositions: []types.BasisPosition{bp},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := nav.Positions[0]
	if got.Status != ValuationOK {
		t.Errorf("status: %q want ok", got.Status)
	}
	if !got.SpotValueUSDC.Equal(d("102")) {
		t.Errorf("spot value: %s want 102", got.SpotValueUSDC)
	}
	// Spot +2, perp -2 → net 0 (perfect hedge)
	if !got.NetUnrealizedUSDC.IsZero() {
		t.Errorf("net unrealized should be 0 (delta-neutral): %s", got.NetUnrealizedUSDC)
	}
}

// TestValueAgent_RealizedAndGas: history aggregates flow into NAV.
func TestValueAgent_RealizedAndGas(t *testing.T) {
	e := New(nil, nil)
	nav, err := e.ValueAgent(context.Background(), "a", History{
		RealizedPnLUSDC:   d("12.50"),
		CumulativeGasUSDC: d("3.20"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !nav.RealizedPnLUSDC.Equal(d("12.5")) {
		t.Errorf("realized: %s", nav.RealizedPnLUSDC)
	}
	// No open positions, no unrealized; NAV == realized.
	if !nav.NAVUSDC.Equal(d("12.5")) {
		t.Errorf("nav: %s want 12.5", nav.NAVUSDC)
	}
	if nav.OpenPositions != 0 {
		t.Errorf("open positions: %d", nav.OpenPositions)
	}
}

// TestValueAgent_PerpRPCError_DegradesGracefully: a venue error must NOT
// fail the whole valuation — it should degrade to "spot_at_cost"-style
// status. The runtime can't be allowed to crash because HL is flaky.
func TestValueAgent_PerpRPCError_DegradesGracefully(t *testing.T) {
	bp := makeBasis("WIF", "WIF", types.ChainSolana, d("100"), d("100"), d("1.0"))
	perp := &stubPerp{err: errors.New("rpc down")}
	e := New(perp, nil)
	nav, err := e.ValueAgent(context.Background(), "a", History{
		OpenPositions: []types.BasisPosition{bp},
	})
	if err != nil {
		t.Fatalf("ValueAgent must not fail on perp RPC error: %v", err)
	}
	if nav.Positions[0].Status != ValuationDegradedBoth {
		t.Errorf("status: %q (expected both — perp errored, no swap venue)",
			nav.Positions[0].Status)
	}
}

// TestValueAgent_DeterministicOrdering keeps the JSONB stable so DB
// queries can compare snapshots cleanly.
func TestValueAgent_DeterministicOrdering(t *testing.T) {
	bps := []types.BasisPosition{
		makeBasis("WIF", "WIF", types.ChainSolana, d("100"), d("100"), d("1")),
		makeBasis("BONK", "BONK", types.ChainSolana, d("100"), d("100"), d("0.001")),
		makeBasis("AVAX", "AVAX", types.ChainAvalanche, d("100"), d("3"), d("30")),
	}
	e := New(nil, nil)
	nav, _ := e.ValueAgent(context.Background(), "a", History{OpenPositions: bps})
	want := []string{"AVAX", "BONK", "WIF"}
	for i, p := range nav.Positions {
		if p.Underlying != want[i] {
			t.Errorf("position %d: got %s want %s", i, p.Underlying, want[i])
		}
	}
}
