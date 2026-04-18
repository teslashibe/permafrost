package wallet

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/internal/types"
)

func TestNewEVMSigner_DerivesSameAddressAsHL(t *testing.T) {
	raw, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	hl, err := NewHyperliquidSignerFromPrivate(raw)
	if err != nil {
		t.Fatal(err)
	}
	es, err := NewEVMSigner(hl, types.ChainBase)
	if err != nil {
		t.Fatal(err)
	}
	if es.Address() != hl.Address() {
		t.Errorf("EVM and HL signers must share the same address; got %s vs %s",
			es.Address(), hl.Address())
	}
	if es.Chain() != types.ChainBase {
		t.Errorf("chain label: %q", es.Chain())
	}
}

func TestNewEVMSigner_RejectsNonEVMChain(t *testing.T) {
	raw, _ := hex.DecodeString("ac0974bec39a17e36ba4a6b4d238ff944bacb478cbed5efcae784d7bf4f2ff80")
	hl, _ := NewHyperliquidSignerFromPrivate(raw)
	if _, err := NewEVMSigner(hl, types.ChainSolana); err == nil {
		t.Error("expected error binding EVM signer to solana")
	}
}

func TestEVMSignerFromKeystore_NoKey(t *testing.T) {
	_, err := EVMSignerFromKeystore(nil, types.ChainBase)
	if err == nil || err.Error() == "" || !strings.Contains(err.Error(), "no EVM signer") {
		t.Errorf("expected ErrNoEVMSigner, got %v", err)
	}
}
