package market_maker_basic

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/inference"
	"github.com/teslashibe/permafrost/pkg/inference/mock"
	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// snapAt builds a single-symbol MarketSnapshot at the given mark.
func snapAt(symbol string, mark decimal.Decimal) types.MarketSnapshot {
	return types.MarketSnapshot{
		Symbols: map[string]types.SymbolSnap{
			symbol: {
				Funding: types.FundingRate{
					Symbol:    symbol,
					MarkPrice: mark,
					Interval:  time.Hour,
				},
			},
		},
	}
}

// TestNew_RejectsEmptySymbol: symbol is required.
func TestNew_RejectsEmptySymbol(t *testing.T) {
	_, err := New(map[string]any{"order_size": "10"})
	if err == nil {
		t.Fatal("expected error when symbol is unset")
	}
}

// TestNew_RejectsZeroSize: order_size must be positive.
func TestNew_RejectsZeroSize(t *testing.T) {
	_, err := New(map[string]any{"symbol": "WIF"})
	if err == nil {
		t.Fatal("expected error when order_size is zero")
	}
}

// TestDecide_QuotesAroundMid: with a clean snapshot and no veto, the
// strategy emits exactly two limit orders bracketing the mid by
// SpreadBps on each side.
func TestDecide_QuotesAroundMid(t *testing.T) {
	s, err := NewFromTypedConfig(Config{
		Symbol:    "WIF",
		SpreadBps: 50, // 0.5% each side
		OrderSize: d("100"),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := s.Decide(context.Background(), strategy.DecisionInput{
		Now:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Market: snapAt("WIF", d("1.00")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Orders) != 2 {
		t.Fatalf("expected 2 orders (bid + ask), got %d", len(dec.Orders))
	}

	// Identify which is which by side.
	var bid, ask types.OrderIntent
	for _, o := range dec.Orders {
		if o.Side == types.SideBuy {
			bid = o
		} else {
			ask = o
		}
	}
	// Half-spread = 50bps = 0.005
	wantBid := d("0.995")
	wantAsk := d("1.005")
	if !bid.Price.Equal(wantBid) {
		t.Errorf("bid price: got %s want %s", bid.Price, wantBid)
	}
	if !ask.Price.Equal(wantAsk) {
		t.Errorf("ask price: got %s want %s", ask.Price, wantAsk)
	}
	if !bid.Size.Equal(d("100")) || !ask.Size.Equal(d("100")) {
		t.Errorf("sizes should be %s; got bid=%s ask=%s", d("100"), bid.Size, ask.Size)
	}
	if bid.Type != types.OrderTypeLimit || ask.Type != types.OrderTypeLimit {
		t.Errorf("orders should be limit; got %v / %v", bid.Type, ask.Type)
	}
}

// TestDecide_NoMarketDataIsNoop: missing snapshot for the configured
// symbol should produce a clean note + no orders.
func TestDecide_NoMarketDataIsNoop(t *testing.T) {
	s, _ := NewFromTypedConfig(Config{Symbol: "WIF", OrderSize: d("10")}, nil)
	dec, err := s.Decide(context.Background(), strategy.DecisionInput{
		Now:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Market: types.MarketSnapshot{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Orders) != 0 {
		t.Errorf("expected no orders without market data, got %d", len(dec.Orders))
	}
	if !strings.Contains(dec.Notes, "no market data") {
		t.Errorf("expected note about missing data; got %q", dec.Notes)
	}
}

// TestDecide_RefreshTicksGate: with RefreshTicks=3, ticks 1 and 2
// should be no-ops; tick 3 emits orders.
func TestDecide_RefreshTicksGate(t *testing.T) {
	s, _ := NewFromTypedConfig(Config{
		Symbol:       "WIF",
		OrderSize:    d("10"),
		RefreshTicks: 3,
	}, nil)
	in := strategy.DecisionInput{
		Now:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Market: snapAt("WIF", d("1.00")),
	}
	for i := 1; i <= 3; i++ {
		dec, _ := s.Decide(context.Background(), in)
		if i < 3 && len(dec.Orders) != 0 {
			t.Errorf("tick %d: expected no orders before refresh; got %d", i, len(dec.Orders))
		}
		if i == 3 && len(dec.Orders) != 2 {
			t.Errorf("tick %d: expected 2 orders on refresh; got %d", i, len(dec.Orders))
		}
	}
}

// TestWarmup_RejectsVetoWithoutInference: UseLLMVeto+nil inference
// fails Warmup with a clear error.
func TestWarmup_RejectsVetoWithoutInference(t *testing.T) {
	s, _ := NewFromTypedConfig(Config{
		Symbol:     "WIF",
		OrderSize:  d("10"),
		UseLLMVeto: true,
	}, nil) // explicit nil inference
	err := s.Warmup(context.Background(), strategy.WarmupInput{})
	if err == nil {
		t.Fatal("expected Warmup to fail without inference provider")
	}
	if !strings.Contains(err.Error(), "use_llm_veto") {
		t.Errorf("error should mention use_llm_veto; got %q", err.Error())
	}
}

// TestDecide_LLMVetoBlocksRefresh: with a mock inference that returns
// veto=true, Decide emits no orders.
func TestDecide_LLMVetoBlocksRefresh(t *testing.T) {
	mockInf := mock.New(mock.WithResponse(inference.Response{
		Content: `{"veto":true,"reason":"funding flip ahead"}`,
	}))
	s, _ := NewFromTypedConfig(Config{
		Symbol:     "WIF",
		OrderSize:  d("10"),
		UseLLMVeto: true,
		VetoModel:  "stub",
	}, mockInf)
	dec, err := s.Decide(context.Background(), strategy.DecisionInput{
		Now:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Market: snapAt("WIF", d("1.00")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Orders) != 0 {
		t.Errorf("vetoed cycle should emit no orders, got %d", len(dec.Orders))
	}
	if !strings.Contains(dec.Notes, "vetoed") {
		t.Errorf("notes should explain veto; got %q", dec.Notes)
	}
}

// TestDecide_LLMVetoFalseAllowsQuotes: with veto=false the strategy
// quotes normally.
func TestDecide_LLMVetoFalseAllowsQuotes(t *testing.T) {
	mockInf := mock.New(mock.WithResponse(inference.Response{
		Content: `{"veto":false,"reason":"steady"}`,
	}))
	s, _ := NewFromTypedConfig(Config{
		Symbol:     "WIF",
		OrderSize:  d("10"),
		UseLLMVeto: true,
		VetoModel:  "stub",
	}, mockInf)
	dec, _ := s.Decide(context.Background(), strategy.DecisionInput{
		Now:    time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Market: snapAt("WIF", d("1.00")),
	})
	if len(dec.Orders) != 2 {
		t.Errorf("expected 2 orders when veto=false; got %d", len(dec.Orders))
	}
}

// _ guards against the inference package import being elided.
var _ inference.Provider = (*mock.Provider)(nil)
