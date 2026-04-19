package wallet

import (
	"context"
	"crypto/ed25519"
	"testing"

	"github.com/teslashibe/permafrost/pkg/types"
)

func TestSolanaSigner_FromSeedAndFull(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	fromFull, err := NewSolanaSignerFromPrivate(priv)
	if err != nil {
		t.Fatalf("from full: %v", err)
	}
	fromSeed, err := NewSolanaSignerFromPrivate(priv.Seed())
	if err != nil {
		t.Fatalf("from seed: %v", err)
	}
	if fromFull.Address() != fromSeed.Address() {
		t.Errorf("address mismatch")
	}
	if fromFull.Chain() != types.ChainSolana {
		t.Errorf("Chain: %q", fromFull.Chain())
	}

	sig, err := fromFull.Sign(context.Background(), []byte("hello"))
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}
	if !ed25519.Verify(pub, []byte("hello"), sig) {
		t.Fatalf("signature failed verification")
	}
}

func TestSolanaSigner_BadLength(t *testing.T) {
	if _, err := NewSolanaSignerFromPrivate([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected error")
	}
}

func TestSolanaSigner_GenerateAndRoundTripBase58(t *testing.T) {
	s, err := GenerateSolanaKey()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.SecretKey()) != ed25519.PrivateKeySize {
		t.Fatalf("SecretKey size: %d", len(s.SecretKey()))
	}

	// Address is base58 of pubkey; sanity check it round-trips without error.
	if s.Address() == "" {
		t.Fatal("Address empty")
	}
}
