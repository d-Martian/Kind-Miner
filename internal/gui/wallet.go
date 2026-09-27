package gui

import "github.com/kind-miner/kind-miner/internal/config"

// ValidateAddress performs cheap, syntactic checks on a Monero address — see
// config.CheckAddress — and returns the matching message from copy.go, or ""
// if the address looks good.
func ValidateAddress(addr string) string {
	switch config.CheckAddress(addr) {
	case nil:
		return ""
	case config.ErrAddressEmpty:
		return WalletErrEmpty
	case config.ErrAddressPrefix:
		return WalletErrPrefix
	case config.ErrAddressLength:
		return WalletErrLength
	default:
		return WalletErrCharset
	}
}
