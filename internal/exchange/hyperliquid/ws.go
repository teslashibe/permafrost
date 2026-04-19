package hyperliquid

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/pkg/types"
)

// subscribe opens a WebSocket connection to the supplied URL and streams
// MarketEvents for the given symbols. The returned channel is closed when
// ctx is cancelled or an unrecoverable error occurs.
//
// Subscription set: trades + l2Book per symbol, and one allMids subscription
// across all symbols (used to derive Tick events).
func subscribe(ctx context.Context, wsURL string, symbols []string) (<-chan types.MarketEvent, error) {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid: ws dial: %w", err)
	}
	conn.SetReadLimit(2 << 20) // 2 MiB

	// allMids first, then per-symbol l2Book + trades.
	if err := writeSub(conn, "allMids", "", ""); err != nil {
		conn.Close()
		return nil, err
	}
	for _, sym := range symbols {
		if err := writeSub(conn, "l2Book", sym, ""); err != nil {
			conn.Close()
			return nil, err
		}
		if err := writeSub(conn, "trades", sym, ""); err != nil {
			conn.Close()
			return nil, err
		}
	}

	out := make(chan types.MarketEvent, 256)

	go func() {
		defer close(out)
		defer conn.Close()
		go pingLoop(ctx, conn)

		for {
			if err := ctx.Err(); err != nil {
				return
			}
			_ = conn.SetReadDeadline(time.Now().Add(60 * time.Second))
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			ev, ok := parseMessage(raw)
			if !ok {
				continue
			}
			select {
			case out <- ev:
			case <-ctx.Done():
				return
			}
		}
	}()

	return out, nil
}

// writeSub sends a subscription frame.
func writeSub(conn *websocket.Conn, kind, coin, dex string) error {
	payload := map[string]any{
		"method": "subscribe",
		"subscription": func() map[string]any {
			s := map[string]any{"type": kind}
			if coin != "" {
				s["coin"] = coin
			}
			if dex != "" {
				s["dex"] = dex
			}
			return s
		}(),
	}
	b, _ := json.Marshal(payload)
	return conn.WriteMessage(websocket.TextMessage, b)
}

func pingLoop(ctx context.Context, conn *websocket.Conn) {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			_ = conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second))
		}
	}
}

// wsEnvelope is the outer shape every Hyperliquid WS frame conforms to.
type wsEnvelope struct {
	Channel string          `json:"channel"`
	Data    json.RawMessage `json:"data"`
}

// parseMessage converts a raw WS frame into a typed MarketEvent. Unknown
// channels are dropped silently and (false) is returned.
func parseMessage(raw []byte) (types.MarketEvent, bool) {
	var env wsEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return types.MarketEvent{}, false
	}
	switch env.Channel {
	case "subscriptionResponse", "pong":
		return types.MarketEvent{}, false
	case "allMids":
		return parseAllMids(env.Data)
	case "l2Book":
		return parseL2Book(env.Data)
	case "trades":
		return parseTrades(env.Data)
	default:
		return types.MarketEvent{}, false
	}
}

// parseAllMids fans every coin's mid into a single Tick event. We collapse
// to one event per frame containing only the first coin, since downstream
// consumers index by symbol; emitting one event per coin would multiply
// channel pressure unnecessarily for an MVP.
//
// For multi-symbol updates, callers may want a richer event type later.
func parseAllMids(data json.RawMessage) (types.MarketEvent, bool) {
	var payload struct {
		Mids map[string]string `json:"mids"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return types.MarketEvent{}, false
	}
	for coin, midStr := range payload.Mids {
		mid, err := decimal.NewFromString(midStr)
		if err != nil {
			continue
		}
		return types.MarketEvent{
			Kind: types.EventTick,
			Tick: &types.Tick{
				Time:   time.Now().UTC(),
				Venue:  VenueName,
				Symbol: coin,
				Last:   mid,
			},
		}, true
	}
	return types.MarketEvent{}, false
}

func parseL2Book(data json.RawMessage) (types.MarketEvent, bool) {
	var book l2BookResp
	if err := json.Unmarshal(data, &book); err != nil {
		return types.MarketEvent{}, false
	}
	convert := func(in []bookLevel) []types.BookLevel {
		out := make([]types.BookLevel, 0, len(in))
		for _, lvl := range in {
			px, err1 := decimal.NewFromString(lvl.Px)
			sz, err2 := decimal.NewFromString(lvl.Sz)
			if err1 != nil || err2 != nil {
				continue
			}
			out = append(out, types.BookLevel{Price: px, Size: sz})
		}
		return out
	}
	if len(book.Levels) < 2 {
		return types.MarketEvent{}, false
	}
	snap := types.BookSnapshot{
		Time:   time.Unix(0, book.Time*int64(time.Millisecond)).UTC(),
		Venue:  VenueName,
		Symbol: book.Coin,
		Bids:   convert(book.Levels[0]),
		Asks:   convert(book.Levels[1]),
	}
	return types.MarketEvent{Kind: types.EventBook, Book: &snap}, true
}

func parseTrades(data json.RawMessage) (types.MarketEvent, bool) {
	var trades []struct {
		Coin  string `json:"coin"`
		Side  string `json:"side"` // "B" | "A"
		Px    string `json:"px"`
		Sz    string `json:"sz"`
		Time  int64  `json:"time"`
	}
	if err := json.Unmarshal(data, &trades); err != nil || len(trades) == 0 {
		return types.MarketEvent{}, false
	}
	t := trades[0]
	px, _ := decimal.NewFromString(t.Px)
	sz, _ := decimal.NewFromString(t.Sz)
	side := types.SideBuy
	if strings.EqualFold(t.Side, "A") {
		side = types.SideSell
	}
	return types.MarketEvent{
		Kind: types.EventTrade,
		Trade: &types.Trade{
			Time:   time.Unix(0, t.Time*int64(time.Millisecond)).UTC(),
			Venue:  VenueName,
			Symbol: t.Coin,
			Price:  px,
			Size:   sz,
			Side:   side,
		},
	}, true
}
