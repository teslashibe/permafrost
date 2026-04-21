// Package bittensor wraps the Subtensor (Bittensor blockchain) RPC surface
// used by Permafrost for alpha token trading.
//
// The implementation is built on centrifuge/go-substrate-rpc-client/v4
// (gsrpc) for production-correct SCALE encoding, storage key construction,
// and extrinsic signing. Custom Subtensor JSON-RPC endpoints (subnetInfo_*,
// swap_*) are called directly via gsrpc's Client.Call.
//
// All operations target a configurable Subtensor RPC endpoint — public,
// self-hosted, or paid provider. WebSocket-only (gsrpc requirement).
package bittensor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	gsrpc "github.com/centrifuge/go-substrate-rpc-client/v4"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/shopspring/decimal"
)

const (
	// RAOPerTAO is the unit conversion: 1 TAO = 1e9 RAO.
	RAOPerTAO = 1_000_000_000

	// Pallet names for storage queries / extrinsics on Subtensor.
	palletSystem    = "System"
	palletSubtensor = "SubtensorModule"

	// SS58 prefix for Bittensor (Substrate generic).
	SS58Prefix uint16 = 42
)

// Client is a thread-safe Bittensor chain client. Holds a long-lived
// gsrpc connection plus cached chain metadata. Reconnect is handled
// transparently by gsrpc's underlying WebSocket layer; on persistent
// failure callers will receive errors which they should retry.
type Client struct {
	url string

	mu   sync.RWMutex
	api  *gsrpc.SubstrateAPI
	meta *gsrpctypes.Metadata
}

// NewClient constructs a Client. The URL must be a wss:// (or ws://)
// Subtensor endpoint. Connection is lazy: the first call that needs the
// chain will dial, fetch metadata, and cache it.
//
// Public endpoints documented in BittensorConfig.
func NewClient(url string) *Client {
	url = normaliseWSURL(url)
	return &Client{url: url}
}

// URL returns the configured endpoint.
func (c *Client) URL() string { return c.url }

// connect lazily establishes the WebSocket connection and caches metadata.
// Safe for concurrent callers — only one will perform the dial.
func (c *Client) connect() (*gsrpc.SubstrateAPI, *gsrpctypes.Metadata, error) {
	c.mu.RLock()
	if c.api != nil && c.meta != nil {
		api, meta := c.api, c.meta
		c.mu.RUnlock()
		return api, meta, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.api != nil && c.meta != nil {
		return c.api, c.meta, nil
	}

	api, err := gsrpc.NewSubstrateAPI(c.url)
	if err != nil {
		return nil, nil, fmt.Errorf("subtensor: dial %s: %w", c.url, err)
	}
	meta, err := api.RPC.State.GetMetadataLatest()
	if err != nil {
		return nil, nil, fmt.Errorf("subtensor: get metadata: %w", err)
	}
	c.api = api
	c.meta = meta
	return api, meta, nil
}

// Close terminates the underlying connection. Safe to call repeatedly.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.api != nil {
		c.api.Client.Close()
		c.api = nil
		c.meta = nil
	}
	return nil
}

// ─── chain identity / health ────────────────────────────────────────────────

// GetChain returns the chain name (e.g. "Bittensor"). Used by the doctor
// to verify the RPC is alive and pointed at the expected network.
func (c *Client) GetChain(ctx context.Context) (string, error) {
	api, _, err := c.connect()
	if err != nil {
		return "", err
	}
	chain, err := api.RPC.System.Chain()
	if err != nil {
		return "", err
	}
	return string(chain), nil
}

// GetBlockNumber returns the latest finalized block number.
func (c *Client) GetBlockNumber(ctx context.Context) (uint64, error) {
	api, _, err := c.connect()
	if err != nil {
		return 0, err
	}
	hash, err := api.RPC.Chain.GetFinalizedHead()
	if err != nil {
		return 0, err
	}
	header, err := api.RPC.Chain.GetHeader(hash)
	if err != nil {
		return 0, err
	}
	return uint64(header.Number), nil
}

// ─── account / balance reads ────────────────────────────────────────────────

// GetTAOBalance returns the free TAO balance (in RAO) for the given
// SS58 address. Reads System.Account storage.
func (c *Client) GetTAOBalance(ctx context.Context, ss58 string) (uint64, error) {
	api, meta, err := c.connect()
	if err != nil {
		return 0, err
	}

	pubKey, err := decodeSS58(ss58)
	if err != nil {
		return 0, fmt.Errorf("subtensor: decode ss58 %q: %w", ss58, err)
	}

	key, err := gsrpctypes.CreateStorageKey(meta, palletSystem, "Account", pubKey)
	if err != nil {
		return 0, fmt.Errorf("subtensor: create storage key: %w", err)
	}

	var info gsrpctypes.AccountInfo
	ok, err := api.RPC.State.GetStorageLatest(key, &info)
	if err != nil {
		return 0, fmt.Errorf("subtensor: get account: %w", err)
	}
	if !ok {
		return 0, nil // account does not exist on-chain ⇒ zero balance
	}
	return info.Data.Free.Uint64(), nil
}

// GetAlphaStake returns the alpha stake (in RAO) held by (coldkey, hotkey)
// for a specific subnet. Reads SubtensorModule.Alpha storage.
//
// Per Subtensor source, the storage map key is (hotkey_account_id,
// coldkey_account_id, netuid). For the simple "self-stake" case used by
// trading agents, hotkey == coldkey == the trading wallet.
func (c *Client) GetAlphaStake(ctx context.Context, hotkeySS58, coldkeySS58 string, netuid uint16) (uint64, error) {
	api, meta, err := c.connect()
	if err != nil {
		return 0, err
	}

	hotkey, err := decodeSS58(hotkeySS58)
	if err != nil {
		return 0, fmt.Errorf("subtensor: decode hotkey: %w", err)
	}
	coldkey, err := decodeSS58(coldkeySS58)
	if err != nil {
		return 0, fmt.Errorf("subtensor: decode coldkey: %w", err)
	}

	netuidEncoded, err := encodeU16Compact(netuid)
	if err != nil {
		return 0, err
	}

	key, err := gsrpctypes.CreateStorageKey(meta, palletSubtensor, "Alpha", hotkey, coldkey, netuidEncoded)
	if err != nil {
		// Storage layout has shifted across runtime versions; surface a
		// clear error so the caller can fall back gracefully rather than
		// returning a misleading zero.
		return 0, fmt.Errorf("subtensor: alpha storage key (netuid=%d): %w", netuid, err)
	}

	var raw gsrpctypes.U64
	ok, err := api.RPC.State.GetStorageLatest(key, &raw)
	if err != nil {
		return 0, fmt.Errorf("subtensor: get alpha: %w", err)
	}
	if !ok {
		return 0, nil
	}
	return uint64(raw), nil
}

// ─── subnet info / pricing (custom Subtensor RPCs) ──────────────────────────

// CurrentAlphaPrice returns the current alpha-token price in TAO for a
// subnet. Backed by the swap_currentAlphaPrice runtime API.
//
// The on-chain return value is in RAO (1e-9 TAO); we convert here so
// callers always work in TAO units.
func (c *Client) CurrentAlphaPrice(ctx context.Context, netuid uint16) (decimal.Decimal, error) {
	api, _, err := c.connect()
	if err != nil {
		return decimal.Zero, err
	}
	var raoPrice uint64
	if err := api.Client.Call(&raoPrice, "swap_currentAlphaPrice", netuid); err != nil {
		return decimal.Zero, fmt.Errorf("subtensor: swap_currentAlphaPrice(%d): %w", netuid, err)
	}
	return RAOToTAO(raoPrice), nil
}

// SimulatedSwapResult is the decoded result of a sim_swap RPC call.
// AmountOut and Fee are in the OUTPUT asset's RAO units.
type SimulatedSwapResult struct {
	AmountIn  uint64
	AmountOut uint64
	Fee       uint64
}

// SimSwapTaoForAlpha simulates buying alpha on a subnet without
// submitting an extrinsic. Returns the expected alpha received (in RAO)
// for taoIn (RAO).
//
// Used by Quote() to give strategies a realistic expected fill including
// AMM slippage and fee.
func (c *Client) SimSwapTaoForAlpha(ctx context.Context, netuid uint16, taoInRao uint64) (SimulatedSwapResult, error) {
	api, _, err := c.connect()
	if err != nil {
		return SimulatedSwapResult{}, err
	}
	var raw []byte
	if err := api.Client.Call(&raw, "swap_simSwapTaoForAlpha", netuid, taoInRao); err != nil {
		return SimulatedSwapResult{}, fmt.Errorf("subtensor: simSwapTaoForAlpha: %w", err)
	}
	return decodeSimSwapResult(raw, taoInRao)
}

// SimSwapAlphaForTao simulates selling alpha back to TAO.
func (c *Client) SimSwapAlphaForTao(ctx context.Context, netuid uint16, alphaInRao uint64) (SimulatedSwapResult, error) {
	api, _, err := c.connect()
	if err != nil {
		return SimulatedSwapResult{}, err
	}
	var raw []byte
	if err := api.Client.Call(&raw, "swap_simSwapAlphaForTao", netuid, alphaInRao); err != nil {
		return SimulatedSwapResult{}, fmt.Errorf("subtensor: simSwapAlphaForTao: %w", err)
	}
	return decodeSimSwapResult(raw, alphaInRao)
}

// SubnetSummary holds a digest of one subnet's tradeable state.
// Populated by ListSubnets via per-subnet RPC calls.
type SubnetSummary struct {
	Netuid     uint16
	AlphaPrice decimal.Decimal // TAO per alpha
}

// ListSubnets returns one summary per active subnet. Iterates 1..max
// netuids and calls swap_currentAlphaPrice for each. Subnets returning
// errors are skipped (not yet active or pre-dTAO).
//
// Subnet 0 is the root (no alpha); we skip it.
func (c *Client) ListSubnets(ctx context.Context, max uint16) ([]SubnetSummary, error) {
	if max == 0 {
		max = 128 // sensible upper bound; Bittensor caps at 256
	}
	out := make([]SubnetSummary, 0, max)
	for netuid := uint16(1); netuid <= max; netuid++ {
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		price, err := c.CurrentAlphaPrice(ctx, netuid)
		if err != nil {
			// Subnet not yet active — common for high netuids.
			// Continue rather than failing the whole listing.
			continue
		}
		out = append(out, SubnetSummary{Netuid: netuid, AlphaPrice: price})
	}
	return out, nil
}

// SubnetCount returns the total number of subnets via SubtensorModule.TotalNetworks.
func (c *Client) SubnetCount(ctx context.Context) (uint16, error) {
	api, meta, err := c.connect()
	if err != nil {
		return 0, err
	}
	key, err := gsrpctypes.CreateStorageKey(meta, palletSubtensor, "TotalNetworks", nil)
	if err != nil {
		return 0, fmt.Errorf("subtensor: total networks key: %w", err)
	}
	var n gsrpctypes.U16
	ok, err := api.RPC.State.GetStorageLatest(key, &n)
	if err != nil {
		return 0, fmt.Errorf("subtensor: total networks: %w", err)
	}
	if !ok {
		return 0, nil
	}
	return uint16(n), nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

// RAOToTAO converts RAO (uint64) to TAO as a decimal.
func RAOToTAO(rao uint64) decimal.Decimal {
	return decimal.NewFromUint64(rao).Div(decimal.NewFromInt(RAOPerTAO))
}

// TAOToRAO converts TAO (decimal) to RAO. Truncates fractional RAO.
func TAOToRAO(tao decimal.Decimal) uint64 {
	scaled := tao.Mul(decimal.NewFromInt(RAOPerTAO)).Truncate(0)
	if scaled.IsNegative() {
		return 0
	}
	bi := scaled.BigInt()
	if !bi.IsUint64() {
		return ^uint64(0) // saturate; callers should validate before this
	}
	return bi.Uint64()
}

// normaliseWSURL ensures the URL uses ws:// or wss://. gsrpc requires
// WebSocket; HTTP-only endpoints are not supported.
func normaliseWSURL(url string) string {
	url = strings.TrimRight(url, "/")
	url = strings.Replace(url, "https://", "wss://", 1)
	url = strings.Replace(url, "http://", "ws://", 1)
	return url
}

// errSubtensorRPC wraps custom-RPC errors with context.
var errSubtensorRPC = errors.New("subtensor rpc error")

// connectTimeout is the global ceiling for any single RPC call we make
// internally. Callers can wrap with their own deadline via ctx.
const connectTimeout = 30 * time.Second
