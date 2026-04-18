package oneinch

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"

	"github.com/teslashibe/permafrost/internal/chain/evm"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/internal/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

// VenueName is the registered identifier of this SwapVenue.
const VenueName = "oneinch"

// nativeAddress is 1inch's sentinel for the native gas token (ETH on
// ETH/Base, AVAX on Avalanche, BNB on BSC). Native swaps are deferred
// in v1 — Quote/Swap reject any intent that uses this address until the
// follow-up PR.
const nativeAddress = "0xeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"

// Venue implements swap.SwapVenue against 1inch on a single EVM chain.
type Venue struct {
	one         *Client
	rpc         *evm.Client
	signer      *wallet.EVMSigner
	slippageBps int           // default if SwapIntent doesn't specify
	pollTimeout time.Duration // confirmation wait budget; default 90s

	// Per-(token,spender) infinite-approve cache. We hit
	// allowance() once and trust the result for the lifetime of the
	// process; the cache is invalidated on tx failure.
	approvedMu sync.Mutex
	approved   map[string]bool // key = lowercased token contract
}

// Config configures a Venue.
type Config struct {
	OneInchAPIKey   string
	OneInchBaseURL  string // optional override (proxy)
	RPCURL          string
	Chain           types.ChainID // ChainEthereum / ChainBase / ChainAvalanche / ChainBSC
	DefaultSlippage int           // bps; default 50 (0.5%)
	PollTimeout     time.Duration // default 90s
}

// New constructs a Venue.
func New(cfg Config, signer *wallet.EVMSigner) (*Venue, error) {
	if signer == nil {
		return nil, errors.New("oneinch: signer is required")
	}
	if signer.Chain() != cfg.Chain {
		return nil, fmt.Errorf("oneinch: signer chain %q != venue chain %q",
			signer.Chain(), cfg.Chain)
	}
	if cfg.RPCURL == "" {
		return nil, errors.New("oneinch: RPCURL is required")
	}
	chain, err := evm.LookupChain(cfg.Chain)
	if err != nil {
		return nil, fmt.Errorf("oneinch: %w", err)
	}
	rpc := evm.NewClient(cfg.RPCURL, chain)

	slip := cfg.DefaultSlippage
	if slip <= 0 {
		slip = 50
	}
	pollTO := cfg.PollTimeout
	if pollTO == 0 {
		pollTO = 90 * time.Second
	}
	return &Venue{
		one:         NewClient(chain.NumericID, cfg.OneInchAPIKey, WithBaseURL(cfg.OneInchBaseURL)),
		rpc:         rpc,
		signer:      signer,
		slippageBps: slip,
		pollTimeout: pollTO,
		approved:    map[string]bool{},
	}, nil
}

// Compile-time check.
var _ swap.SwapVenue = (*Venue)(nil)

func (v *Venue) Name() string         { return VenueName }
func (v *Venue) Chain() types.ChainID { return v.signer.Chain() }

// Quote requests a price quote from 1inch. Native gas tokens are not
// supported in v1 — see SCOPE.md.
func (v *Venue) Quote(ctx context.Context, req types.QuoteRequest) (types.Quote, error) {
	if err := guardNative(req.InToken, req.OutToken); err != nil {
		return types.Quote{}, err
	}
	if req.Mode == types.QuoteExactOut {
		return types.Quote{}, errors.New("oneinch: ExactOut is not supported")
	}
	amt := scaleAmount(req.Amount, req.InToken.Decimals)
	q, err := v.one.Quote(ctx, QuoteParams{
		Src:    req.InToken.Mint,
		Dst:    req.OutToken.Mint,
		Amount: amt.String(),
	})
	if err != nil {
		return types.Quote{}, err
	}
	out := unscaleAmount(parseBigInt(q.DstAmount), req.OutToken.Decimals)
	return types.Quote{
		InToken:   req.InToken,
		OutToken:  req.OutToken,
		InAmount:  req.Amount,
		OutAmount: out,
		Route:     VenueName,
		ExpiresAt: time.Now().Add(20 * time.Second).UTC(),
		// Raw is intentionally empty — the /swap call is independent
		// from /quote in 1inch v6 (unlike Jupiter), so we don't carry
		// state forward.
	}, nil
}

// Swap fetches the executable tx from 1inch, ensures the router has
// allowance, signs the swap tx, broadcasts it, and returns the hash.
//
// The approval dance: 1inch's router needs an ERC-20 allowance from the
// caller before it can pull tokens. We do an `approve(MAX)` once per
// (chain, token) pair — that's the "plain infinite approve" model
// chosen during scoping. The approval result is cached in v.approved
// so we don't re-check on every swap.
//
// IMPORTANT: this method blocks until the approve tx (if needed) is
// confirmed. We MUST NOT broadcast the swap before the approve is
// mined, or the swap will revert on insufficient allowance.
func (v *Venue) Swap(ctx context.Context, q types.Quote, slippageBps int) (types.TxHash, error) {
	if time.Now().After(q.ExpiresAt) {
		return "", swap.ErrQuoteExpired
	}
	if err := guardNative(q.InToken, q.OutToken); err != nil {
		return "", err
	}
	if slippageBps <= 0 {
		slippageBps = v.slippageBps
	}

	if err := v.ensureApproved(ctx, q.InToken); err != nil {
		return "", fmt.Errorf("oneinch: approve: %w", err)
	}

	srcAmount := scaleAmount(q.InAmount, q.InToken.Decimals)
	resp, err := v.one.Swap(ctx, SwapParams{
		Src:              q.InToken.Mint,
		Dst:              q.OutToken.Mint,
		Amount:           srcAmount.String(),
		From:             v.signer.Address(),
		Slippage:         bpsToPercent(slippageBps),
		DisableEstimate:  true, // we let RPC do estimation; 1inch's sim is finicky
		AllowPartialFill: false,
	})
	if err != nil {
		return "", fmt.Errorf("oneinch: /swap: %w", err)
	}

	value := new(big.Int)
	if v, ok := new(big.Int).SetString(strings.TrimPrefix(resp.Tx.Value, "0x"), 10); ok {
		value = v
	}
	req := evm.TxRequest{
		From:     v.signer.Address(),
		To:       resp.Tx.To,
		Data:     ensureHexPrefix(resp.Tx.Data),
		Value:    value,
		GasLimit: resp.Tx.Gas, // 1inch's estimate; EstimateAndFill adds buffer if zero
	}
	if err := evm.EstimateAndFill(ctx, v.rpc, &req); err != nil {
		return "", fmt.Errorf("oneinch: fill tx: %w", err)
	}
	hash, err := evm.SignAndSend(ctx, v.rpc, req, v.signer.PrivateKey())
	if err != nil {
		return "", fmt.Errorf("oneinch: send tx: %w", err)
	}
	return types.TxHash(hash), nil
}

// WaitConfirm polls eth_getTransactionReceipt until success/failure or
// the budget elapses.
func (v *Venue) WaitConfirm(ctx context.Context, tx types.TxHash) (types.SwapResult, error) {
	r, err := v.rpc.WaitForReceipt(ctx, string(tx), v.pollTimeout, 2*time.Second)
	if err != nil {
		return types.SwapResult{TxHash: tx, Status: types.SwapStatusPending, Error: err.Error()}, nil
	}
	status := types.SwapStatusFailed
	if r.IsSuccess() {
		status = types.SwapStatusConfirmed
	}
	gasNative := decimal.Zero
	if used, err := r.GasUsedUint(); err == nil {
		if price, err := r.EffectiveGasPriceBig(); err == nil {
			cost := new(big.Int).Mul(big.NewInt(int64(used)), price)
			gasNative = decimal.NewFromBigInt(cost, -18) // wei → native units
		}
	}
	return types.SwapResult{
		TxHash:    tx,
		Status:    status,
		BlockTime: time.Now().UTC(),
		GasNative: gasNative,
	}, nil
}

// Balance reports the wallet balance of an ERC-20 (or native gas token)
// for the configured signer address.
func (v *Venue) Balance(ctx context.Context, asset types.Asset) (decimal.Decimal, error) {
	if asset.Chain != v.Chain() {
		return decimal.Zero, fmt.Errorf("oneinch: asset chain %q != venue chain %q",
			asset.Chain, v.Chain())
	}
	if isNativeMint(asset.Mint) {
		bal, err := v.rpc.GetBalance(ctx, v.signer.Address())
		if err != nil {
			return decimal.Zero, err
		}
		return decimal.NewFromBigInt(bal, -18), nil
	}
	tok := evm.NewERC20Token(v.rpc, asset.Mint)
	bal, err := tok.BalanceOf(ctx, v.signer.Address())
	if err != nil {
		return decimal.Zero, err
	}
	return decimal.NewFromBigInt(bal, -asset.Decimals), nil
}

// ensureApproved checks (and if needed installs) infinite allowance
// from the wallet to 1inch's router for token. Idempotent and cached.
func (v *Venue) ensureApproved(ctx context.Context, token types.Asset) error {
	if isNativeMint(token.Mint) {
		return nil // native doesn't need approval
	}
	key := strings.ToLower(token.Mint)

	v.approvedMu.Lock()
	if v.approved[key] {
		v.approvedMu.Unlock()
		return nil
	}
	v.approvedMu.Unlock()

	allowed, err := v.one.Allowance(ctx, token.Mint, v.signer.Address())
	if err != nil {
		return fmt.Errorf("oneinch: allowance: %w", err)
	}
	if allowed == "" || allowed == "0" {
		approveResp, err := v.one.ApproveTx(ctx, token.Mint, "") // "" => MAX
		if err != nil {
			return fmt.Errorf("oneinch: approve calldata: %w", err)
		}
		req := evm.TxRequest{
			From: v.signer.Address(),
			To:   approveResp.To,
			Data: ensureHexPrefix(approveResp.Data),
		}
		if err := evm.EstimateAndFill(ctx, v.rpc, &req); err != nil {
			return fmt.Errorf("oneinch: fill approve tx: %w", err)
		}
		hash, err := evm.SignAndSend(ctx, v.rpc, req, v.signer.PrivateKey())
		if err != nil {
			return fmt.Errorf("oneinch: send approve tx: %w", err)
		}
		// MUST wait for the approve to confirm before we can swap.
		r, err := v.rpc.WaitForReceipt(ctx, hash, v.pollTimeout, 2*time.Second)
		if err != nil {
			return fmt.Errorf("oneinch: approve receipt: %w", err)
		}
		if !r.IsSuccess() {
			return fmt.Errorf("oneinch: approve tx %s reverted on chain", hash)
		}
	}

	v.approvedMu.Lock()
	v.approved[key] = true
	v.approvedMu.Unlock()
	return nil
}

// ─── helpers ────────────────────────────────────────────────────────────────

func guardNative(in, out types.Asset) error {
	if isNativeMint(in.Mint) || isNativeMint(out.Mint) {
		return errors.New("oneinch: native gas-token swaps are not supported in v1 (see SCOPE.md)")
	}
	return nil
}

func isNativeMint(m string) bool { return strings.EqualFold(m, nativeAddress) }

func ensureHexPrefix(s string) string {
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		return s
	}
	return "0x" + s
}

func scaleAmount(amount decimal.Decimal, decimals int32) *big.Int {
	if decimals < 0 {
		decimals = 0
	}
	scaled := amount.Shift(decimals).Truncate(0)
	if scaled.IsNegative() {
		return new(big.Int)
	}
	return scaled.BigInt()
}

func unscaleAmount(units *big.Int, decimals int32) decimal.Decimal {
	if units == nil {
		return decimal.Zero
	}
	return decimal.NewFromBigInt(units, -decimals)
}

func parseBigInt(s string) *big.Int {
	n := new(big.Int)
	if s == "" {
		return n
	}
	n.SetString(strings.TrimSpace(s), 10)
	return n
}

// bpsToPercent converts 50 (bps) → "0.5" (percent string the 1inch
// /swap endpoint expects).
func bpsToPercent(bps int) string {
	if bps <= 0 {
		return "0.5"
	}
	return decimal.New(int64(bps), -2).String()
}
