package risk

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/types"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestPolicy_PreTrade_OrderNotional(t *testing.T) {
	p := NewPolicy(types.RiskLimits{MaxNotionalPerLeg: d("1000")})
	intent := types.OrderIntent{Price: d("100"), Size: d("11")} // notional 1100
	v := p.PreTrade(context.Background(), "a", intent, types.PortfolioSnapshot{})
	if !v.IsBlock() {
		t.Errorf("expected block, got %+v", v)
	}
	if v.Reason != "max_notional_per_leg" {
		t.Errorf("reason: %q", v.Reason)
	}
}

func TestPolicy_PreTrade_OrderTotalExposure(t *testing.T) {
	p := NewPolicy(types.RiskLimits{MaxTotalBasisExposure: d("5000")})
	snap := types.PortfolioSnapshot{
		OpenBasis: []types.BasisPosition{{
			Legs: []types.BasisLeg{{Kind: types.BasisLegPerp, AvgPrice: d("100"), Qty: d("40")}}, // 4000
		}},
	}
	intent := types.OrderIntent{Price: d("100"), Size: d("20")} // 2000
	v := p.PreTrade(context.Background(), "a", intent, snap)
	if !v.IsBlock() || v.Reason != "max_total_exposure" {
		t.Errorf("expected total-exposure block, got %+v", v)
	}
}

func TestPolicy_PreTrade_ConcurrentPositions(t *testing.T) {
	p := NewPolicy(types.RiskLimits{MaxConcurrentPositions: 2})
	snap := types.PortfolioSnapshot{
		OpenBasis: []types.BasisPosition{{ID: "1"}, {ID: "2"}},
	}
	v := p.PreTrade(context.Background(), "a", types.OrderIntent{}, snap)
	if !v.IsBlock() || v.Reason != "max_concurrent_positions" {
		t.Errorf("expected positions block, got %+v", v)
	}
}

// TestPolicy_PreTrade_ClosesAlwaysAllowed: even at cap, reduce-only
// orders MUST pass — you have to be able to unwind.
func TestPolicy_PreTrade_ClosesAlwaysAllowed(t *testing.T) {
	p := NewPolicy(types.RiskLimits{MaxConcurrentPositions: 2})
	snap := types.PortfolioSnapshot{
		OpenBasis: []types.BasisPosition{{ID: "1"}, {ID: "2"}, {ID: "3"}}, // even over cap
	}
	close := types.OrderIntent{ReduceOnly: true}
	v := p.PreTrade(context.Background(), "a", close, snap)
	if v.IsBlock() {
		t.Errorf("reduce-only order must pass at any open count, got %+v", v)
	}
}

// TestPolicy_PreTrade_ClosingSwapAllowed: token→USDC swap is closing,
// shouldn't be blocked by the position cap.
func TestPolicy_PreTrade_ClosingSwapAllowed(t *testing.T) {
	p := NewPolicy(types.RiskLimits{MaxConcurrentPositions: 1})
	snap := types.PortfolioSnapshot{OpenBasis: []types.BasisPosition{{ID: "1"}}}
	closingSwap := types.SwapIntent{
		InToken:  types.Asset{Symbol: "WIF"},
		OutToken: types.Asset{Symbol: "USDC"},
		InAmount: d("100"),
	}
	if v := p.PreTrade(context.Background(), "a", closingSwap, snap); v.IsBlock() {
		t.Errorf("closing swap must pass at cap, got %+v", v)
	}

	openingSwap := types.SwapIntent{
		InToken:  types.Asset{Symbol: "USDC"},
		OutToken: types.Asset{Symbol: "WIF"},
		InAmount: d("100"),
	}
	if v := p.PreTrade(context.Background(), "a", openingSwap, snap); !v.IsBlock() {
		t.Errorf("opening swap MUST be blocked at cap, got %+v", v)
	}
}

func TestPolicy_PreTrade_SwapSlippage(t *testing.T) {
	p := NewPolicy(types.RiskLimits{MaxSpotSlippageBps: 50})
	intent := types.SwapIntent{SlippageBps: 100, InAmount: d("100")}
	v := p.PreTrade(context.Background(), "a", intent, types.PortfolioSnapshot{})
	if !v.IsBlock() || v.Reason != "max_spot_slippage_bps" {
		t.Errorf("expected slippage block, got %+v", v)
	}
}

func TestPolicy_PreTrade_SwapNotional(t *testing.T) {
	p := NewPolicy(types.RiskLimits{MaxNotionalPerLeg: d("100")})
	intent := types.SwapIntent{InAmount: d("200")}
	v := p.PreTrade(context.Background(), "a", intent, types.PortfolioSnapshot{})
	if !v.IsBlock() || v.Reason != "max_notional_per_leg" {
		t.Errorf("expected swap notional block, got %+v", v)
	}
}

func TestPolicy_Allows_WhenInLimits(t *testing.T) {
	p := NewPolicy(types.RiskLimits{
		MaxNotionalPerLeg:      d("10000"),
		MaxTotalBasisExposure:  d("100000"),
		MaxConcurrentPositions: 10,
		MaxSpotSlippageBps:     200,
	})
	o := types.OrderIntent{Price: d("100"), Size: d("10")}
	if v := p.PreTrade(context.Background(), "a", o, types.PortfolioSnapshot{}); v.IsBlock() {
		t.Errorf("order should pass: %+v", v)
	}
	s := types.SwapIntent{SlippageBps: 50, InAmount: d("100")}
	if v := p.PreTrade(context.Background(), "a", s, types.PortfolioSnapshot{}); v.IsBlock() {
		t.Errorf("swap should pass: %+v", v)
	}
}

func TestMaxDrawdownBreaker(t *testing.T) {
	b := MaxDrawdownBreaker{MaxFraction: d("0.10")}
	cases := []struct {
		name      string
		hwm, nav  string
		wantKind  types.VerdictKind
	}{
		{"green", "1000", "1000", types.VerdictAllow},
		{"warn", "1000", "915", types.VerdictWarn},   // 8.5% drawdown
		{"halt", "1000", "880", types.VerdictBlock},  // 12% drawdown
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			snap := types.PortfolioSnapshot{HighWaterMark: d(c.hwm), NAV: d(c.nav)}
			v := b.Check(snap, types.RiskLimits{}, drawdown(snap))
			if v.Kind != c.wantKind {
				t.Errorf("got %+v want %s", v, c.wantKind)
			}
		})
	}
}

func TestDailyLossBreaker(t *testing.T) {
	b := DailyLossBreaker{}
	limits := types.RiskLimits{MaxDailyLoss: d("100")}

	if v := b.Check(types.PortfolioSnapshot{DailyPnL: d("-50")}, limits, decimal.Zero); v.IsBlock() {
		t.Errorf("loss=50 < limit=100 should allow, got %+v", v)
	}
	if v := b.Check(types.PortfolioSnapshot{DailyPnL: d("-150")}, limits, decimal.Zero); !v.IsBlock() {
		t.Errorf("loss=150 >= limit=100 should block, got %+v", v)
	}
	if v := b.Check(types.PortfolioSnapshot{DailyPnL: d("-150")}, types.RiskLimits{}, decimal.Zero); v.IsBlock() {
		t.Errorf("zero limit should disable check, got %+v", v)
	}
}

func TestFundingFlipBreaker(t *testing.T) {
	b := FundingFlipBreaker{
		FundingByPositionID: map[string]decimal.Decimal{
			"open-1": d("-0.0001"),
		},
	}
	snap := types.PortfolioSnapshot{
		OpenBasis: []types.BasisPosition{{ID: "open-1"}, {ID: "open-2"}},
	}
	v := b.Check(snap, types.RiskLimits{}, decimal.Zero)
	if v.Kind != types.VerdictWarn {
		t.Errorf("expected warn, got %+v", v)
	}
}

func TestPolicy_Portfolio_BlocksOnFirstBreaker(t *testing.T) {
	p := NewPolicy(
		types.RiskLimits{MaxDailyLoss: d("100")},
		MaxDrawdownBreaker{MaxFraction: d("0.10")},
		DailyLossBreaker{},
	)
	snap := types.PortfolioSnapshot{
		HighWaterMark: d("1000"), NAV: d("700"), // 30% drawdown — block first
		DailyPnL: d("-200"),                     // also over loss limit
	}
	v := p.Portfolio(context.Background(), snap)
	if !v.IsBlock() {
		t.Errorf("expected block, got %+v", v)
	}
	if v.Reason != "max_drawdown" {
		t.Errorf("expected first breaker (max_drawdown), got %s", v.Reason)
	}
}
