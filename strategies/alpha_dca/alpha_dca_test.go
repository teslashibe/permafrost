package alpha_dca

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/strategy"
	"github.com/teslashibe/permafrost/pkg/types"
)

func TestAlphaDCA_Registered(t *testing.T) {
	_, err := strategy.Get(Name)
	if err != nil {
		t.Fatalf("strategy %q not registered: %v", Name, err)
	}
}

func TestAlphaDCA_Defaults(t *testing.T) {
	s, err := New(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if s.Name() != Name {
		t.Errorf("name: got %q", s.Name())
	}
}

func TestAlphaDCA_EmitsSwaps(t *testing.T) {
	s, err := New(map[string]any{
		"subnets":        []any{8, 3},
		"tao_per_buy":    1.0,
		"interval_ticks": 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Warmup(context.Background(), strategy.WarmupInput{}); err != nil {
		t.Fatal(err)
	}

	dec, err := s.Decide(context.Background(), strategy.DecisionInput{
		Now: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(dec.Swaps) != 2 {
		t.Fatalf("expected 2 swaps, got %d", len(dec.Swaps))
	}

	for _, sw := range dec.Swaps {
		if sw.Chain != types.ChainBittensor {
			t.Errorf("swap chain: got %q, want bittensor", sw.Chain)
		}
		if sw.InToken.Mint != "TAO" {
			t.Errorf("in token: got %q, want TAO", sw.InToken.Mint)
		}
		if !sw.InAmount.Equal(decimal.NewFromFloat(1.0)) {
			t.Errorf("in amount: got %s, want 1", sw.InAmount)
		}
	}
}

func TestAlphaDCA_Cooldown(t *testing.T) {
	s, err := New(map[string]any{
		"subnets":        []any{8},
		"tao_per_buy":    1.0,
		"interval_ticks": 3,
	})
	if err != nil {
		t.Fatal(err)
	}

	input := strategy.DecisionInput{Now: time.Now()}

	// Tick 1: should wait (tickCount=1, 1%3 != 0)
	d, _ := s.Decide(context.Background(), input)
	if len(d.Swaps) != 0 {
		t.Errorf("tick 1: expected 0 swaps (waiting), got %d", len(d.Swaps))
	}

	// Tick 2: should wait
	d, _ = s.Decide(context.Background(), input)
	if len(d.Swaps) != 0 {
		t.Errorf("tick 2: expected 0 swaps (waiting), got %d", len(d.Swaps))
	}

	// Tick 3: should buy (tickCount=3, 3%3 == 0)
	d, _ = s.Decide(context.Background(), input)
	if len(d.Swaps) != 1 {
		t.Fatalf("tick 3: expected 1 swap, got %d", len(d.Swaps))
	}
}
