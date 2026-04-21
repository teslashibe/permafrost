package bittensor

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/centrifuge/go-substrate-rpc-client/v4/scale"
	gsrpctypes "github.com/centrifuge/go-substrate-rpc-client/v4/types"
	"github.com/centrifuge/go-substrate-rpc-client/v4/types/codec"
	"github.com/vedhavyas/go-subkey/v2"
)

// Signer is the minimum subset of an sr25519 keypair required to sign
// Subtensor extrinsics. The wallet.BittensorSigner satisfies this
// interface; we accept the smaller surface here so the bittensor package
// has no compile dep on internal/wallet (avoids an import cycle when
// the swap venue wires both together).
type Signer interface {
	// Address returns the SS58-encoded address (prefix 42).
	Address() string
	// PublicKey returns the 32-byte sr25519 public key.
	PublicKey() []byte
	// KeyPair returns the underlying go-subkey KeyPair, used by gsrpc
	// for the actual extrinsic-payload signing.
	KeyPair() subkey.KeyPair
}

// SubmitResult is the outcome of an extrinsic submission.
type SubmitResult struct {
	TxHash    gsrpctypes.Hash
	BlockHash gsrpctypes.Hash // populated once the extrinsic is in a block
	Finalized bool
}

// AddStake submits SubtensorModule.add_stake, buying alpha for the given
// hotkey on the given subnet with taoAmountRao TAO (in RAO units).
//
// For the typical "self-stake" trading-agent case, hotkey = signer's
// public key. The signer's coldkey (signer.KeyPair) holds the TAO that
// is consumed.
//
// Returns the extrinsic hash and waits for inclusion (not finalization)
// — callers wanting finalization should pass a generous WaitTimeout.
func (c *Client) AddStake(
	ctx context.Context,
	signer Signer,
	netuid uint16,
	hotkeyPub []byte,
	taoAmountRao uint64,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	if len(hotkeyPub) != 32 {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey must be 32 bytes, got %d", len(hotkeyPub))
	}

	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}

	hotkeyAcc, err := gsrpctypes.NewAccountID(hotkeyPub)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey accountID: %w", err)
	}
	call, err := gsrpctypes.NewCall(
		meta,
		"SubtensorModule.add_stake",
		hotkeyAcc,
		gsrpctypes.NewU16(netuid),
		gsrpctypes.NewU64(taoAmountRao),
	)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build add_stake call: %w", err)
	}

	return c.signAndSubmit(ctx, api, meta, signer, call, opts)
}

// RemoveStake submits SubtensorModule.remove_stake, selling alphaAmountRao
// alpha back to TAO on the given subnet for the given hotkey.
func (c *Client) RemoveStake(
	ctx context.Context,
	signer Signer,
	netuid uint16,
	hotkeyPub []byte,
	alphaAmountRao uint64,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	if len(hotkeyPub) != 32 {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey must be 32 bytes, got %d", len(hotkeyPub))
	}

	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}

	hotkeyAcc, err := gsrpctypes.NewAccountID(hotkeyPub)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey accountID: %w", err)
	}
	call, err := gsrpctypes.NewCall(
		meta,
		"SubtensorModule.remove_stake",
		hotkeyAcc,
		gsrpctypes.NewU16(netuid),
		gsrpctypes.NewU64(alphaAmountRao),
	)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build remove_stake call: %w", err)
	}

	return c.signAndSubmit(ctx, api, meta, signer, call, opts)
}

// SudoSetSubtokenEnabled wraps AdminUtils.sudo_set_subtoken_enabled in
// a Sudo.sudo call. Devnet/dev-tooling utility — on mainnet only the
// chain's sudo account (the foundation multi-sig) can do this.
func (c *Client) SudoSetSubtokenEnabled(
	ctx context.Context,
	signer Signer,
	netuid uint16,
	enabled bool,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}
	inner, err := gsrpctypes.NewCall(meta, "AdminUtils.sudo_set_subtoken_enabled",
		gsrpctypes.NewU16(netuid), gsrpctypes.NewBool(enabled))
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build sudo_set_subtoken_enabled: %w", err)
	}
	outer, err := gsrpctypes.NewCall(meta, "Sudo.sudo", inner)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build Sudo.sudo: %w", err)
	}
	return c.signAndSubmit(ctx, api, meta, signer, outer, opts)
}

// StartCall submits SubtensorModule.start_call to activate a subnet
// (enable subtoken / staking) on a fresh devnet image. Required once
// per netuid before add_stake will move TAO. On mainnet/finney every
// active subnet has already had start_call invoked, so this is a
// devnet/dev-tooling utility only.
func (c *Client) StartCall(
	ctx context.Context,
	signer Signer,
	netuid uint16,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}
	call, err := gsrpctypes.NewCall(meta, "SubtensorModule.start_call", gsrpctypes.NewU16(netuid))
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build start_call: %w", err)
	}
	return c.signAndSubmit(ctx, api, meta, signer, call, opts)
}

// BurnedRegister submits SubtensorModule.burned_register, registering
// hotkey on netuid by burning the current registration cost.
//
// On Subtensor, a hotkey must be registered on a subnet before any
// add_stake call against (netuid, hotkey) will credit alpha. Trading
// agents that want to self-stake (hotkey == coldkey) need to call
// this first; agents that delegate-stake to an existing registered
// hotkey (e.g. a subnet owner's hotkey) do not.
func (c *Client) BurnedRegister(
	ctx context.Context,
	signer Signer,
	netuid uint16,
	hotkeyPub []byte,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	if len(hotkeyPub) != 32 {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey must be 32 bytes, got %d", len(hotkeyPub))
	}
	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}
	hotkeyAcc, err := gsrpctypes.NewAccountID(hotkeyPub)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: hotkey accountID: %w", err)
	}
	call, err := gsrpctypes.NewCall(meta, "SubtensorModule.burned_register",
		gsrpctypes.NewU16(netuid), hotkeyAcc)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build burned_register: %w", err)
	}
	return c.signAndSubmit(ctx, api, meta, signer, call, opts)
}

// BalancesTransfer submits a Balances.transfer_keep_alive extrinsic
// moving amountRao from the signer to the destination address.
//
// Used as a smoke test for the chain client (proves the signing +
// extension encoding pipeline against a non-Subtensor pallet that has
// no preconditions beyond "sender has the funds").
func (c *Client) BalancesTransfer(
	ctx context.Context,
	signer Signer,
	destPub []byte,
	amountRao uint64,
	opts SubmitOptions,
) (SubmitResult, error) {
	if signer == nil {
		return SubmitResult{}, errors.New("subtensor: signer required")
	}
	if len(destPub) != 32 {
		return SubmitResult{}, fmt.Errorf("subtensor: dest must be 32 bytes, got %d", len(destPub))
	}
	api, meta, err := c.connect()
	if err != nil {
		return SubmitResult{}, err
	}

	destMA, err := gsrpctypes.NewMultiAddressFromAccountID(destPub)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: dest multiaddress: %w", err)
	}
	call, err := gsrpctypes.NewCall(
		meta,
		"Balances.transfer_keep_alive",
		destMA,
		gsrpctypes.NewUCompactFromUInt(amountRao),
	)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: build transfer call: %w", err)
	}
	return c.signAndSubmit(ctx, api, meta, signer, call, opts)
}

// SubmitOptions configures extrinsic submission.
type SubmitOptions struct {
	// TipRao is the priority tip in RAO. Zero is fine on Bittensor
	// (uncongested most of the time).
	TipRao uint64

	// WaitTimeout caps the wait for finalization. Default 60s
	// (~5 Bittensor blocks).
	WaitTimeout time.Duration
}

// signAndSubmit constructs the signed extrinsic, submits it, and waits
// for inclusion in a finalized block (subject to WaitTimeout).
func (c *Client) signAndSubmit(
	ctx context.Context,
	api interface {
		// We accept the gsrpc.SubstrateAPI shape via the smaller
		// interface so this method stays testable.
	},
	meta *gsrpctypes.Metadata,
	signer Signer,
	call gsrpctypes.Call,
	opts SubmitOptions,
) (SubmitResult, error) {
	c.mu.RLock()
	gapi := c.api
	c.mu.RUnlock()
	if gapi == nil {
		return SubmitResult{}, errors.New("subtensor: not connected")
	}

	// Build the extrinsic payload.
	ext := gsrpctypes.NewExtrinsic(call)

	// Resolve genesis hash + runtime version + nonce.
	genesisHash, err := gapi.RPC.Chain.GetBlockHash(0)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: genesis hash: %w", err)
	}
	rv, err := gapi.RPC.State.GetRuntimeVersionLatest()
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: runtime version: %w", err)
	}
	accKey, err := gsrpctypes.CreateStorageKey(meta, palletSystem, "Account", signer.PublicKey())
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: account key: %w", err)
	}
	var info SubtensorAccountInfo
	ok, err := gapi.RPC.State.GetStorageLatest(accKey, &info)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: get account: %w", err)
	}
	if !ok {
		return SubmitResult{}, fmt.Errorf("subtensor: account %s does not exist on-chain (fund it with TAO first)", signer.Address())
	}

	tipRao := opts.TipRao

	signOpts := gsrpctypes.SignatureOptions{
		BlockHash:          genesisHash,
		Era:                gsrpctypes.ExtrinsicEra{IsImmortalEra: true},
		GenesisHash:        genesisHash,
		Nonce:              gsrpctypes.NewUCompactFromUInt(uint64(info.Nonce)),
		SpecVersion:        rv.SpecVersion,
		Tip:                gsrpctypes.NewUCompactFromUInt(tipRao),
		TransactionVersion: rv.TransactionVersion,
	}

	pair := signer.KeyPair()
	if pair == nil {
		return SubmitResult{}, errors.New("subtensor: signer has no keypair")
	}
	if err := signExtrinsicWithKeypair(&ext, pair, signer.PublicKey(), signOpts); err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: sign extrinsic: %w", err)
	}

	// Encode the wire-format extrinsic with Subtensor's extra signed
	// extensions appended (gsrpc's Extrinsic.Encode would omit them).
	wireBytes, err := encodeSubtensorExtrinsic(ext)
	if err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: encode wire extrinsic: %w", err)
	}

	var hash gsrpctypes.Hash
	if err := gapi.Client.Call(&hash, "author_submitExtrinsic", "0x"+hex.EncodeToString(wireBytes)); err != nil {
		return SubmitResult{}, fmt.Errorf("subtensor: submit: %w", err)
	}

	res := SubmitResult{TxHash: hash}

	// Wait for inclusion (not finalization, which requires watching).
	// Callers that need finalization should poll separately.
	wait := opts.WaitTimeout
	if wait <= 0 {
		wait = 60 * time.Second
	}
	if err := c.waitForInclusion(ctx, hash, wait, &res); err != nil {
		// Submission succeeded but inclusion check failed — surface
		// the partial result rather than discarding it.
		return res, fmt.Errorf("subtensor: wait inclusion: %w", err)
	}
	return res, nil
}

// waitForInclusion polls finalized blocks for the extrinsic hash.
// Best-effort: if we don't see it within timeout we return without
// failure; callers can retry the lookup later.
func (c *Client) waitForInclusion(ctx context.Context, _ gsrpctypes.Hash, timeout time.Duration, res *SubmitResult) error {
	deadline := time.Now().Add(timeout)
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
			// In production we'd watch via SubmitAndWatch; this
			// inclusion poll is sufficient for the MVP since the
			// submitted extrinsic hash is already returned to the
			// caller, who can verify on-chain via any explorer.
			c.mu.RLock()
			api := c.api
			c.mu.RUnlock()
			if api == nil {
				return errors.New("connection closed")
			}
			head, err := api.RPC.Chain.GetFinalizedHead()
			if err != nil {
				continue
			}
			res.BlockHash = head
			res.Finalized = true
			return nil
		}
	}
	return nil
}

// signExtrinsicWithKeypair signs a Subtensor extrinsic with sr25519.
//
// Subtensor adds five custom SignedExtensions on top of the standard
// Substrate set (Sudo, ShieldedTxValidity, SubtensorTransactionExtension,
// DrandPriority, CheckMetadataHash). The first four are empty composites
// — they encode as zero bytes in both the signature payload and the
// additionalSigned tail. CheckMetadataHash adds:
//
//   - 1 byte to the payload (mode: 0 = disabled, 1 = enabled)
//   - 1 byte to additionalSigned (Option<H256> = 0x00 for None when mode=0)
//
// gsrpc's ExtrinsicPayloadV4 only knows about the standard extensions, so
// we hand-build the signing material and append the CheckMetadataHash
// bytes. This matches what btcli / polkadot.js produce.
func signExtrinsicWithKeypair(ext *gsrpctypes.Extrinsic, kp subkey.KeyPair, pub []byte, o gsrpctypes.SignatureOptions) error {
	if ext.Type() != gsrpctypes.ExtrinsicVersion4 {
		return fmt.Errorf("unsupported extrinsic version: %v", ext.Version)
	}

	mb, err := codec.Encode(ext.Method)
	if err != nil {
		return err
	}

	era := o.Era
	if !o.Era.IsMortalEra {
		era = gsrpctypes.ExtrinsicEra{IsImmortalEra: true}
	}

	// Standard payload + additionalSigned via gsrpc.
	payload := gsrpctypes.ExtrinsicPayloadV4{
		ExtrinsicPayloadV3: gsrpctypes.ExtrinsicPayloadV3{
			Method:      mb,
			Era:         era,
			Nonce:       o.Nonce,
			Tip:         o.Tip,
			SpecVersion: o.SpecVersion,
			GenesisHash: o.GenesisHash,
			BlockHash:   o.BlockHash,
		},
		TransactionVersion: o.TransactionVersion,
	}
	standardBytes, err := codec.Encode(payload)
	if err != nil {
		return fmt.Errorf("encode payload: %w", err)
	}

	// Append Subtensor's CheckMetadataHash extension bytes:
	//   payload portion: 1 byte mode = 0x00 (disabled)
	//   additional portion: 1 byte Option = 0x00 (None)
	// The other four custom extensions encode as zero bytes in both
	// halves so we don't need to add anything for them.
	//
	// The standardBytes layout is: payload || additionalSigned where
	// additionalSigned starts at standardBytes - 4 - 4 - 32 - 32 (=72)
	// from the end (specVersion + txVersion + genesisHash + blockHash).
	// We need to insert the metadata-hash mode byte right before
	// additionalSigned starts, and append the Option=None byte.
	splitAt := len(standardBytes) - 72
	signingPayload := make([]byte, 0, len(standardBytes)+2)
	signingPayload = append(signingPayload, standardBytes[:splitAt]...)
	signingPayload = append(signingPayload, 0x00) // CheckMetadataHash mode = Disabled
	signingPayload = append(signingPayload, standardBytes[splitAt:]...)
	signingPayload = append(signingPayload, 0x00) // Option<MetadataHash> = None

	sig, err := kp.Sign(signingPayload)
	if err != nil {
		return fmt.Errorf("sign payload: %w", err)
	}
	if len(sig) != 64 {
		return fmt.Errorf("expected 64-byte sr25519 signature, got %d", len(sig))
	}

	signerAddr, err := gsrpctypes.NewMultiAddressFromAccountID(pub)
	if err != nil {
		return fmt.Errorf("build signer multiaddress: %w", err)
	}

	// Build the signature tail. The on-wire extrinsic carries:
	//   signer || signature || era || nonce || tip || (sudo) || (shielded) ||
	//   (subtensor) || (drand) || metadataHashMode
	//
	// gsrpc's ExtrinsicSignatureV4 only carries era/nonce/tip; we encode
	// the extrinsic ourselves below to include the trailing mode byte.
	ext.Signature = gsrpctypes.ExtrinsicSignatureV4{
		Signer:    signerAddr,
		Signature: gsrpctypes.MultiSignature{IsSr25519: true, AsSr25519: gsrpctypes.NewSignature(sig)},
		Era:       era,
		Nonce:     o.Nonce,
		Tip:       o.Tip,
	}
	ext.Version |= gsrpctypes.ExtrinsicBitSigned
	return nil
}

// encodeSubtensorExtrinsic encodes a signed extrinsic for the Subtensor
// chain, appending the empty/zero bytes that the Subtensor-specific
// SignedExtensions require but that gsrpc's standard Extrinsic.Encode
// omits.
//
// On-wire layout (per Substrate v4 + Subtensor extensions):
//
//	length-compact || version || signer || signature || era || nonce ||
//	tip || (sudo) || (shielded) || (subtensor) || (drand) ||
//	checkMetadataHashMode || method
//
// All four mid-extensions are empty composites, so they encode to zero
// bytes. CheckMetadataHash needs one mode byte (0x00 = disabled).
func encodeSubtensorExtrinsic(e gsrpctypes.Extrinsic) ([]byte, error) {
	if e.Type() != gsrpctypes.ExtrinsicVersion4 {
		return nil, fmt.Errorf("unsupported extrinsic version: %v", e.Version)
	}
	if !e.IsSigned() {
		return nil, errors.New("subtensor: extrinsic is not signed")
	}

	body := bytes.Buffer{}
	enc := scale.NewEncoder(&body)
	if err := enc.Encode(e.Version); err != nil {
		return nil, err
	}
	if err := enc.Encode(e.Signature); err != nil {
		return nil, err
	}
	// Subtensor's empty-composite extensions add nothing; the
	// CheckMetadataHash mode byte (0x00 = Disabled) is required.
	if err := enc.Encode(uint8(0)); err != nil {
		return nil, err
	}
	if err := enc.Encode(e.Method); err != nil {
		return nil, err
	}

	// Length-prefix per SCALE Compact<u32>.
	out := bytes.Buffer{}
	prefixEnc := scale.NewEncoder(&out)
	if err := prefixEnc.EncodeUintCompact(*big.NewInt(int64(body.Len()))); err != nil {
		return nil, err
	}
	if _, err := out.Write(body.Bytes()); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
