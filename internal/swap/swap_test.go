package swap_test

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/swap/noop"
	"github.com/teslashibe/permafrost/internal/types"
)

func TestNoopSwapVenue(t *testing.T) {
	v := noop.New()
	ctx := context.Background()

	in := types.Asset{Symbol: "USDC", Chain: types.ChainSolana, Mint: "USDCmint"}
	out := types.Asset{Symbol: "WIF", Chain: types.ChainSolana, Mint: "WIFmint"}

	q, err := v.Quote(ctx, types.QuoteRequest{
		InToken: in,
		OutToken: out,
		Amount:   decimal.NewFromInt(100),
		Mode:     types.QuoteExactIn,
	})
	if err != nil {
		t.Fatalf("Quote: %v", err)
	}
	if !q.InAmount.Equal(decimal.NewFromInt(100)) {
		t.Errorf("InAmount: got %s", q.InAmount)
	}

	tx, err := v.Swap(ctx, q, 50)
	if err != nil {
		t.Fatalf("Swap: %v", err)
	}
	res, err := v.WaitConfirm(ctx, tx)
	if err != nil {
		t.Fatalf("WaitConfirm: %v", err)
	}
	if res.Status != types.SwapStatusConfirmed {
		t.Errorf("Status: got %q", res.Status)
	}
	if len(v.Submitted()) != 1 {
		t.Errorf("Submitted: got %d", len(v.Submitted()))
	}
}
