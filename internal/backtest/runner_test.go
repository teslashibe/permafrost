package backtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/assets"
	fab "github.com/teslashibe/permafrost/strategies/private/funding_arb_basic"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func mkTick(t time.Time, sym, rate string, interval time.Duration) FundingTick {
	return FundingTick{Time: t, Symbol: sym, Rate: d(rate), Interval: interval}
}

func TestRunner_NoTicks(t *testing.T) {
	s, _ := fab.New(fab.Config{}, mustReg(t), nil)
	r := NewRunner(s, d("1000"), time.Hour, Costs{})
	if _, err := r.Run(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty ticks")
	}
}

func TestRunner_OpensAndClosesAcrossFundingChange(t *testing.T) {
	registry := mustReg(t)
	s, err := fab.New(fab.Config{
		EntryAnnualisedFunding: d("0.5"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
	}, registry, nil)
	if err != nil {
		t.Fatal(err)
	}

	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	// 24h of high funding, then 24h of low funding (BONK)
	var ticks []FundingTick
	for i := 0; i < 48; i++ {
		rate := "0.0001" // ann ≈ 0.876, opens
		if i >= 24 {
			rate = "0.000001" // ann ≈ 0.0088, closes
		}
		ticks = append(ticks, mkTick(t0.Add(time.Duration(i)*time.Hour), "WIF", rate, time.Hour))
	}

	r := NewRunner(s, d("1000"), time.Hour, Costs{})
	res, err := r.Run(context.Background(), ticks)
	if err != nil {
		t.Fatal(err)
	}
	if res.NumOpens == 0 {
		t.Fatalf("expected at least one open, got %d", res.NumOpens)
	}
	if res.NumCloses == 0 {
		t.Fatalf("expected at least one close, got %d", res.NumCloses)
	}
	if res.TotalFunding.IsZero() {
		t.Errorf("expected nonzero accrued funding, got %s", res.TotalFunding)
	}
}

func TestRunner_NeverOpensWhenBelowThreshold(t *testing.T) {
	registry := mustReg(t)
	s, _ := fab.New(fab.Config{
		EntryAnnualisedFunding: d("1.0"),
		ExitAnnualisedFunding:  d("0.1"),
		PositionCapUSDC:        d("100"),
	}, registry, nil)
	t0 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	var ticks []FundingTick
	for i := 0; i < 12; i++ {
		ticks = append(ticks, mkTick(t0.Add(time.Duration(i)*time.Hour), "WIF", "0.00001", time.Hour))
	}
	r := NewRunner(s, d("1000"), time.Hour, Costs{})
	res, err := r.Run(context.Background(), ticks)
	if err != nil {
		t.Fatal(err)
	}
	if res.NumOpens != 0 {
		t.Errorf("expected zero opens, got %d", res.NumOpens)
	}
	if !res.EndingNAV.Equal(d("1000")) {
		t.Errorf("NAV should be unchanged at %s, got %s", "1000", res.EndingNAV)
	}
}

func TestComputeMaxDrawdown(t *testing.T) {
	curve := []NAVPoint{
		{NAV: d("1000")},
		{NAV: d("1200")}, // peak
		{NAV: d("900")},  // 25% drawdown from 1200
		{NAV: d("1100")},
	}
	got := computeMaxDrawdown(curve)
	if !got.Equal(d("0.25")) {
		t.Errorf("max drawdown: got %s want 0.25", got)
	}
}

func TestReadCSV(t *testing.T) {
	csv := strings.NewReader(`time,symbol,rate,interval_seconds
2026-01-01T00:00:00Z,WIF,0.0001,3600
2026-01-01T01:00:00Z,WIF,0.00005,3600
`)
	rows, err := ReadCSV(csv)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows: %d", len(rows))
	}
	if rows[0].Symbol != "WIF" || rows[0].Rate.String() != "0.0001" {
		t.Errorf("row0: %+v", rows[0])
	}
	if rows[0].Interval != time.Hour {
		t.Errorf("interval: %v", rows[0].Interval)
	}
}

func TestReadCSV_EpochMillisAccepted(t *testing.T) {
	csv := strings.NewReader(`time,symbol,rate,interval_seconds
1735689600000,WIF,0.0001,3600
`)
	rows, err := ReadCSV(csv)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Time.Year() != 2025 && rows[0].Time.Year() != 2024 {
		t.Errorf("epoch parse: %s", rows[0].Time)
	}
}

func mustReg(t *testing.T) assets.Registry {
	t.Helper()
	r, err := assets.LoadEmbedded()
	if err != nil {
		t.Fatal(err)
	}
	return r
}
