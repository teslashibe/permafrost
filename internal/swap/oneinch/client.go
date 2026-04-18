// Package oneinch is the SwapVenue adapter for 1inch's v6 aggregator API.
// One Venue covers one EVM chain; spin up multiple Venues with the same
// API key (and different chain ids) for multi-chain operation.
//
// API docs: https://portal.1inch.dev/documentation/swap/v6.0/
//
// Endpoints we use (all under /swap/v6.0/{chainId}/):
//
//	GET /quote                        — price discovery, no calldata
//	GET /swap                         — full quote + signed-tx payload
//	GET /approve/allowance            — current allowance for our wallet
//	GET /approve/transaction          — calldata for an approve() to the router
//
// 1inch is mainnet-only. Testnet aggregator support does not exist;
// see SCOPE.md for how we test (mock + mainnet-with-cap).
package oneinch

import (
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

// DefaultBaseURL is the public 1inch API base. Operators with their own
// proxy can override via WithBaseURL.
const DefaultBaseURL = "https://api.1inch.dev"

// Client is a thin HTTP client over the 1inch swap API. Safe for
// concurrent use.
type Client struct {
	baseURL string
	apiKey  string
	chainID uint64
	http    *http.Client
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base. Empty string is a no-op (keeps default).
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithHTTPClient lets tests inject a custom *http.Client (e.g. with a
// shorter timeout or a stubbed Transport).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// NewClient constructs a 1inch client for chainID. apiKey is required —
// 1inch returns 401 without one. Get a free key from
// https://portal.1inch.dev .
func NewClient(chainID uint64, apiKey string, opts ...Option) *Client {
	c := &Client{
		baseURL: DefaultBaseURL,
		apiKey:  apiKey,
		chainID: chainID,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// ChainID returns the chain id this client targets.
func (c *Client) ChainID() uint64 { return c.chainID }

// QuoteParams are the inputs to /quote. Amount must be in base units
// (e.g. wei for ETH-decimaled tokens).
type QuoteParams struct {
	Src        string // token contract address (0x…); use 0xeeee… for native
	Dst        string // token contract address (0x…)
	Amount     string // base-unit amount as a decimal string
	IncludeGas bool   // if true, response includes estimated gas
}

// QuoteResponse is the subset of fields we use.
type QuoteResponse struct {
	DstAmount string `json:"dstAmount"`
	Gas       uint64 `json:"gas,omitempty"`
}

// Quote fetches a price quote.
func (c *Client) Quote(ctx context.Context, p QuoteParams) (*QuoteResponse, error) {
	q := url.Values{}
	q.Set("src", p.Src)
	q.Set("dst", p.Dst)
	q.Set("amount", p.Amount)
	if p.IncludeGas {
		q.Set("includeGas", "true")
	}
	var out QuoteResponse
	if err := c.get(ctx, fmt.Sprintf("/swap/v6.0/%d/quote", c.chainID), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// SwapParams are the inputs to /swap.
type SwapParams struct {
	Src      string // src token (0x…)
	Dst      string // dst token (0x…)
	Amount   string // base units
	From     string // wallet address (0x…)
	Slippage string // percent, e.g. "0.5" for 50bps. 1inch uses % not bps.

	// Optional — a few of the most useful flags.
	DisableEstimate    bool   // skip 1inch's tx simulation (we'll let RPC fail if it's bad)
	AllowPartialFill   bool   // accept a less-than-full fill rather than revert
	IncludeProtocols   bool   // include the route in the response (handy for logs)
	IncludeGas         bool   // include estimated gas
	Receiver           string // optional; default = From
	Referrer           string // optional; for fee sharing
	Permit             string // hex-encoded EIP-2612 permit; empty for plain approve flow
}

// SwapResponse is the subset of fields we use. tx is the executable
// transaction calldata + target.
type SwapResponse struct {
	DstAmount string `json:"dstAmount"`
	Tx        struct {
		From     string `json:"from"`
		To       string `json:"to"`
		Data     string `json:"data"`
		Value    string `json:"value"`
		Gas      uint64 `json:"gas,omitempty"`
		GasPrice string `json:"gasPrice,omitempty"`
	} `json:"tx"`
	Protocols json.RawMessage `json:"protocols,omitempty"`
}

// Swap fetches the full swap payload (quote + tx data).
func (c *Client) Swap(ctx context.Context, p SwapParams) (*SwapResponse, error) {
	if p.From == "" {
		return nil, fmt.Errorf("oneinch: From is required")
	}
	if p.Slippage == "" {
		p.Slippage = "0.5"
	}
	q := url.Values{}
	q.Set("src", p.Src)
	q.Set("dst", p.Dst)
	q.Set("amount", p.Amount)
	q.Set("from", p.From)
	q.Set("slippage", p.Slippage)
	if p.DisableEstimate {
		q.Set("disableEstimate", "true")
	}
	if p.AllowPartialFill {
		q.Set("allowPartialFill", "true")
	}
	if p.IncludeProtocols {
		q.Set("includeProtocols", "true")
	}
	if p.IncludeGas {
		q.Set("includeGas", "true")
	}
	if p.Receiver != "" {
		q.Set("receiver", p.Receiver)
	}
	if p.Referrer != "" {
		q.Set("referrer", p.Referrer)
	}
	if p.Permit != "" {
		q.Set("permit", p.Permit)
	}
	var out SwapResponse
	if err := c.get(ctx, fmt.Sprintf("/swap/v6.0/%d/swap", c.chainID), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Allowance returns how much the spender (1inch router) can pull from
// owner for tokenAddress. Result is a base-unit decimal string.
type AllowanceResponse struct {
	Allowance string `json:"allowance"`
}

func (c *Client) Allowance(ctx context.Context, tokenAddress, walletAddress string) (string, error) {
	q := url.Values{}
	q.Set("tokenAddress", tokenAddress)
	q.Set("walletAddress", walletAddress)
	var out AllowanceResponse
	if err := c.get(ctx, fmt.Sprintf("/swap/v6.0/%d/approve/allowance", c.chainID), q, &out); err != nil {
		return "", err
	}
	return out.Allowance, nil
}

// ApproveTx returns the calldata + target for an approve(MAX) (or
// approve(amount) if amount is non-empty) to the 1inch router.
type ApproveTxResponse struct {
	To       string `json:"to"`
	Data     string `json:"data"`
	Value    string `json:"value"`
	GasPrice string `json:"gasPrice,omitempty"`
}

func (c *Client) ApproveTx(ctx context.Context, tokenAddress, amount string) (*ApproveTxResponse, error) {
	q := url.Values{}
	q.Set("tokenAddress", tokenAddress)
	if amount != "" {
		q.Set("amount", amount)
	}
	var out ApproveTxResponse
	if err := c.get(ctx, fmt.Sprintf("/swap/v6.0/%d/approve/transaction", c.chainID), q, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Spender returns the 1inch router address — the spender that needs to
// be approved before /swap can pull tokens. Result is cached per chain
// inside 1inch and rarely changes, but we re-fetch on each call to be
// safe.
type SpenderResponse struct {
	Address string `json:"address"`
}

func (c *Client) Spender(ctx context.Context) (string, error) {
	var out SpenderResponse
	if err := c.get(ctx, fmt.Sprintf("/swap/v6.0/%d/approve/spender", c.chainID), nil, &out); err != nil {
		return "", err
	}
	return out.Address, nil
}

// ─── transport ──────────────────────────────────────────────────────────────

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	u := c.baseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("oneinch: http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("oneinch: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		// 1inch returns JSON errors with description + statusCode fields.
		var apiErr struct {
			Description string `json:"description"`
			Status      int    `json:"statusCode"`
			Error       string `json:"error"`
		}
		_ = json.Unmarshal(body, &apiErr)
		msg := apiErr.Description
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		return fmt.Errorf("oneinch %d: %s", resp.StatusCode, msg)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("oneinch: parse response: %w (body=%q)", err, string(body))
	}
	return nil
}

// ParseUint parses a 1inch base-unit amount string. Helper for callers.
func ParseUint(s string) (uint64, error) { return strconv.ParseUint(strings.TrimSpace(s), 10, 64) }
