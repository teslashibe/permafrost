package evm

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strings"
)

// ERC-20 selectors (4-byte function signatures, big-endian).
//
//	balanceOf(address)              0x70a08231
//	allowance(address,address)      0xdd62ed3e
//	approve(address,uint256)        0x095ea7b3
//	transfer(address,uint256)       0xa9059cbb
//	decimals()                      0x313ce567
const (
	selBalanceOf = "70a08231"
	selAllowance = "dd62ed3e"
	selApprove   = "095ea7b3"
	selTransfer  = "a9059cbb"
	selDecimals  = "313ce567"
)

// MaxUint256 is the canonical "infinite approval" amount.
var MaxUint256 = func() *big.Int {
	n := new(big.Int).Lsh(big.NewInt(1), 256)
	return n.Sub(n, big.NewInt(1))
}()

// ERC20Token is a thin reader/writer over a single ERC-20 contract.
type ERC20Token struct {
	rpc      *Client
	contract string // 0x-prefixed lowercase
}

// NewERC20Token constructs a token handle. The contract address is
// stored lowercased for stable comparison.
func NewERC20Token(rpc *Client, contract string) *ERC20Token {
	return &ERC20Token{rpc: rpc, contract: strings.ToLower(contract)}
}

// Address returns the underlying contract address.
func (t *ERC20Token) Address() string { return t.contract }

// BalanceOf returns the ERC-20 balance of owner (in base units — the
// caller is responsible for token decimals).
func (t *ERC20Token) BalanceOf(ctx context.Context, owner string) (*big.Int, error) {
	data := "0x" + selBalanceOf + padAddress(owner)
	out, err := t.rpc.Call(ctx, CallParams{To: t.contract, Data: data})
	if err != nil {
		return nil, fmt.Errorf("erc20.balanceOf: %w", err)
	}
	return new(big.Int).SetBytes(out), nil
}

// Allowance returns how much spender is allowed to pull from owner.
func (t *ERC20Token) Allowance(ctx context.Context, owner, spender string) (*big.Int, error) {
	data := "0x" + selAllowance + padAddress(owner) + padAddress(spender)
	out, err := t.rpc.Call(ctx, CallParams{To: t.contract, Data: data})
	if err != nil {
		return nil, fmt.Errorf("erc20.allowance: %w", err)
	}
	return new(big.Int).SetBytes(out), nil
}

// Decimals returns the on-chain decimals() value.
func (t *ERC20Token) Decimals(ctx context.Context) (uint8, error) {
	data := "0x" + selDecimals
	out, err := t.rpc.Call(ctx, CallParams{To: t.contract, Data: data})
	if err != nil {
		return 0, fmt.Errorf("erc20.decimals: %w", err)
	}
	if len(out) == 0 {
		return 0, fmt.Errorf("erc20.decimals: empty response")
	}
	n := new(big.Int).SetBytes(out)
	return uint8(n.Uint64()), nil
}

// EncodeApprove builds calldata for `approve(spender, amount)`. Returns
// the 0x-prefixed hex string ready to drop into a tx.
func EncodeApprove(spender string, amount *big.Int) string {
	return "0x" + selApprove + padAddress(spender) + padUint256(amount)
}

// EncodeTransfer builds calldata for `transfer(to, amount)`.
func EncodeTransfer(to string, amount *big.Int) string {
	return "0x" + selTransfer + padAddress(to) + padUint256(amount)
}

// EncodeBalanceOf builds calldata for `balanceOf(owner)`. Exposed for
// callers that want to stage a manual eth_call.
func EncodeBalanceOf(owner string) string {
	return "0x" + selBalanceOf + padAddress(owner)
}

// EncodeAllowance builds calldata for `allowance(owner, spender)`.
func EncodeAllowance(owner, spender string) string {
	return "0x" + selAllowance + padAddress(owner) + padAddress(spender)
}

// padAddress formats a 0x-prefixed address as a 32-byte left-zero-padded
// hex string (the EVM ABI calling convention for `address` arguments).
func padAddress(addr string) string {
	clean := strings.ToLower(strings.TrimPrefix(addr, "0x"))
	if len(clean) > 40 {
		clean = clean[len(clean)-40:]
	}
	return strings.Repeat("0", 64-len(clean)) + clean
}

// padUint256 formats a *big.Int as a 32-byte left-zero-padded hex string.
// Negative values panic — uint256 has no negatives.
func padUint256(n *big.Int) string {
	if n == nil {
		return strings.Repeat("0", 64)
	}
	if n.Sign() < 0 {
		panic("evm: uint256 cannot be negative")
	}
	h := hex.EncodeToString(n.Bytes())
	if len(h) > 64 {
		panic(fmt.Sprintf("evm: uint256 overflow: %d bytes", len(n.Bytes())))
	}
	return strings.Repeat("0", 64-len(h)) + h
}
