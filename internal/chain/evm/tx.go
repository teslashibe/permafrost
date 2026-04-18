package evm

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	ethcrypto "github.com/ethereum/go-ethereum/crypto"
)

// TxRequest captures the inputs to building a single transaction. Gas
// fields are filled in by EstimateAndFill if left zero.
type TxRequest struct {
	From  string   // 0x address (used for nonce + gas estimation)
	To    string   // 0x address; "" for contract creation (not used today)
	Data  string   // 0x-hex calldata
	Value *big.Int // wei; nil treated as zero

	// Optional caller-supplied overrides; left zero, the builder fills them.
	GasLimit             uint64   // EstimateGas if 0
	Nonce                *uint64  // pending count if nil
	GasPriceWei          *big.Int // legacy chains; GasPrice() if nil
	MaxFeePerGasWei      *big.Int // EIP-1559; computed from base fee if nil
	MaxPriorityFeePerGas *big.Int // EIP-1559; MaxPriorityFeePerGas() if nil
}

// EstimateAndFill performs RPC reads to populate any unset fields on req.
// On EIP-1559 chains we set maxFee = 2*baseFee + tip with a 25% safety
// buffer on the base fee — this is the well-known "Alchemy/Infura"
// recipe that survives short fee spikes without overpaying badly.
func EstimateAndFill(ctx context.Context, c *Client, req *TxRequest) error {
	if req.From == "" {
		return errors.New("evm: tx From is required")
	}
	if req.Value == nil {
		req.Value = new(big.Int)
	}
	if req.Nonce == nil {
		n, err := c.GetTransactionCount(ctx, req.From)
		if err != nil {
			return fmt.Errorf("evm: nonce: %w", err)
		}
		req.Nonce = &n
	}
	if req.GasLimit == 0 {
		valHex := "0x0"
		if req.Value.Sign() > 0 {
			valHex = "0x" + req.Value.Text(16)
		}
		gas, err := c.EstimateGas(ctx, EstimateGasParams{
			From: req.From, To: req.To, Data: req.Data, Value: valHex,
		})
		if err != nil {
			return fmt.Errorf("evm: estimate gas: %w", err)
		}
		// 25% safety buffer over the estimate. ERC-20 + aggregator calls
		// are notoriously borderline on the estimate.
		req.GasLimit = gas + (gas / 4)
	}

	switch c.chain.GasModel {
	case GasModelEIP1559:
		if req.MaxPriorityFeePerGas == nil {
			tip, err := c.MaxPriorityFeePerGas(ctx)
			if err != nil {
				return fmt.Errorf("evm: priority fee: %w", err)
			}
			req.MaxPriorityFeePerGas = tip
		}
		if req.MaxFeePerGasWei == nil {
			fh, err := c.FeeHistory(ctx, 1, "latest")
			if err != nil || len(fh.BaseFeePerGas) == 0 {
				// Fall back to gasPrice if fee history isn't available.
				gp, gpErr := c.GasPrice(ctx)
				if gpErr != nil {
					return fmt.Errorf("evm: fallback gas price: %w", gpErr)
				}
				maxFee := new(big.Int).Mul(gp, big.NewInt(2))
				maxFee.Add(maxFee, req.MaxPriorityFeePerGas)
				req.MaxFeePerGasWei = maxFee
			} else {
				base, ok := new(big.Int).SetString(strings.TrimPrefix(fh.BaseFeePerGas[len(fh.BaseFeePerGas)-1], "0x"), 16)
				if !ok {
					return fmt.Errorf("evm: parse base fee: %s", fh.BaseFeePerGas[0])
				}
				// 2*base + tip = comfortable max for the next ~6 blocks.
				maxFee := new(big.Int).Mul(base, big.NewInt(2))
				maxFee.Add(maxFee, req.MaxPriorityFeePerGas)
				req.MaxFeePerGasWei = maxFee
			}
		}
	case GasModelLegacy:
		if req.GasPriceWei == nil {
			gp, err := c.GasPrice(ctx)
			if err != nil {
				return fmt.Errorf("evm: gas price: %w", err)
			}
			req.GasPriceWei = gp
		}
	default:
		return fmt.Errorf("evm: unknown gas model %q", c.chain.GasModel)
	}
	return nil
}

// SignAndSend builds a transaction from req, signs it with priv, and
// submits via eth_sendRawTransaction. Returns the tx hash.
//
// priv is the secp256k1 private key (32 bytes). It MUST come from the
// wallet keystore — never from a config file or env var.
func SignAndSend(ctx context.Context, c *Client, req TxRequest, priv *ecdsa.PrivateKey) (string, error) {
	if priv == nil {
		return "", errors.New("evm: private key is nil")
	}
	if req.Nonce == nil {
		return "", errors.New("evm: nonce is required (call EstimateAndFill first)")
	}
	if req.GasLimit == 0 {
		return "", errors.New("evm: gas limit is required (call EstimateAndFill first)")
	}

	chainID := new(big.Int).SetUint64(c.chain.NumericID)
	to := common.HexToAddress(req.To)
	data, err := hexToBytes(req.Data)
	if err != nil {
		return "", fmt.Errorf("evm: data hex: %w", err)
	}
	value := req.Value
	if value == nil {
		value = new(big.Int)
	}

	var tx *ethtypes.Transaction
	switch c.chain.GasModel {
	case GasModelEIP1559:
		if req.MaxFeePerGasWei == nil || req.MaxPriorityFeePerGas == nil {
			return "", errors.New("evm: EIP-1559 requires maxFee and tip")
		}
		tx = ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			ChainID:   chainID,
			Nonce:     *req.Nonce,
			GasTipCap: req.MaxPriorityFeePerGas,
			GasFeeCap: req.MaxFeePerGasWei,
			Gas:       req.GasLimit,
			To:        &to,
			Value:     value,
			Data:      data,
		})
	case GasModelLegacy:
		if req.GasPriceWei == nil {
			return "", errors.New("evm: legacy requires GasPriceWei")
		}
		tx = ethtypes.NewTx(&ethtypes.LegacyTx{
			Nonce:    *req.Nonce,
			GasPrice: req.GasPriceWei,
			Gas:      req.GasLimit,
			To:       &to,
			Value:    value,
			Data:     data,
		})
	default:
		return "", fmt.Errorf("evm: unknown gas model %q", c.chain.GasModel)
	}

	signer := ethtypes.LatestSignerForChainID(chainID)
	signed, err := ethtypes.SignTx(tx, signer, priv)
	if err != nil {
		return "", fmt.Errorf("evm: sign: %w", err)
	}
	raw, err := signed.MarshalBinary()
	if err != nil {
		return "", fmt.Errorf("evm: marshal tx: %w", err)
	}
	hexRaw := "0x" + common.Bytes2Hex(raw)
	hash, err := c.SendRawTransaction(ctx, hexRaw)
	if err != nil {
		return "", fmt.Errorf("evm: broadcast: %w", err)
	}
	return hash, nil
}

// SendAndWait is a convenience wrapper: estimate → sign → send → wait
// for receipt. Returns the receipt (which may indicate on-chain
// failure via Status="0x0"). budget defaults to 90s if zero.
func SendAndWait(ctx context.Context, c *Client, req TxRequest, priv *ecdsa.PrivateKey, budget time.Duration) (*Receipt, string, error) {
	if budget == 0 {
		budget = 90 * time.Second
	}
	if err := EstimateAndFill(ctx, c, &req); err != nil {
		return nil, "", err
	}
	hash, err := SignAndSend(ctx, c, req, priv)
	if err != nil {
		return nil, "", err
	}
	r, err := c.WaitForReceipt(ctx, hash, budget, 2*time.Second)
	if err != nil {
		return nil, hash, err
	}
	return r, hash, nil
}

// AddressFromPrivateKey derives the canonical 0x-prefixed lowercase EVM
// address from a secp256k1 private key. Handy for cross-checking that
// the loaded keystore key matches an expected address.
func AddressFromPrivateKey(priv *ecdsa.PrivateKey) string {
	return strings.ToLower(ethcrypto.PubkeyToAddress(priv.PublicKey).Hex())
}
