package wallet

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/decred/dcrd/dcrec/secp256k1/v4"
	"github.com/decred/dcrd/dcrec/secp256k1/v4/ecdsa"
	"golang.org/x/crypto/sha3"

	"github.com/teslashibe/permafrost/pkg/types"
)

// HyperliquidSigner signs payloads with a secp256k1 keypair (EVM-style).
//
// The address is derived per Ethereum convention: keccak256(uncompressed
// pubkey[1:])[12:] — the rightmost 20 bytes of the keccak hash of the
// 64-byte uncompressed public key (without the 0x04 prefix), rendered as
// 0x-prefixed lowercase hex.
type HyperliquidSigner struct {
	priv *secp256k1.PrivateKey
	addr string
}

// NewHyperliquidSignerFromHex parses a hex-encoded 32-byte private key.
// The leading "0x" is optional. Whitespace is trimmed.
func NewHyperliquidSignerFromHex(s string) (*HyperliquidSigner, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	raw, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: hex decode: %w", err)
	}
	return NewHyperliquidSignerFromPrivate(raw)
}

// NewHyperliquidSignerFromPrivate constructs a signer from a 32-byte private
// key.
func NewHyperliquidSignerFromPrivate(raw []byte) (*HyperliquidSigner, error) {
	if len(raw) != 32 {
		return nil, fmt.Errorf("hyperliquid: private key must be 32 bytes, got %d", len(raw))
	}
	priv := secp256k1.PrivKeyFromBytes(raw)
	return &HyperliquidSigner{
		priv: priv,
		addr: deriveEVMAddress(priv.PubKey()),
	}, nil
}

// PrivateKeyHex returns the 0x-prefixed hex-encoded private key. Callers MUST
// treat this as sensitive material.
func (s *HyperliquidSigner) PrivateKeyHex() string {
	return "0x" + hex.EncodeToString(s.priv.Serialize())
}

// PrivateKeyBytes returns the raw 32-byte secp256k1 secret. Callers MUST
// treat this as sensitive material — never log it, never persist it
// outside the encrypted keystore. The byte slice is a fresh copy so the
// caller can zeroise it.
func (s *HyperliquidSigner) PrivateKeyBytes() []byte {
	raw := s.priv.Serialize()
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

func (s *HyperliquidSigner) Address() string      { return s.addr }
func (s *HyperliquidSigner) Chain() types.ChainID { return types.ChainHyperliquid }

// Sign produces a 65-byte ECDSA signature in [R || S || V] form, which is
// the EVM-standard layout used by Hyperliquid's EIP-712 actions. The payload
// passed in MUST be the 32-byte hash to sign (callers handle hashing because
// the hash type is action-specific in the Hyperliquid protocol).
func (s *HyperliquidSigner) Sign(_ context.Context, payload []byte) ([]byte, error) {
	if s.priv == nil {
		return nil, errors.New("hyperliquid: signer not initialised")
	}
	if len(payload) != 32 {
		return nil, fmt.Errorf("hyperliquid: Sign expects a 32-byte hash, got %d bytes", len(payload))
	}
	sig := ecdsa.SignCompact(s.priv, payload, false)
	// dcrec returns [V || R || S] (65 bytes). Reorder to [R || S || V] for EVM.
	out := make([]byte, 65)
	copy(out[0:32], sig[1:33])
	copy(out[32:64], sig[33:65])
	out[64] = sig[0] - 27 // dcrec uses 27/28; EVM expects 0/1
	return out, nil
}

// deriveEVMAddress computes the 0x-prefixed lowercase EVM address from a
// secp256k1 public key.
func deriveEVMAddress(pub *secp256k1.PublicKey) string {
	ub := pub.SerializeUncompressed() // 65 bytes: 0x04 || X || Y
	h := sha3.NewLegacyKeccak256()
	h.Write(ub[1:])
	sum := h.Sum(nil)
	return "0x" + hex.EncodeToString(sum[12:])
}

// Compile-time check.
var _ Signer = (*HyperliquidSigner)(nil)
