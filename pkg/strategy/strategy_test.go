package strategy_test

import (
	"context"
	"testing"

	"github.com/teslashibe/permafrost/pkg/strategy"
	_ "github.com/teslashibe/permafrost/strategies/noop"
	"github.com/teslashibe/permafrost/pkg/types"
)

func TestRegistry(t *testing.T) {
	names := strategy.List()
	found := false
	for _, n := range names {
		if n == "noop" {
			found = true
		}
	}
	if !found {
		t.Fatalf("noop strategy not in registry: %v", names)
	}

	ctor, err := strategy.Get("noop")
	if err != nil {
		t.Fatalf("Get noop: %v", err)
	}
	s, err := ctor(nil)
	if err != nil {
		t.Fatalf("noop ctor: %v", err)
	}
	if s.Name() != "noop" {
		t.Errorf("Name: got %q", s.Name())
	}

	if _, err := strategy.Get("does-not-exist"); err == nil {
		t.Errorf("Get unknown should error")
	}
}

func TestNoopDecide(t *testing.T) {
	ctor, _ := strategy.Get("noop")
	s, _ := ctor(nil)
	dec, err := s.Decide(context.Background(), strategy.DecisionInput{AgentID: "x"})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if dec.HasIntents() {
		t.Errorf("noop should produce no intents, got %+v", dec)
	}
}

func TestDecisionHasIntents(t *testing.T) {
	cases := []struct {
		name string
		dec  strategy.Decision
		want bool
	}{
		{"empty", strategy.Decision{}, false},
		{"orders", strategy.Decision{Orders: []types.OrderIntent{{}}}, true},
		{"swaps", strategy.Decision{Swaps: []types.SwapIntent{{}}}, true},
		{"cancels", strategy.Decision{Cancels: []types.OrderID{"x"}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.dec.HasIntents(); got != tc.want {
				t.Errorf("HasIntents: got %v want %v", got, tc.want)
			}
		})
	}
}
