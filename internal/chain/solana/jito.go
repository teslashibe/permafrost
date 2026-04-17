package solana

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// JitoClient submits bundles to the Jito Block Engine. URLs follow the
// pattern https://<region>.mainnet.block-engine.jito.wtf/api/v1/bundles
// (e.g. https://mainnet.block-engine.jito.wtf/api/v1/bundles).
type JitoClient struct {
	url  string
	http *http.Client
}

// NewJitoClient constructs a JitoClient.
func NewJitoClient(url string) *JitoClient {
	return &JitoClient{
		url:  strings.TrimRight(url, "/"),
		http: &http.Client{Timeout: 15 * time.Second},
	}
}

// SendBundle submits up to 5 base64-encoded transactions as a single bundle
// and returns the bundle UUID.
func (c *JitoClient) SendBundle(ctx context.Context, b64Txs []string) (string, error) {
	if len(b64Txs) == 0 {
		return "", errors.New("jito: empty bundle")
	}
	if len(b64Txs) > 5 {
		return "", errors.New("jito: bundle exceeds 5 transactions")
	}
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "sendBundle",
		"params":  []any{b64Txs, map[string]any{"encoding": "base64"}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("jito: do: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return "", fmt.Errorf("jito: %d: %s", resp.StatusCode, truncate(raw))
	}
	var out struct {
		Result string    `json:"result"`
		Error  *rpcError `json:"error,omitempty"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("jito: decode: %w", err)
	}
	if out.Error != nil {
		return "", out.Error
	}
	return out.Result, nil
}
