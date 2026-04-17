package vault

import (
	"errors"
	"sort"

	"github.com/shopspring/decimal"
)

// AgentAllocation is one agent's share of vault capital.
type AgentAllocation struct {
	AgentID string
	Weight  decimal.Decimal // 0..1
	Amount  decimal.Decimal // resolved capital
}

// AllocatorPolicy decides how to split AvailableCapital across agents.
type AllocatorPolicy interface {
	Allocate(available decimal.Decimal, weights map[string]decimal.Decimal) ([]AgentAllocation, error)
}

// ProportionalPolicy splits available capital pro-rata to weights. Weights
// are normalised so they sum to 1; an agent with weight 0 gets nothing.
type ProportionalPolicy struct{}

// Allocate implements AllocatorPolicy.
func (ProportionalPolicy) Allocate(available decimal.Decimal, weights map[string]decimal.Decimal) ([]AgentAllocation, error) {
	if available.IsNegative() {
		return nil, errors.New("vault: available capital is negative")
	}
	total := decimal.Zero
	for _, w := range weights {
		if w.IsNegative() {
			return nil, errors.New("vault: negative weight")
		}
		total = total.Add(w)
	}
	out := make([]AgentAllocation, 0, len(weights))
	for id, w := range weights {
		out = append(out, AgentAllocation{
			AgentID: id,
			Weight:  w,
			Amount:  decimal.Zero, // filled below
		})
	}
	// sort for determinism
	sort.Slice(out, func(i, j int) bool { return out[i].AgentID < out[j].AgentID })

	if total.IsZero() || available.IsZero() {
		return out, nil
	}
	for i := range out {
		out[i].Amount = available.Mul(out[i].Weight).Div(total)
	}
	return out, nil
}
