// Package exchange defines the Venue contract: the orderbook-style perpetuals
// venue interface. Concrete adapters (e.g. Hyperliquid) live in subpackages.
//
// SwapVenue, the DEX equivalent for spot leg execution, lives in
// internal/swap. The two interfaces are deliberately distinct because the
// execution semantics differ (place-and-wait vs request-quote-execute-confirm).
package exchange

import (
	"context"

	"github.com/teslashibe/permafrost/internal/types"
)

// Venue is an orderbook venue capable of placing and cancelling orders and
// streaming market data. All methods MUST be safe for concurrent use.
type Venue interface {
	// Name is a stable identifier matching the agents.venue column.
	Name() string

	// Subscribe opens a stream of MarketEvents for the given symbols. The
	// channel is closed when the supplied context is cancelled or the
	// underlying connection terminates with an unrecoverable error.
	Subscribe(ctx context.Context, symbols []string) (<-chan types.MarketEvent, error)

	// Place submits an order. Implementations MUST honour OrderIntent.ClientID
	// for idempotency: a second call with the same ClientID MUST NOT result
	// in a second order, and SHOULD return the original ack if available.
	Place(ctx context.Context, intent types.OrderIntent) (types.OrderAck, error)

	// Cancel removes an open order. Cancelling an unknown or already-terminal
	// order MUST NOT return an error.
	Cancel(ctx context.Context, id types.OrderID) error

	// Positions returns currently-open positions across all symbols.
	Positions(ctx context.Context) ([]types.Position, error)

	// Balances returns the venue-side balance per asset.
	Balances(ctx context.Context) ([]types.Balance, error)

	// FundingRates returns the latest funding observation per symbol. If
	// symbols is empty, all subscribed symbols are returned.
	FundingRates(ctx context.Context, symbols []string) ([]types.FundingRate, error)
}
