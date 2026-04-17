// Package swap defines the SwapVenue contract: the on-chain DEX/aggregator
// interface used to execute spot legs of basis positions.
//
// SwapVenue is deliberately not the same shape as exchange.Venue because
// DEX trading is request-quote-execute-confirm rather than place-and-wait.
// Concrete adapters (Jupiter on Solana, 0x on EVM) live in subpackages.
package swap

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/types"
)

// SwapVenue executes swaps on a single chain via a single aggregator/DEX.
type SwapVenue interface {
	// Name is the adapter identifier (e.g. "jupiter").
	Name() string

	// Chain reports which chain this venue settles on.
	Chain() types.ChainID

	// Quote prices a swap. The returned Quote may carry opaque routing data
	// in its Raw field; that data MUST be passed back unchanged to Swap.
	Quote(ctx context.Context, req types.QuoteRequest) (types.Quote, error)

	// Swap submits the trade for the given quote with the supplied slippage
	// budget. It returns once the transaction has been broadcast; callers
	// should call WaitConfirm to await on-chain confirmation.
	Swap(ctx context.Context, q types.Quote, slippageBps int) (types.TxHash, error)

	// WaitConfirm blocks until the transaction is confirmed or the context
	// is cancelled. Implementations SHOULD return SwapStatusFailed promptly
	// when the chain reports a definitive failure (rather than waiting for
	// the context deadline).
	WaitConfirm(ctx context.Context, tx types.TxHash) (types.SwapResult, error)

	// Balance reports the wallet balance of the given asset for the venue's
	// configured signing address.
	Balance(ctx context.Context, asset types.Asset) (decimal.Decimal, error)
}

// ErrQuoteExpired is returned by Swap when the supplied Quote is past its
// ExpiresAt; the caller should re-quote.
var ErrQuoteExpired = errors.New("swap: quote expired")

// ErrInsufficientBalance is returned by Quote/Swap when the configured
// wallet does not hold enough of the input asset.
var ErrInsufficientBalance = errors.New("swap: insufficient balance")
