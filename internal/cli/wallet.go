package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/teslashibe/permafrost/pkg/types"
	"github.com/teslashibe/permafrost/internal/wallet"
)

func init() { addCommandFactory(newWalletCmd) }

func newWalletCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wallet",
		Short: "Manage the local encrypted keystore (Solana + Hyperliquid + Bittensor)",
	}
	cmd.AddCommand(
		newWalletShowCmd(),
		newWalletGenerateCmd(),
		newWalletImportCmd(),
		newWalletPathCmd(),
	)
	return cmd
}

func newWalletShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "show",
		Aliases: []string{"list"},
		Short:   "List addresses in the keystore (no passphrase required)",
		RunE: func(c *cobra.Command, _ []string) error {
			path, err := keystorePath(c)
			if err != nil {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					fmt.Println("(no keystore at " + path + ")")
					return nil
				}
				return err
			}
			// Parse just the addresses section without unlocking. Use a
			// permissive unmarshaller so we ignore the encrypted fields.
			var public struct {
				Addresses []struct {
					Chain   string `json:"chain"`
					Address string `json:"address"`
				} `json:"addresses"`
			}
			if err := json.Unmarshal(body, &public); err != nil {
				return fmt.Errorf("parse keystore: %w", err)
			}
			if len(public.Addresses) == 0 {
				fmt.Println("(empty keystore)")
				return nil
			}
			for _, a := range public.Addresses {
				fmt.Printf("%-12s %s\n", a.Chain, a.Address)
			}
			return nil
		},
	}
}

func newWalletGenerateCmd() *cobra.Command {
	var chain string
	cmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate a fresh keypair on the given chain and add it to the keystore",
		RunE: func(c *cobra.Command, _ []string) error {
			ks, err := openKeystore(c)
			if err != nil {
				return err
			}
			switch types.ChainID(chain) {
			case types.ChainSolana:
				s, err := wallet.GenerateSolanaKey()
				if err != nil {
					return err
				}
				ks.SetSolana(s)
				fmt.Println("solana       " + s.Address())
		case types.ChainHyperliquid:
			s, err := wallet.GenerateHyperliquidKey()
			if err != nil {
				return err
			}
			ks.SetHyperliquid(s)
			fmt.Println("hyperliquid  " + s.Address())
		case types.ChainBittensor:
			s, err := wallet.GenerateBittensorKey()
			if err != nil {
				return err
			}
			ks.SetBittensor(s)
			fmt.Println("bittensor    " + s.Address())
			fmt.Println("\nFund this address with TAO before trading. Send TAO from")
			fmt.Println("an exchange (Coinbase, Kraken, MEXC) or another Bittensor wallet.")
		default:
			return fmt.Errorf("unknown chain %q (use solana | hyperliquid | bittensor)", chain)
		}
		return ks.Save()
		},
	}
	cmd.Flags().StringVar(&chain, "chain", "", "chain (solana | hyperliquid | bittensor)")
	_ = cmd.MarkFlagRequired("chain")
	return cmd
}

func newWalletImportCmd() *cobra.Command {
	var (
		chain string
		from  string
	)
	cmd := &cobra.Command{
		Use:   "import",
		Short: "Import a key from a file into the keystore",
		Long: `Import a key from a file:
  - solana:      a Phantom-style JSON array of 64 numbers, OR a base58
                  ed25519 secret key, OR raw 32/64-byte hex.
  - hyperliquid: a 32-byte hex private key (with or without 0x prefix).
`,
		RunE: func(c *cobra.Command, _ []string) error {
			ks, err := openKeystore(c)
			if err != nil {
				return err
			}
			body, err := os.ReadFile(from)
			if err != nil {
				return fmt.Errorf("read %s: %w", from, err)
			}
			text := strings.TrimSpace(string(body))

			switch types.ChainID(chain) {
			case types.ChainSolana:
				s, err := parseSolanaImport(text)
				if err != nil {
					return err
				}
				ks.SetSolana(s)
				fmt.Println("solana       " + s.Address())
		case types.ChainHyperliquid:
			s, err := wallet.NewHyperliquidSignerFromHex(text)
			if err != nil {
				return err
			}
			ks.SetHyperliquid(s)
			fmt.Println("hyperliquid  " + s.Address())
		case types.ChainBittensor:
			s, err := parseBittensorImport(text)
			if err != nil {
				return err
			}
			ks.SetBittensor(s)
			fmt.Println("bittensor    " + s.Address())
		default:
			return fmt.Errorf("unknown chain %q (use solana | hyperliquid | bittensor)", chain)
		}
		return ks.Save()
		},
	}
	cmd.Flags().StringVar(&chain, "chain", "", "chain (solana | hyperliquid | bittensor)")
	cmd.Flags().StringVar(&from, "from", "", "path to key file")
	_ = cmd.MarkFlagRequired("chain")
	_ = cmd.MarkFlagRequired("from")
	return cmd
}

func newWalletPathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the resolved keystore path",
		RunE: func(c *cobra.Command, _ []string) error {
			p, err := keystorePath(c)
			if err != nil {
				return err
			}
			fmt.Println(p)
			return nil
		},
	}
}

// ─── helpers ────────────────────────────────────────────────────────────────

func openKeystore(c *cobra.Command) (*wallet.LocalKeystore, error) {
	path, err := keystorePath(c)
	if err != nil {
		return nil, err
	}
	pass, err := keystorePassphrase(c)
	if err != nil {
		return nil, err
	}
	return wallet.NewLocalKeystore(path, pass)
}

func keystorePath(c *cobra.Command) (string, error) {
	g := FromContext(c.Context())
	if g == nil {
		return "", errors.New("globals not initialised")
	}
	if g.Config.Wallet.KeystorePath != "" {
		return g.Config.Wallet.KeystorePath, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home: %w", err)
	}
	return filepath.Join(home, ".permafrost", "keystore.json"), nil
}

func keystorePassphrase(c *cobra.Command) (string, error) {
	g := FromContext(c.Context())
	if g == nil {
		return "", errors.New("globals not initialised")
	}
	envName := g.Config.Wallet.PassphraseEnv
	if envName == "" {
		envName = "PERMAFROST_KEYSTORE_PASSPHRASE"
	}
	pass := os.Getenv(envName)
	if pass == "" {
		return "", fmt.Errorf("set %s to unlock the keystore", envName)
	}
	return pass, nil
}

// parseSolanaImport accepts either:
//   - a JSON array of numbers (Phantom export, 32 or 64 entries), or
//   - a base58-encoded ed25519 secret, or
//   - hex-encoded 32 or 64 bytes.
func parseSolanaImport(text string) (*wallet.SolanaSigner, error) {
	if strings.HasPrefix(text, "[") {
		var nums []int
		if err := jsonStrictUnmarshal([]byte(text), &nums); err != nil {
			return nil, fmt.Errorf("parse JSON array: %w", err)
		}
		bs := make([]byte, len(nums))
		for i, n := range nums {
			if n < 0 || n > 255 {
				return nil, fmt.Errorf("byte %d out of range: %d", i, n)
			}
			bs[i] = byte(n)
		}
		return wallet.NewSolanaSignerFromPrivate(bs)
	}
	if hexish(text) {
		return solanaFromHex(text)
	}
	return wallet.NewSolanaSignerFromBase58(text)
}

func hexish(s string) bool {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	if len(s) != 64 && len(s) != 128 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func solanaFromHex(s string) (*wallet.SolanaSigner, error) {
	s = strings.TrimPrefix(strings.TrimPrefix(s, "0x"), "0X")
	raw, err := hexDecodeStrict(s)
	if err != nil {
		return nil, err
	}
	return wallet.NewSolanaSignerFromPrivate(raw)
}

func hexDecodeStrict(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("hex: odd-length string")
	}
	raw := make([]byte, len(s)/2)
	for i := 0; i < len(raw); i++ {
		hi, ok1 := unhex(s[2*i])
		lo, ok2 := unhex(s[2*i+1])
		if !ok1 || !ok2 {
			return nil, fmt.Errorf("hex: invalid byte at %d", i)
		}
		raw[i] = (hi << 4) | lo
	}
	return raw, nil
}

func unhex(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// parseBittensorImport accepts:
//   - a 32-byte mini-secret seed in hex (with or without 0x prefix), or
//   - a BIP-39 mnemonic phrase (12+ words separated by spaces).
func parseBittensorImport(text string) (*wallet.BittensorSigner, error) {
	if hexish(text) {
		s := strings.TrimPrefix(strings.TrimPrefix(text, "0x"), "0X")
		raw, err := hexDecodeStrict(s)
		if err != nil {
			return nil, err
		}
		return wallet.NewBittensorSignerFromPrivate(raw)
	}
	// Treat as mnemonic phrase.
	if strings.Count(text, " ") >= 11 { // 12-word mnemonic minimum
		return wallet.NewBittensorSignerFromPhrase(text, "")
	}
	return nil, errors.New("bittensor: input must be 32-byte hex seed or BIP-39 mnemonic phrase")
}

// jsonStrictUnmarshal parses JSON and surfaces unknown-field errors. Used
// for both keystore-public-section parsing and Phantom-style array imports.
func jsonStrictUnmarshal(b []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
