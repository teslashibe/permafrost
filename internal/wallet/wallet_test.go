package wallet_test

import (
	"context"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/internal/types"
	"github.com/teslashibe/permafrost/internal/wallet/noop"
)

func TestNoopSigner(t *testing.T) {
	s := noop.NewSigner(types.ChainSolana)
	if s.Chain() != types.ChainSolana {
		t.Errorf("Chain: got %q", s.Chain())
	}
	if !strings.HasPrefix(s.Address(), "noop_") {
		t.Errorf("Address: got %q", s.Address())
	}

	sig1, err := s.Sign(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	sig2, _ := s.Sign(context.Background(), []byte("hello"))
	if string(sig1) != string(sig2) {
		t.Errorf("Sign should be deterministic")
	}
	sig3, _ := s.Sign(context.Background(), []byte("world"))
	if string(sig1) == string(sig3) {
		t.Errorf("Sign should depend on payload")
	}
}

func TestNoopKeystore(t *testing.T) {
	ks := noop.NewKeystore(types.ChainSolana, types.ChainHyperliquid)

	if _, err := ks.Signer(types.ChainSolana); err != nil {
		t.Errorf("Signer(solana): %v", err)
	}
	if _, err := ks.Signer("ethereum"); err == nil {
		t.Errorf("Signer(unknown): expected error")
	}

	chains := ks.Chains()
	if len(chains) != 2 {
		t.Errorf("Chains: got %d want 2", len(chains))
	}
}
