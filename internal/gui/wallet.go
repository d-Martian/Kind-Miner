package gui

import "strings"

// base58 alphabet used by Monero addresses (Bitcoin alphabet — no 0/O/I/l).
const base58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// ValidateAddress performs cheap, syntactic checks on a Monero address.
// It does not verify the network checksum (that would need full base58 decode
// and CryptoNote keccak); the goal is only to catch typos and wrong-currency
// pastes before the miner is launched.
//
// Returns the matching constant from copy.go, or "" if the address looks good.
func ValidateAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return WalletErrEmpty
	}
	if !(strings.HasPrefix(addr, "4") || strings.HasPrefix(addr, "8")) {
		return WalletErrPrefix
	}
	// Standard addresses are 95 chars. Integrated addresses are 106. Accept
	// both — anything else is almost certainly a typo or a partial paste.
	if len(addr) != 95 && len(addr) != 106 {
		return WalletErrLength
	}
	for _, r := range addr {
		if !strings.ContainsRune(base58, r) {
			return WalletErrCharset
		}
	}
	return ""
}
