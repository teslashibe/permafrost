package wallet

import (
	"context"
	"testing"

	"github.com/teslashibe/permafrost/pkg/types"
)

func TestBittensorSigner_RoundTrip(t *testing.T) {
	seed := make([]byte, 32)
	for i := range seed {
		seed[i] = byte(i)
	}

	s, err := NewBittensorSignerFromSeed(seed)
	if err != nil {
		t.Fatal(err)
	}

	if s.Chain() != types.ChainBittensor {
		t.Errorf("chain: got %q, want %q", s.Chain(), types.ChainBittensor)
	}

	addr := s.Address()
	if addr == "" {
		t.Fatal("address is empty")
	}
	t.Logf("address: %s", addr)

	// SS58 addresses for prefix 42 start with '5'.
	if addr[0] != '5' {
		t.Errorf("SS58 prefix 42 address should start with '5', got %q", addr)
	}

	sig, err := s.Sign(context.Background(), []byte("hello bittensor"))
	if err != nil {
		t.Fatal(err)
	}
	if len(sig) != 64 {
		t.Errorf("signature length: got %d, want 64", len(sig))
	}

	// Roundtrip via SecretKey.
	s2, err := NewBittensorSignerFromPrivate(s.SecretKey())
	if err != nil {
		t.Fatal(err)
	}
	if s2.Address() != addr {
		t.Errorf("roundtrip address mismatch: %q vs %q", s2.Address(), addr)
	}
}

func TestBittensorSigner_InvalidSeed(t *testing.T) {
	_, err := NewBittensorSignerFromSeed([]byte{1, 2, 3})
	if err == nil {
		t.Fatal("expected error for short seed")
	}
}

func TestGenerateBittensorKey(t *testing.T) {
	s, err := GenerateBittensorKey()
	if err != nil {
		t.Fatal(err)
	}
	if s.Address() == "" {
		t.Fatal("generated key has empty address")
	}
	if s.Chain() != types.ChainBittensor {
		t.Errorf("chain: got %q", s.Chain())
	}
}
