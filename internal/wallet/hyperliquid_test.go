package wallet

import (
	"context"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/internal/types"
)

func TestHyperliquidSigner_FromHex(t *testing.T) {
	// Test vector: a known secp256k1 keypair.
	// priv = 0x1111...1111
	// expected EVM address derived deterministically from secp256k1 pubkey.
	const priv = "0x1111111111111111111111111111111111111111111111111111111111111111"
	s, err := NewHyperliquidSignerFromHex(priv)
	if err != nil {
		t.Fatal(err)
	}
	if s.Chain() != types.ChainHyperliquid {
		t.Errorf("Chain: %q", s.Chain())
	}
	addr := s.Address()
	if !strings.HasPrefix(addr, "0x") || len(addr) != 42 {
		t.Errorf("Address shape: %q", addr)
	}

	// Determinism: identical input produces identical address.
	s2, _ := NewHyperliquidSignerFromHex(priv)
	if s.Address() != s2.Address() {
		t.Errorf("address not deterministic: %s vs %s", s.Address(), s2.Address())
	}
}

func TestHyperliquidSigner_PrivateKeyHex_RoundTrip(t *testing.T) {
	s1, err := GenerateHyperliquidKey()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := NewHyperliquidSignerFromHex(s1.PrivateKeyHex())
	if err != nil {
		t.Fatal(err)
	}
	if s1.Address() != s2.Address() {
		t.Errorf("round-trip address: %s vs %s", s1.Address(), s2.Address())
	}
}

func TestHyperliquidSigner_BadLength(t *testing.T) {
	if _, err := NewHyperliquidSignerFromPrivate(make([]byte, 31)); err == nil {
		t.Fatal("expected error")
	}
}

func TestHyperliquidSigner_RequiresHash(t *testing.T) {
	s, _ := GenerateHyperliquidKey()
	if _, err := s.Sign(context.Background(), []byte("not 32 bytes")); err == nil {
		t.Fatal("expected error for non-32-byte payload")
	}
}

func TestHyperliquidSigner_ProducesValidShape(t *testing.T) {
	s, _ := GenerateHyperliquidKey()
	hash := make([]byte, 32)
	for i := range hash {
		hash[i] = byte(i)
	}
	sig, err := s.Sign(context.Background(), hash)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 65 {
		t.Errorf("sig length: %d want 65", len(sig))
	}
	if sig[64] > 1 {
		t.Errorf("V byte: %d want 0 or 1", sig[64])
	}
}
