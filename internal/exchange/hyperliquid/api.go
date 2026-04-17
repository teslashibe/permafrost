package hyperliquid

import "encoding/json"

// ─── /info request envelope ──────────────────────────────────────────────────

// infoRequest is the discriminated-union body sent to /info. Different
// request types use different field sets; we include them all and rely on
// omitempty.
type infoRequest struct {
	Type      string `json:"type"`
	User      string `json:"user,omitempty"`
	Coin      string `json:"coin,omitempty"`
	StartTime int64  `json:"startTime,omitempty"`
	EndTime   int64  `json:"endTime,omitempty"`
}

// ─── clearinghouseState response ────────────────────────────────────────────

type clearinghouseStateResp struct {
	AssetPositions []struct {
		Position struct {
			Coin           string `json:"coin"`
			Szi            string `json:"szi"`              // signed size, base units
			EntryPx        string `json:"entryPx"`
			LiquidationPx  string `json:"liquidationPx"`
			MarginUsed     string `json:"marginUsed"`
			UnrealizedPnl  string `json:"unrealizedPnl"`
			ReturnOnEquity string `json:"returnOnEquity"`
			Leverage       struct {
				Type  string `json:"type"`
				Value int    `json:"value"`
			} `json:"leverage"`
		} `json:"position"`
		Type string `json:"type"`
	} `json:"assetPositions"`

	CrossMarginSummary struct {
		AccountValue    string `json:"accountValue"`
		TotalNtlPos     string `json:"totalNtlPos"`
		TotalRawUsd     string `json:"totalRawUsd"`
		TotalMarginUsed string `json:"totalMarginUsed"`
	} `json:"crossMarginSummary"`

	MarginSummary struct {
		AccountValue    string `json:"accountValue"`
		TotalNtlPos     string `json:"totalNtlPos"`
		TotalRawUsd     string `json:"totalRawUsd"`
		TotalMarginUsed string `json:"totalMarginUsed"`
	} `json:"marginSummary"`

	Withdrawable string `json:"withdrawable"`
	Time         int64  `json:"time"`
}

// ─── metaAndAssetCtxs response ──────────────────────────────────────────────

// metaAndAssetCtxsResp is a 2-tuple of (meta, ctxs) returned as a JSON array.
type metaAndAssetCtxsResp struct {
	Meta meta
	Ctxs []assetCtx
}

func (r *metaAndAssetCtxsResp) UnmarshalJSON(b []byte) error {
	var raw [2]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(raw[0], &r.Meta); err != nil {
		return err
	}
	return json.Unmarshal(raw[1], &r.Ctxs)
}

type meta struct {
	Universe []universeItem `json:"universe"`
}

type universeItem struct {
	Name         string `json:"name"`
	SzDecimals   int    `json:"szDecimals"`
	MaxLeverage  int    `json:"maxLeverage"`
	OnlyIsolated bool   `json:"onlyIsolated"`
}

// assetCtx is one row in the second element of metaAndAssetCtxs. It
// corresponds 1:1 with universe[i] by index.
type assetCtx struct {
	Funding      string   `json:"funding"`       // per-hour funding rate, fractional
	OpenInterest string   `json:"openInterest"`
	PrevDayPx    string   `json:"prevDayPx"`
	DayNtlVlm    string   `json:"dayNtlVlm"`
	Premium      string   `json:"premium"`
	OraclePx     string   `json:"oraclePx"`
	MarkPx       string   `json:"markPx"`
	MidPx        string   `json:"midPx"`
	ImpactPxs    []string `json:"impactPxs"`
}

// ─── L2 book snapshot ───────────────────────────────────────────────────────

type l2BookResp struct {
	Coin   string         `json:"coin"`
	Time   int64          `json:"time"`
	Levels [2][]bookLevel `json:"levels"` // [bids, asks]
}

type bookLevel struct {
	Px string `json:"px"`
	Sz string `json:"sz"`
	N  int    `json:"n"` // number of orders at this level
}
