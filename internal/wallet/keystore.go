package wallet

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/scrypt"

	"github.com/teslashibe/permafrost/pkg/types"
)

const (
	keystoreVersion = 1

	// scrypt parameters tuned for interactive use; ~250ms on a modern laptop.
	scryptN = 1 << 15 // 32768
	scryptR = 8
	scryptP = 1
	keyLen  = 32 // AES-256

	saltLen  = 16
	nonceLen = 12 // GCM standard
)

// keystoreFile is the on-disk JSON representation. Public addresses are
// stored in clear so they can be listed without unlocking the file.
type keystoreFile struct {
	Version    int               `json:"version"`
	KDF        kdfParams         `json:"kdf"`
	Nonce      string            `json:"nonce"`      // base64
	Ciphertext string            `json:"ciphertext"` // base64
	Addresses  []addressEntry    `json:"addresses"`
}

type kdfParams struct {
	Name string `json:"name"` // "scrypt"
	N    int    `json:"n"`
	R    int    `json:"r"`
	P    int    `json:"p"`
	Salt string `json:"salt"` // base64
}

type addressEntry struct {
	Chain   types.ChainID `json:"chain"`
	Address string        `json:"address"`
}

// secretsBlob is the plaintext encrypted body of a keystore. Maps chain ->
// raw secret bytes (ed25519 64-byte secret for Solana, 32-byte secp256k1 for
// Hyperliquid).
type secretsBlob struct {
	Solana      []byte `json:"solana,omitempty"`
	Hyperliquid []byte `json:"hyperliquid,omitempty"`
}

// LocalKeystore is an encrypted-on-disk Keystore implementation suited to
// single-operator local-first deployments. It is the only concrete Keystore
// shipped in v1; KMS / hardware wallets are deferred to v2.
type LocalKeystore struct {
	path       string
	passphrase string

	mu      sync.Mutex
	signers map[types.ChainID]Signer
	addrs   []addressEntry
}

// NewLocalKeystore opens an existing keystore file at path or creates an
// empty in-memory one if the file does not exist. Call Save() to persist.
func NewLocalKeystore(path, passphrase string) (*LocalKeystore, error) {
	if path == "" {
		return nil, errors.New("keystore: path is required")
	}
	if passphrase == "" {
		return nil, errors.New("keystore: passphrase is required")
	}
	ks := &LocalKeystore{
		path:       path,
		passphrase: passphrase,
		signers:    make(map[types.ChainID]Signer),
	}
	switch _, err := os.Stat(path); {
	case err == nil:
		if err := ks.load(); err != nil {
			return nil, err
		}
	case errors.Is(err, fs.ErrNotExist):
		// fresh keystore
	default:
		return nil, fmt.Errorf("keystore: stat: %w", err)
	}
	return ks, nil
}

// Path returns the on-disk path.
func (k *LocalKeystore) Path() string { return k.path }

// Addresses returns a snapshot of (chain, address) pairs in deterministic
// order. Safe to call without a passphrase.
func (k *LocalKeystore) Addresses() []addressEntry {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]addressEntry, len(k.addrs))
	copy(out, k.addrs)
	return out
}

// SetSolana installs (or replaces) the Solana key in the keystore. Call
// Save() after one or more set operations to persist.
func (k *LocalKeystore) SetSolana(s *SolanaSigner) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.signers[types.ChainSolana] = s
	k.upsertAddrLocked(addressEntry{Chain: types.ChainSolana, Address: s.Address()})
}

// SetHyperliquid installs (or replaces) the Hyperliquid key.
func (k *LocalKeystore) SetHyperliquid(s *HyperliquidSigner) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.signers[types.ChainHyperliquid] = s
	k.upsertAddrLocked(addressEntry{Chain: types.ChainHyperliquid, Address: s.Address()})
}

// Signer returns the signer for chain or an error if none is configured.
func (k *LocalKeystore) Signer(chain types.ChainID) (Signer, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	s, ok := k.signers[chain]
	if !ok {
		return nil, fmt.Errorf("keystore: no signer for chain %q", chain)
	}
	return s, nil
}

// Chains returns the configured chains in deterministic order.
func (k *LocalKeystore) Chains() []types.ChainID {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]types.ChainID, 0, len(k.signers))
	for _, e := range k.addrs {
		if _, ok := k.signers[e.Chain]; ok {
			out = append(out, e.Chain)
		}
	}
	return out
}

// Save writes the encrypted keystore to disk.
func (k *LocalKeystore) Save() error {
	k.mu.Lock()
	defer k.mu.Unlock()

	blob := secretsBlob{}
	if s, ok := k.signers[types.ChainSolana].(*SolanaSigner); ok {
		blob.Solana = s.SecretKey()
	}
	if s, ok := k.signers[types.ChainHyperliquid].(*HyperliquidSigner); ok {
		raw := s.priv.Serialize()
		blob.Hyperliquid = raw
	}
	plain, err := json.Marshal(blob)
	if err != nil {
		return fmt.Errorf("keystore: marshal blob: %w", err)
	}

	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return fmt.Errorf("keystore: rand salt: %w", err)
	}
	dk, err := scrypt.Key([]byte(k.passphrase), salt, scryptN, scryptR, scryptP, keyLen)
	if err != nil {
		return fmt.Errorf("keystore: scrypt: %w", err)
	}
	gcm, err := newGCM(dk)
	if err != nil {
		return err
	}
	nonce := make([]byte, nonceLen)
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("keystore: rand nonce: %w", err)
	}
	ct := gcm.Seal(nil, nonce, plain, nil)

	file := keystoreFile{
		Version: keystoreVersion,
		KDF: kdfParams{
			Name: "scrypt", N: scryptN, R: scryptR, P: scryptP,
			Salt: hex.EncodeToString(salt),
		},
		Nonce:      hex.EncodeToString(nonce),
		Ciphertext: hex.EncodeToString(ct),
		Addresses:  k.addrs,
	}
	body, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("keystore: marshal file: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(k.path), 0o700); err != nil {
		return fmt.Errorf("keystore: mkdir: %w", err)
	}
	if err := os.WriteFile(k.path, body, 0o600); err != nil {
		return fmt.Errorf("keystore: write: %w", err)
	}
	return nil
}

// load decrypts the on-disk keystore using k.passphrase.
func (k *LocalKeystore) load() error {
	body, err := os.ReadFile(k.path)
	if err != nil {
		return fmt.Errorf("keystore: read: %w", err)
	}
	var file keystoreFile
	if err := json.Unmarshal(body, &file); err != nil {
		return fmt.Errorf("keystore: parse: %w", err)
	}
	if file.Version != keystoreVersion {
		return fmt.Errorf("keystore: unsupported version %d", file.Version)
	}
	if file.KDF.Name != "scrypt" {
		return fmt.Errorf("keystore: unsupported KDF %q", file.KDF.Name)
	}
	salt, err := hex.DecodeString(file.KDF.Salt)
	if err != nil {
		return fmt.Errorf("keystore: salt decode: %w", err)
	}
	nonce, err := hex.DecodeString(file.Nonce)
	if err != nil {
		return fmt.Errorf("keystore: nonce decode: %w", err)
	}
	ct, err := hex.DecodeString(file.Ciphertext)
	if err != nil {
		return fmt.Errorf("keystore: ciphertext decode: %w", err)
	}
	dk, err := scrypt.Key([]byte(k.passphrase), salt, file.KDF.N, file.KDF.R, file.KDF.P, keyLen)
	if err != nil {
		return fmt.Errorf("keystore: scrypt: %w", err)
	}
	gcm, err := newGCM(dk)
	if err != nil {
		return err
	}
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return fmt.Errorf("keystore: decrypt failed (wrong passphrase?)")
	}
	var blob secretsBlob
	if err := json.Unmarshal(plain, &blob); err != nil {
		return fmt.Errorf("keystore: blob decode: %w", err)
	}

	k.mu.Lock()
	defer k.mu.Unlock()
	k.addrs = file.Addresses
	if blob.Solana != nil {
		s, err := NewSolanaSignerFromPrivate(blob.Solana)
		if err != nil {
			return fmt.Errorf("keystore: load solana: %w", err)
		}
		k.signers[types.ChainSolana] = s
	}
	if blob.Hyperliquid != nil {
		s, err := NewHyperliquidSignerFromPrivate(blob.Hyperliquid)
		if err != nil {
			return fmt.Errorf("keystore: load hyperliquid: %w", err)
		}
		k.signers[types.ChainHyperliquid] = s
	}
	// Ensure addresses recorded match the loaded keys; refresh if stale.
	for chain, s := range k.signers {
		k.upsertAddrLocked(addressEntry{Chain: chain, Address: s.Address()})
	}
	return nil
}

func (k *LocalKeystore) upsertAddrLocked(e addressEntry) {
	for i, a := range k.addrs {
		if a.Chain == e.Chain {
			k.addrs[i] = e
			return
		}
	}
	k.addrs = append(k.addrs, e)
}

// GenerateSolanaKey makes a fresh ed25519 keypair via crypto/rand.
func GenerateSolanaKey() (*SolanaSigner, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return NewSolanaSignerFromPrivate(priv)
}

// GenerateHyperliquidKey makes a fresh secp256k1 keypair via crypto/rand.
func GenerateHyperliquidKey() (*HyperliquidSigner, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	return NewHyperliquidSignerFromPrivate(raw)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("keystore: aes: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("keystore: gcm: %w", err)
	}
	return gcm, nil
}

// Compile-time check.
var _ Keystore = (*LocalKeystore)(nil)
