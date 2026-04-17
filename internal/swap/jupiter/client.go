// Package jupiter implements the swap.SwapVenue interface against the
// Jupiter aggregator on Solana. Swaps are submitted via Jito bundles for
// MEV protection by default; raw RPC submission is also supported.
//
// Jupiter API reference: https://dev.jup.ag/docs/api-reference/swap/v1/quote
package jupiter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultBaseURL = "https://api.jup.ag"

// Client is a thin HTTP client over Jupiter v1.
type Client struct {
	base    string
	apiKey  string
	http    *http.Client
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithBaseURL overrides the default https://api.jup.ag base URL.
func WithBaseURL(u string) ClientOption {
	return func(c *Client) { c.base = strings.TrimRight(u, "/") }
}

// WithAPIKey installs an x-api-key header for authenticated tiers.
func WithAPIKey(k string) ClientOption {
	return func(c *Client) { c.apiKey = k }
}

// NewClient constructs a Client with sensible defaults.
func NewClient(opts ...ClientOption) *Client {
	c := &Client{
		base: defaultBaseURL,
		http: &http.Client{Timeout: 15 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// QuoteResponse is the JSON shape returned by GET /swap/v1/quote. The full
// response is preserved as opaque RouteJSON because it must be passed back
// unchanged to /swap/v1/swap.
type QuoteResponse struct {
	InputMint            string          `json:"inputMint"`
	OutputMint           string          `json:"outputMint"`
	InAmount             string          `json:"inAmount"`            // raw base units
	OutAmount            string          `json:"outAmount"`           // raw base units, expected
	OtherAmountThreshold string          `json:"otherAmountThreshold"` // min out (or max in)
	SwapMode             string          `json:"swapMode"`            // "ExactIn" | "ExactOut"
	SlippageBps          int             `json:"slippageBps"`
	PriceImpactPct       string          `json:"priceImpactPct"`
	RoutePlan            json.RawMessage `json:"routePlan"`
	ContextSlot          uint64          `json:"contextSlot"`
	TimeTaken            float64         `json:"timeTaken"`

	// RouteJSON is the entire raw response body. Pass it back to Swap as-is.
	RouteJSON json.RawMessage `json:"-"`
}

// QuoteParams configures a /quote call.
type QuoteParams struct {
	InputMint         string
	OutputMint        string
	Amount            uint64 // base units
	SlippageBps       int    // 50 = 0.5%
	SwapMode          string // "ExactIn" (default) | "ExactOut"
	OnlyDirectRoutes  bool
	RestrictIntermediateTokens bool
	MaxAccounts       int
}

// Quote calls GET /swap/v1/quote and returns the parsed response. The raw
// JSON is preserved on QuoteResponse.RouteJSON for use with Swap().
func (c *Client) Quote(ctx context.Context, p QuoteParams) (*QuoteResponse, error) {
	q := url.Values{}
	q.Set("inputMint", p.InputMint)
	q.Set("outputMint", p.OutputMint)
	q.Set("amount", strconv.FormatUint(p.Amount, 10))
	if p.SlippageBps > 0 {
		q.Set("slippageBps", strconv.Itoa(p.SlippageBps))
	}
	if p.SwapMode != "" {
		q.Set("swapMode", p.SwapMode)
	}
	if p.OnlyDirectRoutes {
		q.Set("onlyDirectRoutes", "true")
	}
	if p.RestrictIntermediateTokens {
		q.Set("restrictIntermediateTokens", "true")
	}
	if p.MaxAccounts > 0 {
		q.Set("maxAccounts", strconv.Itoa(p.MaxAccounts))
	}
	u := c.base + "/swap/v1/quote?" + q.Encode()
	body, err := c.do(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	var qr QuoteResponse
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, fmt.Errorf("jupiter: decode quote: %w", err)
	}
	qr.RouteJSON = body
	return &qr, nil
}

// SwapParams configures a /swap call.
type SwapParams struct {
	QuoteResponse                json.RawMessage // raw bytes from Quote()
	UserPublicKey                string          // base58
	WrapAndUnwrapSOL             bool
	UseSharedAccounts            bool
	FeeAccount                   string
	ComputeUnitPriceMicroLamports *uint64        // priority fee
	AsLegacyTransaction          bool
	DynamicComputeUnitLimit      bool
	SkipUserAccountsRpcCalls     bool
}

// SwapResponse is the JSON shape returned by /swap/v1/swap.
type SwapResponse struct {
	SwapTransaction       string `json:"swapTransaction"`       // base64-encoded
	LastValidBlockHeight  uint64 `json:"lastValidBlockHeight"`
	PrioritizationFeeLamports uint64 `json:"prioritizationFeeLamports"`
}

// Swap calls POST /swap/v1/swap and returns the assembled transaction.
func (c *Client) Swap(ctx context.Context, p SwapParams) (*SwapResponse, error) {
	if p.UserPublicKey == "" {
		return nil, fmt.Errorf("jupiter: UserPublicKey is required")
	}
	if len(p.QuoteResponse) == 0 {
		return nil, fmt.Errorf("jupiter: QuoteResponse is required")
	}
	body := map[string]any{
		"quoteResponse":            json.RawMessage(p.QuoteResponse),
		"userPublicKey":            p.UserPublicKey,
		"wrapAndUnwrapSol":         p.WrapAndUnwrapSOL,
		"useSharedAccounts":        p.UseSharedAccounts,
		"asLegacyTransaction":      p.AsLegacyTransaction,
		"dynamicComputeUnitLimit":  p.DynamicComputeUnitLimit,
		"skipUserAccountsRpcCalls": p.SkipUserAccountsRpcCalls,
	}
	if p.FeeAccount != "" {
		body["feeAccount"] = p.FeeAccount
	}
	if p.ComputeUnitPriceMicroLamports != nil {
		body["computeUnitPriceMicroLamports"] = *p.ComputeUnitPriceMicroLamports
	}
	raw, err := c.do(ctx, http.MethodPost, c.base+"/swap/v1/swap", body)
	if err != nil {
		return nil, err
	}
	var resp SwapResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("jupiter: decode swap: %w", err)
	}
	if resp.SwapTransaction == "" {
		return nil, fmt.Errorf("jupiter: empty swapTransaction in response")
	}
	return &resp, nil
}

// do performs a request, returning raw bytes on success.
func (c *Client) do(ctx context.Context, method, url string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("jupiter: marshal: %w", err)
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.apiKey != "" {
		req.Header.Set("x-api-key", c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jupiter: do: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return raw, fmt.Errorf("jupiter: %s -> %d: %s", url, resp.StatusCode, truncate(raw))
	}
	return raw, nil
}

func truncate(b []byte) string {
	if len(b) > 1024 {
		return string(b[:1024]) + "..."
	}
	return string(b)
}
