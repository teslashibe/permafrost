package types

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestAssetEqual(t *testing.T) {
	a := Asset{Symbol: "USDC", Chain: ChainSolana, Mint: "ABC"}
	b := Asset{Symbol: "USD-Coin", Chain: ChainSolana, Mint: "ABC"} // different symbol, same identity
	c := Asset{Symbol: "USDC", Chain: ChainSolana, Mint: "DEF"}

	if !a.Equal(b) {
		t.Errorf("Equal: identity should be (chain, mint) not symbol")
	}
	if a.Equal(c) {
		t.Errorf("Equal: different mint should be unequal")
	}
}

func TestOrderIntentNotional(t *testing.T) {
	o := OrderIntent{Price: d("100.5"), Size: d("2")}
	if got := o.Notional(); !got.Equal(d("201")) {
		t.Errorf("Notional: got %s want 201", got)
	}
	o.Price = d("0")
	if got := o.Notional(); !got.IsZero() {
		t.Errorf("Notional with zero price: got %s", got)
	}
}

func TestDeriveClientID_Deterministic(t *testing.T) {
	a := DeriveClientID("agent1", "dec1", 0, "hyperliquid", "WIF-PERP", SideSell, d("100"))
	b := DeriveClientID("agent1", "dec1", 0, "hyperliquid", "WIF-PERP", SideSell, d("100"))
	if a != b {
		t.Errorf("DeriveClientID should be deterministic, got %q vs %q", a, b)
	}
}

func TestDeriveClientID_DiffersOnSlot(t *testing.T) {
	a := DeriveClientID("a", "d", 0, "v", "s", SideBuy, d("1"))
	b := DeriveClientID("a", "d", 1, "v", "s", SideBuy, d("1"))
	if a == b {
		t.Errorf("DeriveClientID should change with slot, got %q", a)
	}
}

func TestPositionFlags(t *testing.T) {
	if !(Position{Qty: d("1")}).IsLong() {
		t.Error("IsLong: positive qty")
	}
	if !(Position{Qty: d("-1")}).IsShort() {
		t.Error("IsShort: negative qty")
	}
	if !(Position{Qty: d("0")}).IsFlat() {
		t.Error("IsFlat: zero qty")
	}
}

func TestBalanceTotal(t *testing.T) {
	b := Balance{Free: d("3"), Locked: d("2")}
	if got := b.Total(); !got.Equal(d("5")) {
		t.Errorf("Total: got %s want 5", got)
	}
}

func TestTickMid(t *testing.T) {
	if got := (Tick{Bid: d("100"), Ask: d("102")}).Mid(); !got.Equal(d("101")) {
		t.Errorf("Mid: got %s want 101", got)
	}
	if got := (Tick{Last: d("99")}).Mid(); !got.Equal(d("99")) {
		t.Errorf("Mid: empty book should fall back to last, got %s", got)
	}
}

func TestFundingAnnualised(t *testing.T) {
	// 0.0001 (1bp) per hour = 1bp * 24 * 365 = 87600bp = 8.76 (876%) annualised
	f := FundingRate{Rate: d("0.0001"), Interval: time.Hour}
	got := f.Annualised()
	want := d("0.876")
	if !got.Equal(want) {
		t.Errorf("Annualised: got %s want %s", got, want)
	}
}

func TestFundingAnnualised_ZeroInterval(t *testing.T) {
	f := FundingRate{Rate: d("0.0001"), Interval: 0}
	if !f.Annualised().IsZero() {
		t.Errorf("Annualised with 0 interval should be 0")
	}
}

func TestBasisPositionNetPnL(t *testing.T) {
	bp := BasisPosition{
		RealizedFunding:  d("100"),
		RealizedBasisPnL: d("10"),
		RealizedFees:     d("5"),
		RealizedGas:      d("1"),
	}
	if got := bp.NetPnL(); !got.Equal(d("104")) {
		t.Errorf("NetPnL: got %s want 104", got)
	}
}

func TestPortfolioTotalBasisExposure(t *testing.T) {
	snap := PortfolioSnapshot{
		OpenBasis: []BasisPosition{
			{Legs: []BasisLeg{
				{Kind: BasisLegSpot, Qty: d("10"), AvgPrice: d("100")},
				{Kind: BasisLegPerp, Qty: d("10"), AvgPrice: d("101")},
			}},
			{Legs: []BasisLeg{
				{Kind: BasisLegPerp, Qty: d("5"), AvgPrice: d("200")},
			}},
		},
	}
	got := snap.TotalBasisExposure()
	want := d("2010") // 10*101 + 5*200
	if !got.Equal(want) {
		t.Errorf("TotalBasisExposure: got %s want %s", got, want)
	}
}
