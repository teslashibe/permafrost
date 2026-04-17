package wallet

import (
	"context"
	"crypto/ed25519"
	"path/filepath"
	"strings"
	"testing"

	"github.com/teslashibe/permafrost/internal/types"
)

func TestLocalKeystore_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	pass := "correct horse battery staple"

	ks, err := NewLocalKeystore(path, pass)
	if err != nil {
		t.Fatal(err)
	}

	sol, err := GenerateSolanaKey()
	if err != nil {
		t.Fatal(err)
	}
	hl, err := GenerateHyperliquidKey()
	if err != nil {
		t.Fatal(err)
	}
	ks.SetSolana(sol)
	ks.SetHyperliquid(hl)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	// Reopen with correct passphrase
	ks2, err := NewLocalKeystore(path, pass)
	if err != nil {
		t.Fatal(err)
	}
	chains := ks2.Chains()
	if len(chains) != 2 {
		t.Fatalf("Chains: %v", chains)
	}

	gotSol, err := ks2.Signer(types.ChainSolana)
	if err != nil {
		t.Fatal(err)
	}
	if gotSol.Address() != sol.Address() {
		t.Errorf("solana address mismatch")
	}
	gotHL, err := ks2.Signer(types.ChainHyperliquid)
	if err != nil {
		t.Fatal(err)
	}
	if gotHL.Address() != hl.Address() {
		t.Errorf("hyperliquid address mismatch")
	}

	// Sign with reopened key, verify against original pubkey
	sig, _ := gotSol.Sign(context.Background(), []byte("hello"))
	pub := ed25519.PrivateKey(sol.SecretKey()).Public().(ed25519.PublicKey)
	if !ed25519.Verify(pub, []byte("hello"), sig) {
		t.Errorf("loaded solana key cannot sign correctly")
	}
}

func TestLocalKeystore_WrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")

	ks, _ := NewLocalKeystore(path, "right")
	sol, _ := GenerateSolanaKey()
	ks.SetSolana(sol)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	_, err := NewLocalKeystore(path, "wrong")
	if err == nil || !strings.Contains(err.Error(), "decrypt failed") {
		t.Fatalf("expected decrypt failure, got %v", err)
	}
}

func TestLocalKeystore_AddressesWithoutUnlock(t *testing.T) {
	// We can't read addresses without unlocking via our API, but the
	// public Addresses() method works in-memory after a load. Smoke-test
	// that empty file is OK.
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.json")
	ks, err := NewLocalKeystore(path, "x")
	if err != nil {
		t.Fatal(err)
	}
	if len(ks.Addresses()) != 0 {
		t.Errorf("fresh keystore should have no addresses")
	}
	if len(ks.Chains()) != 0 {
		t.Errorf("fresh keystore should have no chains")
	}
}

func TestLocalKeystore_ReplaceKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "keystore.json")
	pass := "p"

	ks, _ := NewLocalKeystore(path, pass)
	first, _ := GenerateSolanaKey()
	ks.SetSolana(first)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	second, _ := GenerateSolanaKey()
	ks.SetSolana(second)
	if err := ks.Save(); err != nil {
		t.Fatal(err)
	}

	loaded, _ := NewLocalKeystore(path, pass)
	got, _ := loaded.Signer(types.ChainSolana)
	if got.Address() != second.Address() {
		t.Errorf("replace did not stick")
	}
	if len(loaded.Addresses()) != 1 {
		t.Errorf("expected exactly 1 address after replace")
	}
}

func TestLocalKeystore_RequiresArgs(t *testing.T) {
	if _, err := NewLocalKeystore("", "x"); err == nil {
		t.Error("missing path should error")
	}
	if _, err := NewLocalKeystore("x", ""); err == nil {
		t.Error("missing passphrase should error")
	}
}
