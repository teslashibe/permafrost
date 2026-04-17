package vault

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestProportional_BasicSplit(t *testing.T) {
	p := ProportionalPolicy{}
	allocs, err := p.Allocate(d("1000"), map[string]decimal.Decimal{
		"a": d("1"), "b": d("1"), "c": d("2"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(allocs) != 3 {
		t.Fatalf("got %d allocations", len(allocs))
	}
	got := map[string]decimal.Decimal{}
	for _, a := range allocs {
		got[a.AgentID] = a.Amount
	}
	want := map[string]string{"a": "250", "b": "250", "c": "500"}
	for k, v := range want {
		if !got[k].Equal(d(v)) {
			t.Errorf("%s: got %s want %s", k, got[k], v)
		}
	}
}

func TestProportional_ZeroAvailable(t *testing.T) {
	p := ProportionalPolicy{}
	allocs, err := p.Allocate(d("0"), map[string]decimal.Decimal{"a": d("1")})
	if err != nil {
		t.Fatal(err)
	}
	if !allocs[0].Amount.IsZero() {
		t.Errorf("expected zero amount, got %s", allocs[0].Amount)
	}
}

func TestProportional_RejectsNegative(t *testing.T) {
	p := ProportionalPolicy{}
	if _, err := p.Allocate(d("-1"), map[string]decimal.Decimal{"a": d("1")}); err == nil {
		t.Error("expected error for negative available")
	}
	if _, err := p.Allocate(d("1"), map[string]decimal.Decimal{"a": d("-1")}); err == nil {
		t.Error("expected error for negative weight")
	}
}

func TestProportional_DeterministicOrder(t *testing.T) {
	p := ProportionalPolicy{}
	allocs, _ := p.Allocate(d("100"), map[string]decimal.Decimal{
		"zeta": d("1"), "alpha": d("1"), "mu": d("1"),
	})
	for i, want := range []string{"alpha", "mu", "zeta"} {
		if allocs[i].AgentID != want {
			t.Errorf("position %d: got %s want %s", i, allocs[i].AgentID, want)
		}
	}
}
