package config

import (
	"errors"
	"strings"
)

// The syntactic checks on a Monero payout address, shared by the GUI's
// onboarding and kind-minerd's init. They catch typos and wrong-currency
// pastes; they do not verify the checksum, which needs a full base58 decode
// and Keccak.
var (
	ErrAddressEmpty   = errors.New("address is required")
	ErrAddressPrefix  = errors.New("address must start with 4 or 8")
	ErrAddressLength  = errors.New("address should be 95 characters long")
	ErrAddressCharset = errors.New("address contains invalid characters")
)

// base58 is the alphabet Monero addresses use (Bitcoin's — no 0, O, I or l).
const base58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// CheckAddress returns nil for an address that looks like a Monero address, or
// the error naming what is wrong with it.
func CheckAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ErrAddressEmpty
	}
	if !(strings.HasPrefix(addr, "4") || strings.HasPrefix(addr, "8")) {
		return ErrAddressPrefix
	}
	// Standard addresses are 95 characters and integrated ones 106. Anything
	// else is almost certainly a typo or a partial paste.
	if len(addr) != 95 && len(addr) != 106 {
		return ErrAddressLength
	}
	for _, r := range addr {
		if !strings.ContainsRune(base58, r) {
			return ErrAddressCharset
		}
	}
	return nil
}
