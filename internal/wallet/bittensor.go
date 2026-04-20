package wallet

import (
	"context"
	"crypto/ed25519"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"
	"golang.org/x/crypto/blake2b"

	"github.com/teslashibe/permafrost/pkg/types"
)

// SS58 network prefix for Bittensor.
const bittensorSS58Prefix byte = 42

// BittensorSigner signs payloads with an ed25519 keypair for the Bittensor
// network. Bittensor supports both sr25519 and ed25519; we use ed25519
// because Go has native support, keeping the dependency footprint minimal.
//
// The address is SS58-encoded with network prefix 42 (Substrate generic /
// Bittensor default).
type BittensorSigner struct {
	priv ed25519.PrivateKey
	addr string // SS58-encoded
}

// NewBittensorSignerFromSeed constructs a signer from a 32-byte ed25519 seed.
func NewBittensorSignerFromSeed(seed []byte) (*BittensorSigner, error) {
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("bittensor: seed must be 32 bytes, got %d", len(seed))
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)
	addr, err := ss58Encode(pub, bittensorSS58Prefix)
	if err != nil {
		return nil, fmt.Errorf("bittensor: ss58 encode: %w", err)
	}
	return &BittensorSigner{priv: priv, addr: addr}, nil
}

// NewBittensorSignerFromPrivate constructs a signer from a 64-byte ed25519
// private key (seed || pubkey) or a 32-byte seed.
func NewBittensorSignerFromPrivate(secret []byte) (*BittensorSigner, error) {
	switch len(secret) {
	case ed25519.PrivateKeySize: // 64
		priv := ed25519.PrivateKey(append([]byte(nil), secret...))
		pub := priv.Public().(ed25519.PublicKey)
		addr, err := ss58Encode(pub, bittensorSS58Prefix)
		if err != nil {
			return nil, fmt.Errorf("bittensor: ss58 encode: %w", err)
		}
		return &BittensorSigner{priv: priv, addr: addr}, nil
	case ed25519.SeedSize: // 32
		return NewBittensorSignerFromSeed(secret)
	default:
		return nil, fmt.Errorf("bittensor: secret must be 32 or 64 bytes, got %d", len(secret))
	}
}

// SecretKey returns a copy of the 64-byte ed25519 private key.
func (s *BittensorSigner) SecretKey() []byte {
	out := make([]byte, len(s.priv))
	copy(out, s.priv)
	return out
}

func (s *BittensorSigner) Address() string      { return s.addr }
func (s *BittensorSigner) Chain() types.ChainID { return types.ChainBittensor }

func (s *BittensorSigner) Sign(_ context.Context, payload []byte) ([]byte, error) {
	if s.priv == nil {
		return nil, errors.New("bittensor: signer not initialised")
	}
	return ed25519.Sign(s.priv, payload), nil
}

var _ Signer = (*BittensorSigner)(nil)

// ss58Encode encodes a 32-byte public key with a single-byte network prefix
// into an SS58 address. Covers prefix range 0..63.
func ss58Encode(pub ed25519.PublicKey, prefix byte) (string, error) {
	if len(pub) != 32 {
		return "", fmt.Errorf("ss58: expected 32-byte pubkey, got %d", len(pub))
	}
	payload := make([]byte, 0, 35)
	payload = append(payload, prefix)
	payload = append(payload, pub...)
	hash := ss58Checksum(payload)
	payload = append(payload, hash[0], hash[1])
	return base58.Encode(payload), nil
}

// ss58Checksum computes the 2-byte SS58 checksum: first two bytes of
// blake2b-512("SS58PRE" || data).
func ss58Checksum(data []byte) [2]byte {
	pre := []byte("SS58PRE")
	h, _ := blake2b.New512(nil)
	h.Write(pre)
	h.Write(data)
	sum := h.Sum(nil)
	return [2]byte{sum[0], sum[1]}
}

// GenerateBittensorKey creates a fresh ed25519 keypair for Bittensor.
func GenerateBittensorKey() (*BittensorSigner, error) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, err
	}
	return NewBittensorSignerFromPrivate(priv)
}
