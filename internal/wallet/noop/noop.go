// Package noop provides a Signer/Keystore that returns deterministic dummy
// values. For tests only — never use against real funds.
package noop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/teslashibe/permafrost/internal/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// Signer is a deterministic stub.
type Signer struct {
	chain types.ChainID
	addr  string
}

// NewSigner constructs a stub for the given chain. The address is a stable
// hash so tests can pin against it.
func NewSigner(chain types.ChainID) *Signer {
	h := sha256.Sum256([]byte("noop-signer:" + string(chain)))
	return &Signer{chain: chain, addr: "noop_" + hex.EncodeToString(h[:8])}
}

func (s *Signer) Address() string                                  { return s.addr }
func (s *Signer) Chain() types.ChainID                             { return s.chain }
func (s *Signer) Sign(_ context.Context, payload []byte) ([]byte, error) {
	h := sha256.Sum256(append([]byte(s.addr+":"), payload...))
	return h[:], nil
}

// Keystore is a chain → Signer map.
type Keystore struct {
	signers map[types.ChainID]wallet.Signer
}

// NewKeystore constructs a Keystore preloaded with stub signers for the
// supplied chains.
func NewKeystore(chains ...types.ChainID) *Keystore {
	ks := &Keystore{signers: make(map[types.ChainID]wallet.Signer, len(chains))}
	for _, c := range chains {
		ks.signers[c] = NewSigner(c)
	}
	return ks
}

func (k *Keystore) Signer(chain types.ChainID) (wallet.Signer, error) {
	s, ok := k.signers[chain]
	if !ok {
		return nil, fmt.Errorf("no signer for chain %q", chain)
	}
	return s, nil
}

func (k *Keystore) Chains() []types.ChainID {
	out := make([]types.ChainID, 0, len(k.signers))
	for c := range k.signers {
		out = append(out, c)
	}
	return out
}

var (
	_ wallet.Signer   = (*Signer)(nil)
	_ wallet.Keystore = (*Keystore)(nil)
)
