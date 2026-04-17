// Package hyperliquid implements the exchange.Venue interface against the
// Hyperliquid REST and WebSocket APIs.
//
// The adapter is split across:
//   - config.go  — Network selection (mainnet/testnet) and Config.
//   - api.go     — JSON request/response shapes.
//   - client.go  — Thin POST client for /info and /exchange.
//   - venue.go   — Venue implementation (read-only operations).
//   - sign.go    — Action signing helpers; full EIP-712 signing wires up
//                  in M4 once the wallet package ships real signers.
//   - ws.go      — WebSocket subscription manager.
package hyperliquid

import (
	"errors"
	"fmt"
	"strings"
)

// VenueName is the stable identifier under which this adapter is registered.
const VenueName = "hyperliquid"

// Network selects mainnet vs testnet endpoints.
type Network string

const (
	NetworkMainnet Network = "mainnet"
	NetworkTestnet Network = "testnet"
)

// Endpoints are the per-network REST and WebSocket URLs.
type Endpoints struct {
	REST string
	WS   string
}

// EndpointsFor returns the endpoint URLs for the given network.
func EndpointsFor(n Network) (Endpoints, error) {
	switch n {
	case NetworkMainnet:
		return Endpoints{
			REST: "https://api.hyperliquid.xyz",
			WS:   "wss://api.hyperliquid.xyz/ws",
		}, nil
	case NetworkTestnet, "":
		return Endpoints{
			REST: "https://api.hyperliquid-testnet.xyz",
			WS:   "wss://api.hyperliquid-testnet.xyz/ws",
		}, nil
	default:
		return Endpoints{}, fmt.Errorf("unknown hyperliquid network %q", n)
	}
}

// Config configures a Hyperliquid Venue. Address is the user account address
// (0x-prefixed hex). Network selects mainnet vs testnet. RESTOverride and
// WSOverride allow tests and private gateways to redirect traffic.
type Config struct {
	Network      Network
	Address      string
	RESTOverride string
	WSOverride   string
}

func (c Config) endpoints() (Endpoints, error) {
	ep, err := EndpointsFor(c.Network)
	if err != nil {
		return ep, err
	}
	if c.RESTOverride != "" {
		ep.REST = strings.TrimRight(c.RESTOverride, "/")
	}
	if c.WSOverride != "" {
		ep.WS = c.WSOverride
	}
	return ep, nil
}

// validate checks the config has the minimum required fields.
func (c Config) validate() error {
	if c.Address == "" {
		return errors.New("hyperliquid: Address is required")
	}
	if !strings.HasPrefix(c.Address, "0x") || len(c.Address) != 42 {
		return fmt.Errorf("hyperliquid: Address %q must be 0x-prefixed 42-char hex", c.Address)
	}
	return nil
}
