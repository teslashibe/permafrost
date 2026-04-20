// Package bittensor provides a thin JSON-RPC client for the Subtensor chain.
//
// Like internal/chain/solana and internal/chain/evm, we deliberately keep a
// minimal surface: only the storage reads and extrinsics required for alpha
// token trading (stake/unstake into subnet pools). All communication goes
// over WebSocket to a configurable Subtensor RPC endpoint — public, paid,
// or self-hosted.
package bittensor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/shopspring/decimal"
)

const (
	// RAO is the smallest unit of TAO (1 TAO = 1e9 RAO).
	RAOPerTAO = 1_000_000_000
)

// Client is a JSON-RPC client for Subtensor nodes. Safe for concurrent use.
// Connects via HTTP(S) — Substrate nodes expose JSON-RPC on both HTTP and WS.
type Client struct {
	url  string
	http *http.Client
	id   atomic.Uint64
}

// NewClient constructs a Client. The url should be the full Subtensor RPC
// endpoint. WebSocket URLs (wss://) are converted to HTTPS for the HTTP
// transport; callers using actual WebSocket subscriptions should use a
// separate WS client.
func NewClient(url string) *Client {
	httpURL := url
	httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
	httpURL = strings.Replace(httpURL, "ws://", "http://", 1)
	return &Client{
		url:  strings.TrimRight(httpURL, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

// URL returns the configured endpoint.
func (c *Client) URL() string { return c.url }

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  []any  `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      uint64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("subtensor rpc %d: %s", e.Code, e.Message) }

func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	id := c.id.Add(1)
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("subtensor: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("subtensor: do: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("subtensor: %s %d: %s", method, resp.StatusCode, truncate(raw))
	}
	var env rpcResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("subtensor: decode: %w", err)
	}
	if env.Error != nil {
		return env.Error
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("subtensor: decode result: %w", err)
		}
	}
	return nil
}

func truncate(b []byte) string {
	if len(b) > 1024 {
		return string(b[:1024]) + "..."
	}
	return string(b)
}

// ─── public API ─────────────────────────────────────────────────────────────

// SubnetInfo holds on-chain metadata for a single subnet.
type SubnetInfo struct {
	Netuid     uint16          `json:"netuid"`
	Name       string          `json:"name"`
	Tempo      uint16          `json:"tempo"`
	Emission   decimal.Decimal `json:"emission"`
	TaoReserve decimal.Decimal `json:"tao_reserve"`
	AlphaReserve decimal.Decimal `json:"alpha_reserve"`
	AlphaPrice decimal.Decimal `json:"alpha_price"` // tao_reserve / alpha_reserve
}

// PoolState holds the AMM reserves for a single subnet.
type PoolState struct {
	Netuid       uint16
	TaoReserve   decimal.Decimal
	AlphaReserve decimal.Decimal
	Price        decimal.Decimal // tao / alpha
}

// GetChain returns the chain name (e.g. "Bittensor" or "Bittensor Testnet").
// Used by permafrost doctor to verify the RPC is alive and pointed at the
// expected network.
func (c *Client) GetChain(ctx context.Context) (string, error) {
	var chain string
	if err := c.call(ctx, "system_chain", []any{}, &chain); err != nil {
		return "", err
	}
	return chain, nil
}

// GetBlockNumber returns the latest finalized block number. Analogous to
// eth_blockNumber for EVM chains.
func (c *Client) GetBlockNumber(ctx context.Context) (uint64, error) {
	var hexHash string
	if err := c.call(ctx, "chain_getFinalizedHead", []any{}, &hexHash); err != nil {
		return 0, err
	}
	var header struct {
		Number string `json:"number"`
	}
	if err := c.call(ctx, "chain_getHeader", []any{hexHash}, &header); err != nil {
		return 0, err
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(header.Number, "0x"), 16)
	return n.Uint64(), nil
}

// GetSubnetCount returns the total number of subnets.
func (c *Client) GetSubnetCount(ctx context.Context) (uint16, error) {
	var result json.RawMessage
	if err := c.call(ctx, "state_call", []any{"SubnetInfoRuntimeApi_get_subnet_count", "0x"}, &result); err != nil {
		return 0, err
	}
	// Fallback: use the subtensorModule.totalNetworks storage
	var hexVal string
	if err := c.call(ctx, "subtensorModule_getSubnetCount", []any{}, &hexVal); err != nil {
		// Not all nodes expose this custom RPC. Try the generic approach.
		return 0, fmt.Errorf("subtensor: getSubnetCount not available: %w", err)
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(hexVal, "0x"), 16)
	return uint16(n.Uint64()), nil
}

// GetSubnetsInfo retrieves info for all subnets via the runtime API.
// This is the primary data source for the CLI subnet listing and market feed.
func (c *Client) GetSubnetsInfo(ctx context.Context) ([]SubnetInfo, error) {
	var result json.RawMessage
	err := c.call(ctx, "subtensorModule_getSubnetsInfo", []any{}, &result)
	if err != nil {
		return nil, fmt.Errorf("subtensor: getSubnetsInfo: %w", err)
	}
	var subnets []SubnetInfo
	if err := json.Unmarshal(result, &subnets); err != nil {
		return nil, fmt.Errorf("subtensor: decode subnets: %w", err)
	}
	return subnets, nil
}

// GetBalance returns the free TAO balance (in RAO) for the given SS58 address.
func (c *Client) GetBalance(ctx context.Context, ss58 string) (uint64, error) {
	var result struct {
		Data struct {
			Free string `json:"free"`
		} `json:"data"`
	}
	if err := c.call(ctx, "system_account", []any{ss58}, &result); err != nil {
		return 0, err
	}
	n := new(big.Int)
	n.SetString(strings.TrimPrefix(result.Data.Free, "0x"), 16)
	return n.Uint64(), nil
}

// RAOToTAO converts RAO (uint64) to TAO as a decimal.
func RAOToTAO(rao uint64) decimal.Decimal {
	return decimal.NewFromUint64(rao).Div(decimal.NewFromInt(RAOPerTAO))
}

// TAOToRAO converts TAO (decimal) to RAO.
func TAOToRAO(tao decimal.Decimal) uint64 {
	return tao.Mul(decimal.NewFromInt(RAOPerTAO)).Truncate(0).BigInt().Uint64()
}
