//go:build live

package hyperliquid

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// TestLive_CancelByOID is a one-off helper to clean up an order placed by
// an earlier integration test. Set PERMAFROST_HL_TEST_KEY and
// PERMAFROST_HL_CANCEL_OID + PERMAFROST_HL_CANCEL_COIN to use it.
func TestLive_CancelByOID(t *testing.T) {
	keyHex := os.Getenv("PERMAFROST_HL_TEST_KEY")
	oid := os.Getenv("PERMAFROST_HL_CANCEL_OID")
	coin := os.Getenv("PERMAFROST_HL_CANCEL_COIN")
	if keyHex == "" || oid == "" || coin == "" {
		t.Skip("set PERMAFROST_HL_TEST_KEY + PERMAFROST_HL_CANCEL_OID + PERMAFROST_HL_CANCEL_COIN")
	}
	signer, err := wallet.NewHyperliquidSignerFromHex(keyHex)
	if err != nil {
		t.Fatal(err)
	}
	v, err := New(Config{
		Network: NetworkTestnet,
		Address: signer.Address(),
	}, WithSigner(signer))
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := v.CancelWithSymbol(ctx, types.OrderID(oid), coin); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	t.Logf("cancelled %s on %s", oid, coin)
}
