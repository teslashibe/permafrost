package evm

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Client is a thin JSON-RPC HTTP client for EVM chains.
//
// It speaks just enough RPC to (1) read balances + allowances, (2) sign
// and broadcast txs, and (3) wait for confirmations. The intentional
// minimum surface keeps the dependency footprint flat and matches
// internal/chain/solana.
type Client struct {
	url   string
	chain Chain
	http  *http.Client
	id    atomic.Uint64
}

// NewClient constructs a Client. URL must be the full RPC endpoint
// (Alchemy, Infura, public, ...). chain identifies the chain so we can
// validate and so callers can read its metadata back.
func NewClient(url string, chain Chain) *Client {
	return &Client{
		url:   strings.TrimRight(url, "/"),
		chain: chain,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Chain returns the chain metadata this client targets.
func (c *Client) Chain() Chain { return c.chain }

// URL returns the configured RPC URL (sans trailing slash).
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
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("evm rpc %d: %s", e.Code, e.Message) }

// IsNotFound reports whether the error is the chain's "no such tx /
// receipt yet" response. We rely on this for the receipt poller.
func IsNotFound(err error) bool {
	var re *rpcError
	if errors.As(err, &re) {
		return re.Code == -32601 || re.Code == -32602
	}
	return false
}

func (c *Client) call(ctx context.Context, method string, params []any, out any) error {
	id := c.id.Add(1)
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("evm: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("evm: http: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("evm: read body: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("evm: http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var r rpcResponse
	if err := json.Unmarshal(raw, &r); err != nil {
		return fmt.Errorf("evm: parse response: %w (body=%q)", err, string(raw))
	}
	if r.Error != nil {
		return r.Error
	}
	if out == nil || len(r.Result) == 0 || string(r.Result) == "null" {
		return nil
	}
	if err := json.Unmarshal(r.Result, out); err != nil {
		return fmt.Errorf("evm: parse result: %w", err)
	}
	return nil
}

// ChainID returns eth_chainId. Used to verify the configured RPC URL
// actually points at the chain we think it does.
func (c *Client) ChainID(ctx context.Context) (uint64, error) {
	var hexStr string
	if err := c.call(ctx, "eth_chainId", []any{}, &hexStr); err != nil {
		return 0, err
	}
	return hexToUint64(hexStr)
}

// BlockNumber returns the latest block height.
func (c *Client) BlockNumber(ctx context.Context) (uint64, error) {
	var hexStr string
	if err := c.call(ctx, "eth_blockNumber", []any{}, &hexStr); err != nil {
		return 0, err
	}
	return hexToUint64(hexStr)
}

// GetBalance returns the native-token balance (in wei / equivalent) for
// the given 0x address at the latest block.
func (c *Client) GetBalance(ctx context.Context, address string) (*big.Int, error) {
	var hexStr string
	if err := c.call(ctx, "eth_getBalance", []any{address, "latest"}, &hexStr); err != nil {
		return nil, err
	}
	return hexToBigInt(hexStr)
}

// GetTransactionCount returns the next nonce for address (pending). We
// use the pending count so back-to-back txs in the same tick get
// distinct nonces.
func (c *Client) GetTransactionCount(ctx context.Context, address string) (uint64, error) {
	var hexStr string
	if err := c.call(ctx, "eth_getTransactionCount", []any{address, "pending"}, &hexStr); err != nil {
		return 0, err
	}
	return hexToUint64(hexStr)
}

// GasPrice returns eth_gasPrice — used for legacy-tx chains (BSC, AVAX).
func (c *Client) GasPrice(ctx context.Context) (*big.Int, error) {
	var hexStr string
	if err := c.call(ctx, "eth_gasPrice", []any{}, &hexStr); err != nil {
		return nil, err
	}
	return hexToBigInt(hexStr)
}

// MaxPriorityFeePerGas returns eth_maxPriorityFeePerGas. EIP-1559 chains.
// Falls back to a small default if the RPC does not implement it.
func (c *Client) MaxPriorityFeePerGas(ctx context.Context) (*big.Int, error) {
	var hexStr string
	err := c.call(ctx, "eth_maxPriorityFeePerGas", []any{}, &hexStr)
	if err != nil {
		// Some RPCs don't implement this. Default to 1 gwei.
		return new(big.Int).Mul(big.NewInt(1), big.NewInt(1_000_000_000)), nil
	}
	return hexToBigInt(hexStr)
}

// FeeHistory returns the base fee from the latest block, used to compute
// maxFeePerGas as 2*baseFee + tip on EIP-1559 chains.
type FeeHistoryResult struct {
	BaseFeePerGas []string `json:"baseFeePerGas"`
}

func (c *Client) FeeHistory(ctx context.Context, blockCount uint64, newest string) (*FeeHistoryResult, error) {
	var out FeeHistoryResult
	if err := c.call(ctx, "eth_feeHistory",
		[]any{fmt.Sprintf("0x%x", blockCount), newest, []float64{}}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CallParams models eth_call's input.
type CallParams struct {
	From string // optional
	To   string
	Data string // 0x-hex calldata
}

// Call performs eth_call against the latest block. data should be 0x-hex.
func (c *Client) Call(ctx context.Context, p CallParams) ([]byte, error) {
	in := map[string]string{"to": p.To, "data": p.Data}
	if p.From != "" {
		in["from"] = p.From
	}
	var hexStr string
	if err := c.call(ctx, "eth_call", []any{in, "latest"}, &hexStr); err != nil {
		return nil, err
	}
	return hexToBytes(hexStr)
}

// EstimateGas returns eth_estimateGas for the call.
type EstimateGasParams struct {
	From  string
	To    string
	Data  string
	Value string // 0x-hex; "0x0" if zero
}

func (c *Client) EstimateGas(ctx context.Context, p EstimateGasParams) (uint64, error) {
	in := map[string]string{"from": p.From, "to": p.To, "data": p.Data}
	if p.Value != "" {
		in["value"] = p.Value
	}
	var hexStr string
	if err := c.call(ctx, "eth_estimateGas", []any{in}, &hexStr); err != nil {
		return 0, err
	}
	return hexToUint64(hexStr)
}

// SendRawTransaction broadcasts a signed transaction. raw must include
// the 0x prefix.
func (c *Client) SendRawTransaction(ctx context.Context, raw string) (string, error) {
	var hash string
	if err := c.call(ctx, "eth_sendRawTransaction", []any{raw}, &hash); err != nil {
		return "", err
	}
	return hash, nil
}

// Receipt is the subset of eth_getTransactionReceipt fields we use.
type Receipt struct {
	TransactionHash   string `json:"transactionHash"`
	BlockNumber       string `json:"blockNumber"`
	BlockHash         string `json:"blockHash"`
	GasUsed           string `json:"gasUsed"`
	EffectiveGasPrice string `json:"effectiveGasPrice"`
	Status            string `json:"status"` // "0x1" success, "0x0" failure
}

// IsSuccess reports whether the tx executed successfully.
func (r *Receipt) IsSuccess() bool { return r != nil && r.Status == "0x1" }

// GasUsedUint returns gasUsed parsed.
func (r *Receipt) GasUsedUint() (uint64, error) { return hexToUint64(r.GasUsed) }

// EffectiveGasPriceBig returns the effective gas price parsed.
func (r *Receipt) EffectiveGasPriceBig() (*big.Int, error) { return hexToBigInt(r.EffectiveGasPrice) }

// GetTransactionReceipt returns the receipt or nil if the tx is not yet
// mined. Returned error reflects a real RPC error; "not yet mined" is
// signalled by (nil, nil).
func (c *Client) GetTransactionReceipt(ctx context.Context, hash string) (*Receipt, error) {
	var r *Receipt
	if err := c.call(ctx, "eth_getTransactionReceipt", []any{hash}, &r); err != nil {
		return nil, err
	}
	return r, nil
}

// WaitForReceipt polls eth_getTransactionReceipt until the tx is mined
// or the context is cancelled or the budget elapses.
func (c *Client) WaitForReceipt(ctx context.Context, hash string, budget time.Duration, interval time.Duration) (*Receipt, error) {
	if interval == 0 {
		interval = 2 * time.Second
	}
	deadline := time.Now().Add(budget)
	tick := time.NewTicker(interval)
	defer tick.Stop()
	for {
		r, err := c.GetTransactionReceipt(ctx, hash)
		if err != nil && !IsNotFound(err) {
			return nil, err
		}
		if r != nil {
			return r, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("evm: receipt timeout for %s", hash)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-tick.C:
		}
	}
}

// ─── hex helpers ────────────────────────────────────────────────────────────

func hexToUint64(s string) (uint64, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return 0, nil
	}
	n := new(big.Int)
	if _, ok := n.SetString(s, 16); !ok {
		return 0, fmt.Errorf("evm: bad hex uint %q", s)
	}
	return n.Uint64(), nil
}

func hexToBigInt(s string) (*big.Int, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return new(big.Int), nil
	}
	n := new(big.Int)
	if _, ok := n.SetString(s, 16); !ok {
		return nil, fmt.Errorf("evm: bad hex bigint %q", s)
	}
	return n, nil
}

func hexToBytes(s string) ([]byte, error) {
	s = strings.TrimPrefix(s, "0x")
	if s == "" {
		return nil, nil
	}
	if len(s)%2 == 1 {
		s = "0" + s
	}
	return hex.DecodeString(s)
}
