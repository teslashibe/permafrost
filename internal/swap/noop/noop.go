// Package noop provides a SwapVenue implementation that quotes a 1:1 trade
// at zero gas, "confirms" instantly, and records calls. For tests only.
package noop

import (
	"context"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/internal/types"
)

// Name is the registered identifier.
const Name = "noop"

// SwapVenue is a deterministic stub.
type SwapVenue struct {
	mu     sync.Mutex
	swaps  []types.Quote
}

// New constructs a fresh stub.
func New() *SwapVenue { return &SwapVenue{} }

// Compile-time check.
var _ swap.SwapVenue = (*SwapVenue)(nil)

func (v *SwapVenue) Name() string         { return Name }
func (v *SwapVenue) Chain() types.ChainID { return "noop" }

func (v *SwapVenue) Quote(_ context.Context, req types.QuoteRequest) (types.Quote, error) {
	out := req.Amount
	if req.Mode == types.QuoteExactOut {
		out = req.Amount
	}
	return types.Quote{
		InToken:   req.InToken,
		OutToken:  req.OutToken,
		InAmount:  req.Amount,
		OutAmount: out,
		Route:     "noop",
		ExpiresAt: time.Now().UTC().Add(30 * time.Second),
	}, nil
}

func (v *SwapVenue) Swap(_ context.Context, q types.Quote, _ int) (types.TxHash, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.swaps = append(v.swaps, q)
	return types.TxHash("noop-tx"), nil
}

func (v *SwapVenue) WaitConfirm(_ context.Context, tx types.TxHash) (types.SwapResult, error) {
	return types.SwapResult{
		TxHash:    tx,
		Status:    types.SwapStatusConfirmed,
		BlockTime: time.Now().UTC(),
	}, nil
}

func (v *SwapVenue) Balance(_ context.Context, _ types.Asset) (decimal.Decimal, error) {
	return decimal.Zero, nil
}

// Submitted returns the quotes recorded by Swap, in submission order.
func (v *SwapVenue) Submitted() []types.Quote {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]types.Quote, len(v.swaps))
	copy(out, v.swaps)
	return out
}
