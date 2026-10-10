package gui

import "github.com/kind-miner/kind-miner/internal/config"

// ValidateAddress checks a Monero payout address — see config.CheckAddress —
// and returns the matching message from copy.go, or "" if P2Pool can pay it.
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
	case config.ErrAddressChecksum:
		return WalletErrChecksum
	case config.ErrAddressSubaddress:
		return WalletErrSubaddress
	case config.ErrAddressIntegrated:
		return WalletErrIntegrated
	default:
		return WalletErrCharset
	}
}
