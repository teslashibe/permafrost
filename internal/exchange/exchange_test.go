package exchange_test

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/exchange/noop"
	"github.com/teslashibe/permafrost/pkg/types"
)

func TestNoopVenuePlaceCancel(t *testing.T) {
	v := noop.New()

	intent := types.OrderIntent{
		Venue:    "noop",
		Symbol:   "WIF-PERP",
		Side:     types.SideSell,
		Type:     types.OrderTypeLimit,
		Price:    decimal.NewFromInt(1),
		Size:     decimal.NewFromInt(10),
		ClientID: "client-1",
	}
	ack, err := v.Place(context.Background(), intent)
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if ack.ClientID != "client-1" {
		t.Errorf("ClientID: got %q", ack.ClientID)
	}
	if ack.Status != types.OrderStatusOpen {
		t.Errorf("Status: got %q", ack.Status)
	}
	if got := v.Placed(); len(got) != 1 || got[0].Symbol != "WIF-PERP" {
		t.Errorf("Placed: got %+v", got)
	}

	if err := v.Cancel(context.Background(), ack.ID); err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if got := v.Cancellations(); len(got) != 1 || got[0] != ack.ID {
		t.Errorf("Cancellations: got %+v", got)
	}
}

func TestNoopVenueSubscribeClosesOnCtxCancel(t *testing.T) {
	v := noop.New()
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := v.Subscribe(ctx, []string{"WIF-PERP"})
	if err != nil {
		t.Fatalf("Subscribe: %v", err)
	}
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Errorf("expected closed channel")
		}
	case <-time.After(time.Second):
		t.Fatalf("channel not closed within 1s")
	}
}
