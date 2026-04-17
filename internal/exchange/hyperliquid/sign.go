package hyperliquid

import (
	"context"
	"errors"

	"github.com/teslashibe/permafrost/internal/wallet"
)

// ErrSignerRequired is returned when a write operation (Place/Cancel) is
// attempted without a Signer configured.
var ErrSignerRequired = errors.New("hyperliquid: signer required for write operations; configure with WithSigner")

// ErrSigningNotImplemented is a placeholder for the full EIP-712 signing
// flow which lands with the wallet package in M4. The Venue accepts a
// Signer in this PR so the constructor surface is stable, but real action
// signing is intentionally deferred to keep this PR focused on read-only
// adapter functionality.
var ErrSigningNotImplemented = errors.New("hyperliquid: action signing not yet implemented (M4)")

// signAction prepares an action for submission to /exchange.
//
// Hyperliquid signs actions using EIP-712 over a hash that combines the
// MessagePack-encoded action, a vault identifier, and a nonce. The exact
// implementation is non-trivial and lands in M4. For now this function
// returns ErrSigningNotImplemented so M2 can ship a complete read-only
// surface plus the Place/Cancel structure.
func signAction(_ context.Context, signer wallet.Signer, _ any) ([]byte, error) {
	if signer == nil {
		return nil, ErrSignerRequired
	}
	return nil, ErrSigningNotImplemented
}
