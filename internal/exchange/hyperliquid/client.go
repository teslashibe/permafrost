package hyperliquid

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client is a thin POST-JSON client over the Hyperliquid REST API. It is
// safe for concurrent use.
type Client struct {
	endpoints Endpoints
	http      *http.Client
}

// NewClient constructs a Client with sensible defaults (15s timeout, JSON
// content type, no retries — callers should layer their own retry policy).
func NewClient(ep Endpoints) *Client {
	return &Client{
		endpoints: ep,
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// post issues a POST with a JSON body and decodes the response into out.
func (c *Client) post(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("hyperliquid: marshal body: %w", err)
	}
	url := c.endpoints.REST + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("hyperliquid: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hyperliquid: do request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("hyperliquid: %s -> %d: %s", url, resp.StatusCode, string(raw))
	}
	if out == nil {
		return nil
	}
	dec := json.NewDecoder(resp.Body)
	dec.UseNumber()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("hyperliquid: decode response: %w", err)
	}
	return nil
}

// info posts an info request and decodes the response.
func (c *Client) info(ctx context.Context, req infoRequest, out any) error {
	return c.post(ctx, "/info", req, out)
}
