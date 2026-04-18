package wallet

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"

	ethcrypto "github.com/ethereum/go-ethereum/crypto"

	"github.com/teslashibe/permafrost/internal/types"
)

// EVMSigner wraps the same secp256k1 keypair used for Hyperliquid and
// adapts it to (1) the Signer interface (for any future generic
// EVM-payload signing needs) and (2) a *crypto/ecdsa.PrivateKey for
// go-ethereum's tx-signing path.
//
// Design rule: one secp256k1 secret → one EVM address → all four EVM
// chains AND Hyperliquid. Operators only manage one secret; the
// keystore stores it once under the "hyperliquid" slot.
//
// EVMSigner is bound to a SPECIFIC chain so deps wiring can label it
// per-chain; the underlying key is identical across instances.
type EVMSigner struct {
	hl    *HyperliquidSigner
	chain types.ChainID
	priv  *ecdsa.PrivateKey
}

// NewEVMSigner adapts an existing HyperliquidSigner for use on any EVM
// chain. The address is the same across all of them.
func NewEVMSigner(hl *HyperliquidSigner, chain types.ChainID) (*EVMSigner, error) {
	if hl == nil {
		return nil, errors.New("evm: nil hyperliquid signer")
	}
	if !chain.IsEVM() {
		return nil, fmt.Errorf("evm: chain %q is not an EVM chain", chain)
	}
	priv, err := ethcrypto.ToECDSA(hl.PrivateKeyBytes())
	if err != nil {
		return nil, fmt.Errorf("evm: bad secp256k1 secret: %w", err)
	}
	return &EVMSigner{hl: hl, chain: chain, priv: priv}, nil
}

// PrivateKey exposes the *ecdsa.PrivateKey for the go-ethereum tx
// signer. The returned pointer MUST NOT be persisted, logged, or
// shipped over the network.
func (s *EVMSigner) PrivateKey() *ecdsa.PrivateKey { return s.priv }

// Address is the same 0x-prefixed lowercase address as the underlying
// HyperliquidSigner — cross-chain identity is intentional.
func (s *EVMSigner) Address() string { return s.hl.Address() }

// Chain is the EVM chain this signer is labelled for. Same secret can
// back many EVMSigners with different Chain() values.
func (s *EVMSigner) Chain() types.ChainID { return s.chain }

// Sign produces a 65-byte ECDSA signature in [R || S || V] form. The
// payload MUST be the 32-byte hash to sign. We delegate to the underlying
// HyperliquidSigner so the signing surface is identical across HL EIP-712
// actions and any future EVM personal_sign use case.
func (s *EVMSigner) Sign(ctx context.Context, payload []byte) ([]byte, error) {
	return s.hl.Sign(ctx, payload)
}

// Compile-time check.
var _ Signer = (*EVMSigner)(nil)

// EVMSignerFromKeystore resolves the Hyperliquid signer from a keystore
// and binds it to the requested EVM chain. Returns ErrNoEVMSigner if
// no Hyperliquid key is configured.
func EVMSignerFromKeystore(ks Keystore, chain types.ChainID) (*EVMSigner, error) {
	if ks == nil {
		return nil, ErrNoEVMSigner
	}
	s, err := ks.Signer(types.ChainHyperliquid)
	if err != nil {
		return nil, ErrNoEVMSigner
	}
	hl, ok := s.(*HyperliquidSigner)
	if !ok {
		return nil, fmt.Errorf("evm: keystore signer for hyperliquid is %T, not *HyperliquidSigner", s)
	}
	return NewEVMSigner(hl, chain)
}

// ErrNoEVMSigner is returned when the keystore has no EVM-capable key.
// Since we reuse the Hyperliquid secp256k1 key for EVM, this means "no
// Hyperliquid key configured".
var ErrNoEVMSigner = errors.New("wallet: no EVM signer in keystore (configure a Hyperliquid key)")
