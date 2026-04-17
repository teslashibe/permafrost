package hyperliquid

import (
	"testing"

	"github.com/teslashibe/permafrost/internal/types"
)

func TestParseAllMids(t *testing.T) {
	raw := []byte(`{"channel":"allMids","data":{"mids":{"WIF":"1.234"}}}`)
	ev, ok := parseMessage(raw)
	if !ok {
		t.Fatal("expected event")
	}
	if ev.Kind != types.EventTick || ev.Tick == nil {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.Tick.Symbol != "WIF" || ev.Tick.Last.String() != "1.234" {
		t.Errorf("tick: %+v", ev.Tick)
	}
	if ev.Tick.Venue != VenueName {
		t.Errorf("venue: %q", ev.Tick.Venue)
	}
}

func TestParseL2Book(t *testing.T) {
	raw := []byte(`{
		"channel":"l2Book",
		"data":{
			"coin":"WIF",
			"time":1700000000000,
			"levels":[
				[{"px":"1.50","sz":"100","n":3},{"px":"1.49","sz":"200","n":5}],
				[{"px":"1.51","sz":"50","n":1},{"px":"1.52","sz":"75","n":2}]
			]
		}
	}`)
	ev, ok := parseMessage(raw)
	if !ok {
		t.Fatal("expected event")
	}
	if ev.Kind != types.EventBook || ev.Book == nil {
		t.Fatalf("unexpected event: %+v", ev)
	}
	b := ev.Book
	if b.Symbol != "WIF" {
		t.Errorf("symbol: %q", b.Symbol)
	}
	if len(b.Bids) != 2 || len(b.Asks) != 2 {
		t.Errorf("levels: bids=%d asks=%d", len(b.Bids), len(b.Asks))
	}
	if b.Bids[0].Price.String() != "1.5" {
		t.Errorf("top bid: %s", b.Bids[0].Price)
	}
}

func TestParseTrades(t *testing.T) {
	raw := []byte(`{
		"channel":"trades",
		"data":[{"coin":"WIF","side":"A","px":"1.50","sz":"10","time":1700000000000}]
	}`)
	ev, ok := parseMessage(raw)
	if !ok {
		t.Fatal("expected event")
	}
	if ev.Kind != types.EventTrade || ev.Trade == nil {
		t.Fatalf("unexpected event: %+v", ev)
	}
	if ev.Trade.Side != types.SideSell {
		t.Errorf("side A should map to sell, got %q", ev.Trade.Side)
	}
}

func TestParseMessage_IgnoresControl(t *testing.T) {
	cases := [][]byte{
		[]byte(`{"channel":"subscriptionResponse","data":{}}`),
		[]byte(`{"channel":"pong","data":{}}`),
		[]byte(`{"channel":"unknown","data":{}}`),
		[]byte(`not json`),
	}
	for _, raw := range cases {
		if _, ok := parseMessage(raw); ok {
			t.Errorf("expected drop for %q", string(raw))
		}
	}
}
