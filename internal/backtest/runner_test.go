package backtest

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"

	// Register noop so we can construct a minimal strategy here without
	// pulling in any private/community strategy package. End-to-end
	// runner tests with real strategies live in those strategies' own
	// test packages (see https://teslashibe.github.io/permafrost/strategies/testing
	// for the pattern).
	_ "github.com/teslashibe/permafrost/strategies/noop"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

func TestRunner_NoTicks(t *testing.T) {
	ctor, err := strategy.Get("noop")
	if err != nil {
		t.Fatalf("noop registry lookup: %v", err)
	}
	s, err := ctor(nil)
	if err != nil {
		t.Fatalf("noop ctor: %v", err)
	}
	r := NewRunner(s, d("1000"), time.Hour, Costs{})
	if _, err := r.Run(context.Background(), nil); err == nil {
		t.Fatal("expected error for empty ticks")
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
