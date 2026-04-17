// Package solana wraps the minimal Solana JSON-RPC surface that Permafrost
// needs to execute swaps via Jupiter and confirm transactions on-chain.
//
// We deliberately avoid pulling the full gagliardetto/solana-go RPC client
// here so this package stays a thin shim around HTTP. Transaction encoding
// uses gagliardetto/solana-go which is a hard dependency of the swap
// adapter (see internal/swap/jupiter).
package solana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

// Commitment levels supported by getSignatureStatuses & sendTransaction.
type Commitment string

const (
	CommitmentProcessed Commitment = "processed"
	CommitmentConfirmed Commitment = "confirmed"
	CommitmentFinalized Commitment = "finalized"
)

// Client is a JSON-RPC client over HTTP. Safe for concurrent use.
type Client struct {
	url  string
	http *http.Client
	id   atomic.Uint64
}

// NewClient constructs a Client. URL must be the full RPC endpoint
// (e.g. https://api.mainnet-beta.solana.com or your Helius URL).
func NewClient(url string) *Client {
	return &Client{
		url:  strings.TrimRight(url, "/"),
		http: &http.Client{Timeout: 30 * time.Second},
	}
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      uint64 `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
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

func (e *rpcError) Error() string { return fmt.Sprintf("solana rpc %d: %s", e.Code, e.Message) }

func (c *Client) call(ctx context.Context, method string, params any, out any) error {
	id := c.id.Add(1)
	body, err := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return fmt.Errorf("solana: marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("solana: do: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("solana: %s %d: %s", method, resp.StatusCode, truncate(raw))
	}
	var env rpcResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("solana: decode: %w", err)
	}
	if env.Error != nil {
		return env.Error
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("solana: decode result: %w", err)
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

// ─── methods ────────────────────────────────────────────────────────────────

// GetBalance returns the SOL balance in lamports for the given base58 address.
func (c *Client) GetBalance(ctx context.Context, address string) (uint64, error) {
	var resp struct {
		Value uint64 `json:"value"`
	}
	if err := c.call(ctx, "getBalance", []any{address, map[string]any{"commitment": "confirmed"}}, &resp); err != nil {
		return 0, err
	}
	return resp.Value, nil
}

// TokenAccountBalance is the response shape for getTokenAccountBalance.
type TokenAccountBalance struct {
	Amount         string `json:"amount"`
	Decimals       int    `json:"decimals"`
	UIAmountString string `json:"uiAmountString"`
}

// GetTokenAccountsByOwner returns SPL token accounts owned by the given
// address that hold the given mint. Returns []token{address, balance}.
func (c *Client) GetTokenAccountsByOwner(ctx context.Context, owner, mint string) ([]TokenAccountInfo, error) {
	var resp struct {
		Value []struct {
			Pubkey  string `json:"pubkey"`
			Account struct {
				Data struct {
					Parsed struct {
						Info struct {
							TokenAmount TokenAccountBalance `json:"tokenAmount"`
							Mint        string              `json:"mint"`
							Owner       string              `json:"owner"`
						} `json:"info"`
					} `json:"parsed"`
				} `json:"data"`
			} `json:"account"`
		} `json:"value"`
	}
	params := []any{
		owner,
		map[string]any{"mint": mint},
		map[string]any{"encoding": "jsonParsed", "commitment": "confirmed"},
	}
	if err := c.call(ctx, "getTokenAccountsByOwner", params, &resp); err != nil {
		return nil, err
	}
	out := make([]TokenAccountInfo, 0, len(resp.Value))
	for _, v := range resp.Value {
		out = append(out, TokenAccountInfo{
			Pubkey:  v.Pubkey,
			Mint:    v.Account.Data.Parsed.Info.Mint,
			Balance: v.Account.Data.Parsed.Info.TokenAmount,
		})
	}
	return out, nil
}

// TokenAccountInfo is one SPL token account associated with a wallet.
type TokenAccountInfo struct {
	Pubkey  string
	Mint    string
	Balance TokenAccountBalance
}

// SendTransaction submits a base64-encoded signed transaction and returns
// the signature.
func (c *Client) SendTransaction(ctx context.Context, b64tx string, skipPreflight bool) (string, error) {
	params := []any{
		b64tx,
		map[string]any{
			"encoding":      "base64",
			"skipPreflight": skipPreflight,
			"maxRetries":    0,
		},
	}
	var sig string
	if err := c.call(ctx, "sendTransaction", params, &sig); err != nil {
		return "", err
	}
	return sig, nil
}

// SignatureStatus is the per-signature status returned by getSignatureStatuses.
type SignatureStatus struct {
	Slot               uint64          `json:"slot"`
	Confirmations      *uint64         `json:"confirmations,omitempty"`
	ConfirmationStatus string          `json:"confirmationStatus,omitempty"` // processed | confirmed | finalized
	Err                json.RawMessage `json:"err,omitempty"`
}

// GetSignatureStatuses returns the per-signature status (or nil if unknown).
func (c *Client) GetSignatureStatuses(ctx context.Context, sigs []string, searchHistory bool) ([]*SignatureStatus, error) {
	var resp struct {
		Value []*SignatureStatus `json:"value"`
	}
	params := []any{sigs, map[string]any{"searchTransactionHistory": searchHistory}}
	if err := c.call(ctx, "getSignatureStatuses", params, &resp); err != nil {
		return nil, err
	}
	return resp.Value, nil
}

// GetLatestBlockhash returns the latest blockhash and last-valid-block-height.
func (c *Client) GetLatestBlockhash(ctx context.Context) (string, uint64, error) {
	var resp struct {
		Value struct {
			Blockhash            string `json:"blockhash"`
			LastValidBlockHeight uint64 `json:"lastValidBlockHeight"`
		} `json:"value"`
	}
	params := []any{map[string]any{"commitment": "confirmed"}}
	if err := c.call(ctx, "getLatestBlockhash", params, &resp); err != nil {
		return "", 0, err
	}
	return resp.Value.Blockhash, resp.Value.LastValidBlockHeight, nil
}
