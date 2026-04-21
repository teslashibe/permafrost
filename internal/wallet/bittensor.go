// Package wallet — Bittensor signer.
//
// Uses sr25519 (Schnorrkel/Ristretto) which is Bittensor's native scheme.
// Implemented via vedhavyas/go-subkey/v2 — the canonical Go port of
// Substrate's subkey CLI. SS58 prefix 42 (Substrate generic / Bittensor).
//
// The signer satisfies both:
//   - permafrost wallet.Signer interface (for keystore registration)
//   - gsrpc signature.KeyringPair compatibility (for extrinsic signing)
package wallet

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/vedhavyas/go-subkey/v2"
	subscale "github.com/vedhavyas/go-subkey/v2/sr25519"

	"github.com/teslashibe/permafrost/pkg/types"
)

// SS58 network prefix for Bittensor (Substrate generic format).
const bittensorSS58Prefix uint16 = 42

// BittensorSigner signs payloads with an sr25519 keypair.
//
// Bittensor's native key scheme is sr25519 (matches polkadot.js, subkey,
// btcli). Extrinsics signed by this signer are byte-for-byte compatible
// with the rest of the Substrate ecosystem.
type BittensorSigner struct {
	kp   subkey.KeyPair
	addr string
}

// NewBittensorSignerFromSeed constructs a signer from a 32-byte mini-secret seed.
func NewBittensorSignerFromSeed(seed []byte) (*BittensorSigner, error) {
	if len(seed) != 32 {
		return nil, fmt.Errorf("bittensor: seed must be 32 bytes, got %d", len(seed))
	}
	scheme := subscale.Scheme{}
	kp, err := scheme.FromSeed(seed)
	if err != nil {
		return nil, fmt.Errorf("bittensor: derive keypair: %w", err)
	}
	return &BittensorSigner{
		kp:   kp,
		addr: kp.SS58Address(bittensorSS58Prefix),
	}, nil
}

// NewBittensorSignerFromPrivate constructs a signer from a stored secret.
// Accepts the 32-byte mini-secret seed (preferred) or 64-byte expanded
// secret-key form.
func NewBittensorSignerFromPrivate(secret []byte) (*BittensorSigner, error) {
	scheme := subscale.Scheme{}
	kp, err := scheme.FromSeed(secret)
	if err != nil {
		return nil, fmt.Errorf("bittensor: derive keypair: %w", err)
	}
	return &BittensorSigner{
		kp:   kp,
		addr: kp.SS58Address(bittensorSS58Prefix),
	}, nil
}

// NewBittensorSignerFromPhrase constructs a signer from a BIP-39 mnemonic
// phrase (the polkadot.js / btcli format). Empty password = standard.
func NewBittensorSignerFromPhrase(phrase, password string) (*BittensorSigner, error) {
	scheme := subscale.Scheme{}
	kp, err := scheme.FromPhrase(phrase, password)
	if err != nil {
		return nil, fmt.Errorf("bittensor: from phrase: %w", err)
	}
	return &BittensorSigner{
		kp:   kp,
		addr: kp.SS58Address(bittensorSS58Prefix),
	}, nil
}

// SecretKey returns a copy of the 32-byte seed (mini-secret).
func (s *BittensorSigner) SecretKey() []byte {
	out := make([]byte, len(s.kp.Seed()))
	copy(out, s.kp.Seed())
	return out
}

// PublicKey returns a copy of the 32-byte sr25519 public key. Used by
// gsrpc when constructing storage keys for System.Account etc.
func (s *BittensorSigner) PublicKey() []byte {
	pub := s.kp.Public()
	out := make([]byte, len(pub))
	copy(out, pub)
	return out
}

// KeyPair returns the underlying subkey keypair for direct use with gsrpc.
func (s *BittensorSigner) KeyPair() subkey.KeyPair { return s.kp }

func (s *BittensorSigner) Address() string      { return s.addr }
func (s *BittensorSigner) Chain() types.ChainID { return types.ChainBittensor }

// Sign produces a 64-byte sr25519 signature over payload. The sr25519
// signing scheme uses Substrate's "substrate" signing context per
// Schnorrkel spec.
func (s *BittensorSigner) Sign(_ context.Context, payload []byte) ([]byte, error) {
	if s.kp == nil {
		return nil, errors.New("bittensor: signer not initialised")
	}
	return s.kp.Sign(payload)
}

var _ Signer = (*BittensorSigner)(nil)

// GenerateBittensorKey creates a fresh sr25519 keypair from crypto/rand.
func GenerateBittensorKey() (*BittensorSigner, error) {
	seed := make([]byte, 32)
	if _, err := rand.Read(seed); err != nil {
		return nil, fmt.Errorf("bittensor: generate: %w", err)
	}
	return NewBittensorSignerFromSeed(seed)
}

// ─── SS58 helper kept for tests ─────────────────────────────────────────────

// ss58Encode is retained as a thin wrapper over the canonical subkey
// implementation so existing tests against well-known addresses continue
// to validate cross-tool compatibility.
func ss58Encode(pub []byte, prefix byte) (string, error) {
	if len(pub) != 32 {
		return "", fmt.Errorf("ss58: expected 32-byte pubkey, got %d", len(pub))
	}
	return subkey.SS58Encode(pub, uint16(prefix)), nil
}
