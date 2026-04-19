package dca_buy

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

func d(s string) decimal.Decimal { return decimal.RequireFromString(s) }

// TestNew_Defaults: an empty cfg map produces a working strategy with
// defaults applied.
func TestNew_Defaults(t *testing.T) {
	s, err := New(map[string]any{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	st := s.(*Strategy)
	if st.cfg.Asset != "SOL" {
		t.Errorf("default asset: got %q want SOL", st.cfg.Asset)
	}
	if !st.cfg.USDCPerTick.Equal(decimal.NewFromInt(50)) {
		t.Errorf("default USDCPerTick: got %s want 50", st.cfg.USDCPerTick)
	}
}

// TestNew_RejectsBadConfig: usdc_per_tick=0 fails validate.
func TestNew_RejectsBadConfig(t *testing.T) {
	if _, err := New(map[string]any{"usdc_per_tick": "-1"}); err == nil {
		t.Error("expected validation error for negative usdc_per_tick")
	}
}

// TestDecide_EmitsSwapOnFirstTick: with no prior buy and a configured
// chain, the first tick produces exactly one SwapIntent: USDC → asset
// on the configured chain, with the configured amount + slippage.
func TestDecide_EmitsSwapOnFirstTick(t *testing.T) {
	s, _ := NewFromTypedConfig(Config{
		Asset:        "WIF",
		Chain:        types.ChainSolana,
		USDCPerTick:  d("100"),
		IntervalSecs: 3600,
		SlippageBps:  25,
	})
	dec, err := s.Decide(context.Background(), strategy.DecisionInput{
		Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Swaps) != 1 {
		t.Fatalf("expected 1 swap, got %d", len(dec.Swaps))
	}
	sw := dec.Swaps[0]
	if sw.InToken.Symbol != "USDC" {
		t.Errorf("InToken: got %q want USDC", sw.InToken.Symbol)
	}
	if sw.OutToken.Symbol != "WIF" {
		t.Errorf("OutToken: got %q want WIF", sw.OutToken.Symbol)
	}
	if sw.Chain != types.ChainSolana {
		t.Errorf("Chain: got %q want solana", sw.Chain)
	}
	if !sw.InAmount.Equal(d("100")) {
		t.Errorf("InAmount: got %s want 100", sw.InAmount)
	}
	if sw.SlippageBps != 25 {
		t.Errorf("SlippageBps: got %d want 25", sw.SlippageBps)
	}
}

// TestDecide_RespectsCooldown: a second tick within IntervalSecs is a
// no-op with a "cooldown" note.
func TestDecide_RespectsCooldown(t *testing.T) {
	s, _ := NewFromTypedConfig(Config{
		USDCPerTick:  d("50"),
		IntervalSecs: 3600, // 1h cooldown
	})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if _, err := s.Decide(context.Background(), strategy.DecisionInput{Now: now}); err != nil {
		t.Fatal(err)
	}

	// 30 minutes later — should be inside cooldown.
	dec, err := s.Decide(context.Background(), strategy.DecisionInput{Now: now.Add(30 * time.Minute)})
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Swaps) != 0 {
		t.Errorf("expected no swap inside cooldown, got %d", len(dec.Swaps))
	}
	if !strings.Contains(dec.Notes, "cooldown") {
		t.Errorf("expected 'cooldown' in notes; got %q", dec.Notes)
	}
}

// TestDecide_BuysAfterCooldown: a tick past IntervalSecs emits a swap.
func TestDecide_BuysAfterCooldown(t *testing.T) {
	s, _ := NewFromTypedConfig(Config{USDCPerTick: d("50"), IntervalSecs: 3600})
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	_, _ = s.Decide(context.Background(), strategy.DecisionInput{Now: now})

	dec, _ := s.Decide(context.Background(), strategy.DecisionInput{Now: now.Add(2 * time.Hour)})
	if len(dec.Swaps) != 1 {
		t.Errorf("expected swap after cooldown, got %d", len(dec.Swaps))
	}
}

// TestDecide_NoUSDCMappingForChain: an unsupported chain results in a
// note + no swap (rather than a panic or hard error).
func TestDecide_NoUSDCMappingForChain(t *testing.T) {
	s, _ := NewFromTypedConfig(Config{
		Asset:       "X",
		Chain:       types.ChainID("imaginary"),
		USDCPerTick: d("50"),
	})
	dec, err := s.Decide(context.Background(), strategy.DecisionInput{
		Now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Swaps) != 0 {
		t.Errorf("expected no swap on unsupported chain, got %d", len(dec.Swaps))
	}
	if !strings.Contains(dec.Notes, "no USDC mapping") {
		t.Errorf("expected note about missing USDC mapping; got %q", dec.Notes)
	}
}

// TestParseConfig_TypeErrors: each typed key produces a clear error
// when given the wrong type.
func TestParseConfig_TypeErrors(t *testing.T) {
	cases := []map[string]any{
		{"asset": 42},                       // not a string
		{"chain": true},                     // not a string
		{"usdc_per_tick": []int{1}},         // not a number-ish
		{"interval_secs": "abc"},            // not a number-ish
		{"slippage_bps": map[string]any{}},  // not a number-ish
	}
	for i, c := range cases {
		if _, err := parseConfig(c); err == nil {
			t.Errorf("case %d (%v): expected error", i, c)
		}
	}
}
