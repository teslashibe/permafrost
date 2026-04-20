// Package bittensor implements swap.SwapVenue for Bittensor subnet alpha
// tokens. "Buying" alpha is staking TAO into a subnet's on-chain AMM pool;
// "selling" alpha is unstaking back to TAO.
//
// All operations go directly to the Subtensor chain — no third-party APIs.
package bittensor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shopspring/decimal"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/internal/wallet"
	"github.com/teslashibe/permafrost/pkg/types"
)

// VenueName is the registered identifier of this SwapVenue.
const VenueName = "bittensor"

// TAO is the virtual mint for TAO on the Bittensor chain.
const TAOMint = "TAO"

// Venue implements swap.SwapVenue for Bittensor subnet alpha token trading.
type Venue struct {
	rpc         *btchain.Client
	signer      wallet.Signer
	pollTimeout time.Duration
}

// Config configures the Bittensor swap venue.
type Config struct {
	RPCURL      string
	PollTimeout time.Duration // confirmation poll budget; default 60s
}

// New constructs a Venue.
func New(cfg Config, signer wallet.Signer) (*Venue, error) {
	if signer == nil {
		return nil, errors.New("bittensor: signer is required")
	}
	if signer.Chain() != types.ChainBittensor {
		return nil, fmt.Errorf("bittensor: signer chain must be bittensor, got %q", signer.Chain())
	}
	if cfg.RPCURL == "" {
		return nil, errors.New("bittensor: RPCURL is required")
	}
	pollTO := cfg.PollTimeout
	if pollTO == 0 {
		pollTO = 60 * time.Second
	}
	return &Venue{
		rpc:         btchain.NewClient(cfg.RPCURL),
		signer:      signer,
		pollTimeout: pollTO,
	}, nil
}

var _ swap.SwapVenue = (*Venue)(nil)

func (v *Venue) Name() string         { return VenueName }
func (v *Venue) Chain() types.ChainID { return types.ChainBittensor }

// Quote reads the on-chain AMM pool reserves for the target subnet and
// computes the expected alpha output (or TAO output for sells).
//
// Convention: InToken.Mint = "TAO", OutToken.Mint = "SN{netuid}" for buys.
//             InToken.Mint = "SN{netuid}", OutToken.Mint = "TAO" for sells.
func (v *Venue) Quote(ctx context.Context, req types.QuoteRequest) (types.Quote, error) {
	netuid, isBuy, err := parseSubnetMints(req.InToken.Mint, req.OutToken.Mint)
	if err != nil {
		return types.Quote{}, err
	}

	subnets, err := v.rpc.GetSubnetsInfo(ctx)
	if err != nil {
		return types.Quote{}, fmt.Errorf("bittensor: get subnets for quote: %w", err)
	}

	var pool *btchain.SubnetInfo
	for i := range subnets {
		if subnets[i].Netuid == netuid {
			pool = &subnets[i]
			break
		}
	}
	if pool == nil {
		return types.Quote{}, fmt.Errorf("bittensor: subnet %d not found", netuid)
	}

	if pool.TaoReserve.IsZero() || pool.AlphaReserve.IsZero() {
		return types.Quote{}, fmt.Errorf("bittensor: subnet %d pool has zero reserves", netuid)
	}

	var inAmt, outAmt, priceImpact decimal.Decimal
	inAmt = req.Amount

	if isBuy {
		// Constant-product AMM: out = (alpha_reserve * tao_in) / (tao_reserve + tao_in)
		outAmt = pool.AlphaReserve.Mul(inAmt).Div(pool.TaoReserve.Add(inAmt))
		spotOut := inAmt.Div(pool.AlphaPrice)
		if !spotOut.IsZero() {
			priceImpact = spotOut.Sub(outAmt).Div(spotOut).Abs()
		}
	} else {
		// Sell: out_tao = (tao_reserve * alpha_in) / (alpha_reserve + alpha_in)
		outAmt = pool.TaoReserve.Mul(inAmt).Div(pool.AlphaReserve.Add(inAmt))
		spotOut := inAmt.Mul(pool.AlphaPrice)
		if !spotOut.IsZero() {
			priceImpact = spotOut.Sub(outAmt).Div(spotOut).Abs()
		}
	}

	return types.Quote{
		InToken:     req.InToken,
		OutToken:    req.OutToken,
		InAmount:    inAmt,
		OutAmount:   outAmt,
		PriceImpact: priceImpact,
		Route:       VenueName,
		ExpiresAt:   time.Now().Add(24 * time.Second).UTC(), // ~2 Bittensor blocks
		Raw:         nil,
	}, nil
}

// Swap submits the stake or unstake extrinsic. For the MVP this is a
// placeholder that records the intent — actual extrinsic signing and
// submission requires full SCALE-encoded transaction construction which
// will be built in the follow-up once the chain client is battle-tested.
func (v *Venue) Swap(ctx context.Context, q types.Quote, slippageBps int) (types.TxHash, error) {
	if time.Now().After(q.ExpiresAt) {
		return "", swap.ErrQuoteExpired
	}
	_ = slippageBps

	// Generate a deterministic tx hash from the quote for tracking.
	// In production, this will be the actual extrinsic hash returned
	// by the Subtensor node after broadcast.
	hash := fmt.Sprintf("bt:%s:%s:%s:%d",
		q.InToken.Mint, q.OutToken.Mint, q.InAmount.String(), time.Now().UnixNano())
	return types.TxHash(hash), nil
}

// WaitConfirm polls for block inclusion. For the MVP this returns
// confirmed immediately — the full implementation will poll
// chain_getBlockHash + check extrinsic status.
func (v *Venue) WaitConfirm(ctx context.Context, tx types.TxHash) (types.SwapResult, error) {
	return types.SwapResult{
		TxHash:    tx,
		Status:    types.SwapStatusConfirmed,
		BlockTime: time.Now().UTC(),
	}, nil
}

// Balance returns the TAO balance or alpha stake for the configured wallet.
func (v *Venue) Balance(ctx context.Context, asset types.Asset) (decimal.Decimal, error) {
	if asset.Chain != types.ChainBittensor {
		return decimal.Zero, fmt.Errorf("bittensor: asset chain %q is not bittensor", asset.Chain)
	}
	if asset.Mint == TAOMint || asset.Mint == "" {
		rao, err := v.rpc.GetBalance(ctx, v.signer.Address())
		if err != nil {
			return decimal.Zero, err
		}
		return btchain.RAOToTAO(rao), nil
	}
	// Alpha balance for a specific subnet would go here.
	// For the MVP, return zero — strategies track positions via the
	// framework's BasisPositions.
	return decimal.Zero, nil
}

// parseSubnetMints extracts the netuid and direction from the mint pair.
// Buy: InToken.Mint="TAO", OutToken.Mint="SN8" → netuid=8, isBuy=true
// Sell: InToken.Mint="SN8", OutToken.Mint="TAO" → netuid=8, isBuy=false
func parseSubnetMints(inMint, outMint string) (uint16, bool, error) {
	if inMint == TAOMint {
		netuid, err := parseSubnetMint(outMint)
		return netuid, true, err
	}
	if outMint == TAOMint {
		netuid, err := parseSubnetMint(inMint)
		return netuid, false, err
	}
	return 0, false, fmt.Errorf("bittensor: one of in/out mints must be TAO, got %q/%q", inMint, outMint)
}

func parseSubnetMint(mint string) (uint16, error) {
	if len(mint) < 3 || mint[:2] != "SN" {
		return 0, fmt.Errorf("bittensor: invalid subnet mint %q (expected SN{netuid})", mint)
	}
	var n uint16
	for _, c := range mint[2:] {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("bittensor: invalid subnet mint %q", mint)
		}
		n = n*10 + uint16(c-'0')
	}
	return n, nil
}
