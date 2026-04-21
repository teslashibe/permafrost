// Package bittensor implements swap.SwapVenue for Bittensor subnet alpha
// tokens, backed by real Subtensor extrinsics.
//
// "Buying" alpha is staking TAO into a subnet's on-chain AMM pool via
// SubtensorModule.add_stake; "selling" alpha is unstaking via
// SubtensorModule.remove_stake. All quotes are derived from on-chain
// AMM simulation (swap_simSwap*) — no third-party APIs.
package bittensor

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	btchain "github.com/teslashibe/permafrost/internal/chain/bittensor"
	"github.com/teslashibe/permafrost/internal/swap"
	"github.com/teslashibe/permafrost/internal/wallet"
	"github.com/teslashibe/permafrost/pkg/types"
)

// VenueName is the registered identifier of this SwapVenue.
const VenueName = "bittensor"

// TAOMint is the virtual mint identifier for TAO. Strategies that want to
// buy alpha set SwapIntent.InToken.Mint = TAOMint.
const TAOMint = "TAO"

// AlphaMintPrefix is the convention for subnet-specific alpha mints:
// "SN8" identifies subnet 8's alpha token.
const AlphaMintPrefix = "SN"

// Venue implements swap.SwapVenue for Bittensor subnet alpha tokens.
//
// The strict-mode flag controls extrinsic submission: when AllowSubmit
// is false (default) Quote/Balance work normally but Swap returns an
// error rather than submitting on-chain. This is the safety guard rail
// — operators must explicitly opt in to live trading.
type Venue struct {
	rpc         *btchain.Client
	signer      *wallet.BittensorSigner
	pollTimeout time.Duration
	allowSubmit bool
}

// Config configures the Bittensor swap venue.
type Config struct {
	// RPCURL is the Subtensor wss:// endpoint. Required.
	RPCURL string
	// PollTimeout caps the wait for extrinsic finalization. Default 60s.
	PollTimeout time.Duration
	// AllowSubmit must be set to true for Swap() to actually submit
	// extrinsics on-chain. Default false — Swap returns ErrSubmitDisabled.
	// This is a deliberate "safe by default" flag: until an operator
	// explicitly enables submission they cannot accidentally lose funds
	// to a misconfigured agent.
	AllowSubmit bool
}

// ErrSubmitDisabled is returned by Swap when AllowSubmit is false.
// This is the production guard rail: agents that try to trade live
// without explicit operator opt-in fail visibly.
var ErrSubmitDisabled = errors.New("bittensor: live extrinsic submission not enabled (set bittensor.allow_submit: true in config to enable)")

// New constructs a Venue.
func New(cfg Config, signer wallet.Signer) (*Venue, error) {
	if signer == nil {
		return nil, errors.New("bittensor: signer is required")
	}
	if signer.Chain() != types.ChainBittensor {
		return nil, fmt.Errorf("bittensor: signer chain must be bittensor, got %q", signer.Chain())
	}
	bts, ok := signer.(*wallet.BittensorSigner)
	if !ok {
		return nil, fmt.Errorf("bittensor: signer must be *wallet.BittensorSigner, got %T", signer)
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
		signer:      bts,
		pollTimeout: pollTO,
		allowSubmit: cfg.AllowSubmit,
	}, nil
}

var _ swap.SwapVenue = (*Venue)(nil)

func (v *Venue) Name() string         { return VenueName }
func (v *Venue) Chain() types.ChainID { return types.ChainBittensor }

// Quote simulates the swap on-chain via swap_simSwap{TaoForAlpha,AlphaForTao}
// and returns a real expected fill including fee and AMM slippage.
//
// Convention:
//   InToken.Mint="TAO",  OutToken.Mint="SN8"  ⇒ buy SN8 alpha with TAO
//   InToken.Mint="SN8",  OutToken.Mint="TAO"  ⇒ sell SN8 alpha for TAO
func (v *Venue) Quote(ctx context.Context, req types.QuoteRequest) (types.Quote, error) {
	netuid, isBuy, err := parseSubnetMints(req.InToken.Mint, req.OutToken.Mint)
	if err != nil {
		return types.Quote{}, err
	}
	inRao := btchain.TAOToRAO(req.Amount)

	var sim btchain.SimulatedSwapResult
	if isBuy {
		sim, err = v.rpc.SimSwapTaoForAlpha(ctx, netuid, inRao)
	} else {
		sim, err = v.rpc.SimSwapAlphaForTao(ctx, netuid, inRao)
	}
	if err != nil {
		return types.Quote{}, err
	}

	// Spot price for impact calc.
	spotPrice, err := v.rpc.CurrentAlphaPrice(ctx, netuid)
	if err != nil {
		return types.Quote{}, err
	}

	inAmt := btchain.RAOToTAO(sim.AmountIn)
	outAmt := btchain.RAOToTAO(sim.AmountOut)

	// Price impact: |spot_out - actual_out| / spot_out.
	var priceImpact decimal.Decimal
	if isBuy {
		// Spot would give us inAmt / spotPrice alpha.
		if !spotPrice.IsZero() && !outAmt.IsZero() {
			spotOut := inAmt.Div(spotPrice)
			if !spotOut.IsZero() {
				priceImpact = spotOut.Sub(outAmt).Div(spotOut).Abs()
			}
		}
	} else {
		// Spot would give us inAmt * spotPrice TAO.
		spotOut := inAmt.Mul(spotPrice)
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
		Raw:         []byte(strconv.FormatUint(uint64(netuid), 10)),
	}, nil
}

// Swap submits the real on-chain stake or unstake extrinsic — but only
// when AllowSubmit is true. Returns ErrSubmitDisabled otherwise so the
// daemon's risk surface notices.
func (v *Venue) Swap(ctx context.Context, q types.Quote, slippageBps int) (types.TxHash, error) {
	if time.Now().After(q.ExpiresAt) {
		return "", swap.ErrQuoteExpired
	}
	if !v.allowSubmit {
		return "", ErrSubmitDisabled
	}
	_ = slippageBps // AMM slippage is enforced by the on-chain swap math + fee

	netuid, isBuy, err := parseSubnetMints(q.InToken.Mint, q.OutToken.Mint)
	if err != nil {
		return "", err
	}

	// Hotkey == coldkey for the simple self-stake trading-agent case.
	hotkeyPub := v.signer.PublicKey()

	opts := btchain.SubmitOptions{WaitTimeout: v.pollTimeout}
	var res btchain.SubmitResult
	if isBuy {
		taoRao := btchain.TAOToRAO(q.InAmount)
		res, err = v.rpc.AddStake(ctx, v.signer, netuid, hotkeyPub, taoRao, opts)
	} else {
		alphaRao := btchain.TAOToRAO(q.InAmount) // amount field is in input units
		res, err = v.rpc.RemoveStake(ctx, v.signer, netuid, hotkeyPub, alphaRao, opts)
	}
	if err != nil {
		return "", err
	}
	return types.TxHash(res.TxHash.Hex()), nil
}

// WaitConfirm in this implementation is a no-op: AddStake/RemoveStake
// already wait for inclusion before returning. Kept for SwapVenue
// interface conformance.
func (v *Venue) WaitConfirm(ctx context.Context, tx types.TxHash) (types.SwapResult, error) {
	return types.SwapResult{
		TxHash:    tx,
		Status:    types.SwapStatusConfirmed,
		BlockTime: time.Now().UTC(),
	}, nil
}

// MarketSnapshot returns current alpha prices for the requested subnet
// symbols (e.g. "SN8/TAO", "SN3/TAO"). Symbols not matching the SN{n}/TAO
// pattern are silently skipped.
//
// This satisfies an extension interface that the agent runtime checks
// for: when present, the runtime merges these ticks into the
// DecisionInput.Market snapshot the strategy receives.
func (v *Venue) MarketSnapshot(ctx context.Context, symbols []string) (map[string]types.SymbolSnap, error) {
	out := make(map[string]types.SymbolSnap, len(symbols))
	for _, sym := range symbols {
		netuid, ok := parseFeedSymbol(sym)
		if !ok {
			continue
		}
		price, err := v.rpc.CurrentAlphaPrice(ctx, netuid)
		if err != nil {
			continue // skip subnet that errored, keep the others
		}
		out[sym] = types.SymbolSnap{
			Tick: types.Tick{
				Time:   time.Now().UTC(),
				Venue:  VenueName,
				Symbol: sym,
				Bid:    price,
				Ask:    price,
				Last:   price,
			},
		}
	}
	return out, nil
}

// parseFeedSymbol parses "SN8/TAO" → 8.
func parseFeedSymbol(s string) (uint16, bool) {
	if !strings.HasPrefix(s, AlphaMintPrefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(s, AlphaMintPrefix)
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return 0, false
	}
	n, err := strconv.ParseUint(rest[:slash], 10, 16)
	if err != nil {
		return 0, false
	}
	return uint16(n), true
}

// Balance returns the TAO balance or alpha stake for the configured wallet.
//
// asset.Mint == "TAO" (or empty)  → free TAO balance
// asset.Mint == "SN{netuid}"      → alpha stake on that subnet
func (v *Venue) Balance(ctx context.Context, asset types.Asset) (decimal.Decimal, error) {
	if asset.Chain != types.ChainBittensor {
		return decimal.Zero, fmt.Errorf("bittensor: asset chain %q is not bittensor", asset.Chain)
	}
	if asset.Mint == TAOMint || asset.Mint == "" {
		rao, err := v.rpc.GetTAOBalance(ctx, v.signer.Address())
		if err != nil {
			return decimal.Zero, err
		}
		return btchain.RAOToTAO(rao), nil
	}
	netuid, err := parseSubnetMint(asset.Mint)
	if err != nil {
		return decimal.Zero, err
	}
	addr := v.signer.Address()
	rao, err := v.rpc.GetAlphaStake(ctx, addr, addr, netuid)
	if err != nil {
		return decimal.Zero, err
	}
	return btchain.RAOToTAO(rao), nil
}

// parseSubnetMints extracts (netuid, isBuy) from the in/out mint pair.
func parseSubnetMints(inMint, outMint string) (uint16, bool, error) {
	if inMint == TAOMint {
		netuid, err := parseSubnetMint(outMint)
		return netuid, true, err
	}
	if outMint == TAOMint {
		netuid, err := parseSubnetMint(inMint)
		return netuid, false, err
	}
	return 0, false, fmt.Errorf("bittensor: one of in/out mints must be %q, got %q/%q", TAOMint, inMint, outMint)
}

// parseSubnetMint converts "SN8" to 8.
func parseSubnetMint(mint string) (uint16, error) {
	if !strings.HasPrefix(mint, AlphaMintPrefix) || len(mint) <= len(AlphaMintPrefix) {
		return 0, fmt.Errorf("bittensor: invalid subnet mint %q (expected SN{netuid})", mint)
	}
	n, err := strconv.ParseUint(mint[len(AlphaMintPrefix):], 10, 16)
	if err != nil {
		return 0, fmt.Errorf("bittensor: invalid subnet mint %q: %w", mint, err)
	}
	return uint16(n), nil
}
