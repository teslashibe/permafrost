// Package noop provides a Venue implementation that returns empty data and
// records calls. Used for tests of the agent runtime and as a stand-in
// while real adapters are being developed.
package noop

import (
	"context"
	"sync"
	"time"

	"github.com/teslashibe/permafrost/internal/exchange"
	"github.com/teslashibe/permafrost/pkg/types"
)

// Name is the registered venue identifier.
const Name = "noop"

// Venue is a no-op exchange.
type Venue struct {
	mu       sync.Mutex
	placed   []types.OrderIntent
	cancels  []types.OrderID
	openOrds []exchange.OpenOrder // tests can pre-seed via SetOpenOrders
}

// New constructs a fresh no-op venue.
func New() *Venue { return &Venue{} }

// Compile-time check.
var _ exchange.Venue = (*Venue)(nil)

func (v *Venue) Name() string { return Name }

func (v *Venue) Subscribe(ctx context.Context, _ []string) (<-chan types.MarketEvent, error) {
	ch := make(chan types.MarketEvent)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (v *Venue) Place(_ context.Context, intent types.OrderIntent) (types.OrderAck, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.placed = append(v.placed, intent)
	return types.OrderAck{
		ID:         types.OrderID("noop-" + string(intent.ClientID)),
		ClientID:   intent.ClientID,
		AcceptedAt: time.Now().UTC(),
		Status:     types.OrderStatusOpen,
		Remaining:  intent.Size,
	}, nil
}

func (v *Venue) Cancel(_ context.Context, id types.OrderID) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cancels = append(v.cancels, id)
	return nil
}

func (v *Venue) Positions(_ context.Context) ([]types.Position, error)         { return nil, nil }
func (v *Venue) Balances(_ context.Context) ([]types.Balance, error)           { return nil, nil }
func (v *Venue) FundingRates(_ context.Context, _ []string) ([]types.FundingRate, error) {
	return nil, nil
}

// OpenOrders returns whatever the test seeded via SetOpenOrders.
// Empty by default, which is the right answer for a noop venue.
func (v *Venue) OpenOrders(_ context.Context) ([]exchange.OpenOrder, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]exchange.OpenOrder, len(v.openOrds))
	copy(out, v.openOrds)
	return out, nil
}

// SetOpenOrders pre-seeds the venue with open orders, for tests of the
// kill-switch cancel path.
func (v *Venue) SetOpenOrders(orders []exchange.OpenOrder) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.openOrds = append(v.openOrds[:0], orders...)
}

// Placed returns the intents recorded by Place, in submission order.
func (v *Venue) Placed() []types.OrderIntent {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]types.OrderIntent, len(v.placed))
	copy(out, v.placed)
	return out
}

// Cancellations returns ids passed to Cancel, in submission order.
func (v *Venue) Cancellations() []types.OrderID {
	v.mu.Lock()
	defer v.mu.Unlock()
	out := make([]types.OrderID, len(v.cancels))
	copy(out, v.cancels)
	return out
}
