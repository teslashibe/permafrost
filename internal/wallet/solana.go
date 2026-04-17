package wallet

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"

	"github.com/teslashibe/permafrost/internal/types"
)

// SolanaSigner signs payloads with an ed25519 keypair.
type SolanaSigner struct {
	priv ed25519.PrivateKey
	addr string
}

// NewSolanaSignerFromPrivate constructs a SolanaSigner from a 64-byte
// ed25519 secret (the Phantom-style export format: 32 bytes seed + 32 bytes
// pubkey). It also accepts the 32-byte seed alone.
func NewSolanaSignerFromPrivate(secret []byte) (*SolanaSigner, error) {
	switch len(secret) {
	case ed25519.PrivateKeySize: // 64
		priv := ed25519.PrivateKey(append([]byte(nil), secret...))
		pub := priv.Public().(ed25519.PublicKey)
		return &SolanaSigner{priv: priv, addr: base58.Encode(pub)}, nil
	case ed25519.SeedSize: // 32
		priv := ed25519.NewKeyFromSeed(secret)
		pub := priv.Public().(ed25519.PublicKey)
		return &SolanaSigner{priv: priv, addr: base58.Encode(pub)}, nil
	default:
		return nil, fmt.Errorf("solana: secret must be 32 or 64 bytes, got %d", len(secret))
	}
}

// NewSolanaSignerFromBase58 parses a base58-encoded ed25519 secret key.
func NewSolanaSignerFromBase58(b58 string) (*SolanaSigner, error) {
	raw, err := base58.Decode(b58)
	if err != nil {
		return nil, fmt.Errorf("solana: base58 decode: %w", err)
	}
	return NewSolanaSignerFromPrivate(raw)
}

// SecretKey returns the 64-byte ed25519 private key (seed || pubkey).
// Callers MUST treat this as sensitive material.
func (s *SolanaSigner) SecretKey() []byte {
	out := make([]byte, len(s.priv))
	copy(out, s.priv)
	return out
}

func (s *SolanaSigner) Address() string         { return s.addr }
func (s *SolanaSigner) Chain() types.ChainID    { return types.ChainSolana }

func (s *SolanaSigner) Sign(_ context.Context, payload []byte) ([]byte, error) {
	if s.priv == nil {
		return nil, errors.New("solana: signer not initialised")
	}
	return ed25519.Sign(s.priv, payload), nil
}

// Compile-time check.
var _ Signer = (*SolanaSigner)(nil)
