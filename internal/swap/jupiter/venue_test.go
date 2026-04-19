package jupiter

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
	walletnoop "github.com/teslashibe/permafrost/internal/wallet/noop"
)

func TestNew_Validation(t *testing.T) {
	if _, err := New(Config{RPCURL: "x"}, nil); err == nil {
		t.Error("nil signer should error")
	}
	hl, _ := wallet.NewHyperliquidSignerFromHex("0x1111111111111111111111111111111111111111111111111111111111111111")
	if _, err := New(Config{RPCURL: "x"}, hl); err == nil {
		t.Error("non-solana signer should error")
	}
	sol, _ := wallet.GenerateSolanaKey()
	if _, err := New(Config{Mode: SubmitJito}, sol); err == nil {
		t.Error("missing RPCURL should error")
	}
	if _, err := New(Config{RPCURL: "x", Mode: SubmitJito}, sol); err == nil {
		t.Error("missing JitoBundleURL with jito mode should error")
	}
	if _, err := New(Config{RPCURL: "x", Mode: SubmitRPC}, sol); err != nil {
		t.Errorf("rpc mode without Jito URL should be OK, got %v", err)
	}
}

func TestScaleUnscale_RoundTrip(t *testing.T) {
	cases := []struct {
		amount   string
		decimals int32
		want     uint64
	}{
		{"1.5", 6, 1_500_000},
		{"0.000001", 6, 1},
		{"100", 9, 100_000_000_000},
		{"0", 6, 0},
	}
	for _, c := range cases {
		got := scaleAmount(decimal.RequireFromString(c.amount), c.decimals)
		if got != c.want {
			t.Errorf("scale(%s,%d): got %d want %d", c.amount, c.decimals, got, c.want)
		}
		back := unscaleAmount(got, c.decimals)
		if !back.Equal(decimal.RequireFromString(c.amount)) {
			t.Errorf("unscale: got %s want %s", back, c.amount)
		}
	}
}

func TestVenue_Chain(t *testing.T) {
	sol, _ := wallet.GenerateSolanaKey()
	v, err := New(Config{RPCURL: "http://x", Mode: SubmitRPC}, sol)
	if err != nil {
		t.Fatal(err)
	}
	if v.Name() != VenueName {
		t.Errorf("Name: %q", v.Name())
	}
	if v.Chain() != types.ChainSolana {
		t.Errorf("Chain: %q", v.Chain())
	}
}

func TestVenue_Swap_RejectsExpiredQuote(t *testing.T) {
	sol, _ := wallet.GenerateSolanaKey()
	v, err := New(Config{RPCURL: "http://x", Mode: SubmitRPC}, sol)
	if err != nil {
		t.Fatal(err)
	}
	q := types.Quote{ExpiresAt: time.Now().Add(-time.Minute)}
	if _, err := v.Swap(context.Background(), q, 50); err == nil {
		t.Fatal("expected expired-quote error")
	}
}

// Sanity: noop signer is on a different chain, so Venue rejects it. Kept as
// a regression guard against accidentally allowing wrong-chain signers.
func TestVenue_RejectsWrongChainSigner(t *testing.T) {
	if _, err := New(Config{RPCURL: "x"}, walletnoop.NewSigner(types.ChainHyperliquid)); err == nil {
		t.Fatal("expected wrong-chain rejection")
	}
}
