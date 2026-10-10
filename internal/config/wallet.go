package config

import (
	"bytes"
	"encoding/binary"
	"errors"
	"math/bits"
	"strings"
)

// The checks on a Monero payout address, shared by the GUI, kind-minerd's
// init and the hub's setup API, and made again before p2pool starts. They
// include the checksum: p2pool 4.18 given an address whose checksum fails
// aborts at once, before it has written a line, so the only sign of a
// one-character typo was "p2pool exited before becoming ready and produced
// no output".
var (
	ErrAddressEmpty      = errors.New("address is required")
	ErrAddressPrefix     = errors.New("address must start with 4")
	ErrAddressLength     = errors.New("address should be 95 characters long")
	ErrAddressCharset    = errors.New("address contains invalid characters")
	ErrAddressChecksum   = errors.New("address has a typo: its checksum does not match")
	ErrAddressSubaddress = errors.New("that is a subaddress, which cannot receive P2Pool payouts: use the wallet's primary address, which starts with 4")
	ErrAddressIntegrated = errors.New("that is an integrated address, which cannot receive P2Pool payouts: use the wallet's primary address, 95 characters long")
)

// base58 is the alphabet Monero addresses use (Bitcoin's — no 0, O, I or l).
const base58 = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// CheckAddress returns nil for an address P2Pool can pay — a mainnet primary
// address with a correct checksum — or the error naming what is wrong with
// it. Subaddresses and integrated addresses are real addresses, but P2Pool
// pays in the coinbase, which only a primary address can receive, and
// p2pool 4.18 aborts on either just as it does on a typo; they get a message
// of their own, because a wallet often shows one of them first.
func CheckAddress(addr string) error {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ErrAddressEmpty
	}
	if !(strings.HasPrefix(addr, "4") || strings.HasPrefix(addr, "8")) {
		return ErrAddressPrefix
	}
	// Primary addresses and subaddresses are 95 characters, integrated ones
	// 106 (let through here only to be named below). Anything else is a typo
	// or a partial paste.
	if len(addr) != 95 && len(addr) != 106 {
		return ErrAddressLength
	}
	for _, r := range addr {
		if !strings.ContainsRune(base58, r) {
			return ErrAddressCharset
		}
	}
	raw, ok := decodeBase58(addr)
	if !ok || len(raw) < 5 {
		return ErrAddressChecksum
	}
	body, sum := raw[:len(raw)-4], raw[len(raw)-4:]
	if h := keccak256(body); !bytes.Equal(h[:4], sum) {
		return ErrAddressChecksum
	}
	// The checksum holds, so the first byte is the tag the wallet wrote: the
	// network, and the kind of address. Mainnet's are one-byte varints.
	switch body[0] {
	case 18:
		return nil
	case 42:
		return ErrAddressSubaddress
	case 19:
		return ErrAddressIntegrated
	default:
		return ErrAddressPrefix
	}
}

// decodeBase58 decodes Monero's base58, which is not Bitcoin's: the input is
// cut into 11-character blocks that each encode 8 bytes, and a shorter last
// block encodes fewer, by the table below. ok is false for a length no
// block size makes, or a block whose value overflows its bytes.
func decodeBase58(s string) (out []byte, ok bool) {
	const fullBlock, fullEncoded = 8, 11
	// bytesFor[n] is how many bytes a final block of n characters holds.
	bytesFor := map[int]int{2: 1, 3: 2, 5: 3, 6: 4, 7: 5, 9: 6, 10: 7, 11: 8}
	for len(s) > 0 {
		n := min(len(s), fullEncoded)
		size, valid := bytesFor[n]
		if !valid {
			return nil, false
		}
		var v uint64
		for _, r := range s[:n] {
			digit := uint64(strings.IndexRune(base58, r))
			hi, lo := bits.Mul64(v, 58)
			lo, carry := bits.Add64(lo, digit, 0)
			if hi != 0 || carry != 0 {
				return nil, false
			}
			v = lo
		}
		if size < fullBlock && v>>(8*size) != 0 {
			return nil, false
		}
		var block [fullBlock]byte
		binary.BigEndian.PutUint64(block[:], v)
		out = append(out, block[fullBlock-size:]...)
		s = s[n:]
	}
	return out, true
}
