// Package wallet defines the Signer contract used for chain-specific signing
// operations. Concrete signers and the local encrypted keystore are added in
// the M4 milestone PR.
//
// Design rule: this package is the only one in the repo that holds raw key
// bytes. Everything else operates on Signer.
package wallet

import (
	"context"

	"github.com/teslashibe/permafrost/pkg/types"
)

// Signer signs payloads for a specific chain. Implementations MUST be safe
// for concurrent use.
type Signer interface {
	// Address is the public address derived from the underlying private key,
	// rendered in the chain's canonical encoding (base58 for Solana, 0x-hex
	// for EVM, etc.).
	Address() string

	// Chain identifies which chain this signer is bound to.
	Chain() types.ChainID

	// Sign produces a signature over the supplied payload. The exact bytes
	// signed are chain-specific; see chain-specific implementations for
	// details (e.g. transaction wire bytes for Solana).
	Sign(ctx context.Context, payload []byte) ([]byte, error)
}

// Keystore is the lookup interface used by the agent runtime to obtain a
// Signer for a configured chain. Implementations are added in M4.
type Keystore interface {
	// Signer returns the signer registered for the given chain or an error
	// if no key is configured.
	Signer(chain types.ChainID) (Signer, error)

	// Chains returns the chains for which signers are configured.
	Chains() []types.ChainID
}
