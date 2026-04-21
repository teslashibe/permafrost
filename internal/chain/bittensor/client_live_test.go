// Live integration tests against the public Subtensor finney endpoint.
//
// Run with: go test -tags=live -v ./internal/chain/bittensor/...
// Skip with: go test ./internal/chain/bittensor/... (default)
//
// These tests verify that the production-grade chain client actually
// works against a live mainnet RPC. They make read-only calls (no
// extrinsics submitted) so they're safe to run in CI.

//go:build live
// +build live

package bittensor

import (
	"context"
	"testing"
	"time"
)

const liveEndpoint = "wss://entrypoint-finney.opentensor.ai:443"

func TestLive_Chain(t *testing.T) {
	c := NewClient(liveEndpoint)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	chain, err := c.GetChain(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if chain != "Bittensor" {
		t.Errorf("chain: got %q, want %q", chain, "Bittensor")
	}
}

func TestLive_BlockNumber(t *testing.T) {
	c := NewClient(liveEndpoint)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	block, err := c.GetBlockNumber(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if block < 1_000_000 {
		t.Errorf("block too low: %d (expected > 1M)", block)
	}
	t.Logf("finalized block: %d", block)
}

func TestLive_AlphaPriceSN8(t *testing.T) {
	c := NewClient(liveEndpoint)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	price, err := c.CurrentAlphaPrice(ctx, 8)
	if err != nil {
		t.Fatal(err)
	}
	if price.IsZero() {
		t.Fatal("SN8 alpha price is zero — RPC likely returned wrong data")
	}
	t.Logf("SN8 alpha price: %s TAO", price)
}

func TestLive_TAOBalance_Alice(t *testing.T) {
	c := NewClient(liveEndpoint)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Query a known address (Alice). Should not error even if zero balance.
	rao, err := c.GetTAOBalance(ctx, "5GrwvaEF5zXb26Fz9rcQpDWS57CtERHpNehXCPcNoHGKutQY")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Alice TAO balance: %d RAO (%s TAO)", rao, RAOToTAO(rao))
}

func TestLive_SubnetCount(t *testing.T) {
	c := NewClient(liveEndpoint)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	count, err := c.SubnetCount(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if count < 50 {
		t.Errorf("subnet count too low: %d (expected > 50)", count)
	}
	t.Logf("total subnets: %d", count)
}

func TestLive_SimSwap(t *testing.T) {
	c := NewClient(liveEndpoint)
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Simulate buying 1 TAO of SN8 alpha.
	res, err := c.SimSwapTaoForAlpha(ctx, 8, 1_000_000_000)
	if err != nil {
		t.Fatal(err)
	}
	if res.AmountOut == 0 {
		t.Fatal("AmountOut is zero")
	}
	t.Logf("1 TAO → %d alpha-RAO of SN8 (fee: %d)", res.AmountOut, res.Fee)
}
