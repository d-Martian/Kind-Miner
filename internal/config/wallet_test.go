package config

import (
	"encoding/binary"
	"errors"
	"testing"
)

// generalFund is the Monero General Fund's published donation address: a
// real mainnet standard address, so its checksum is right.
const generalFund = "44AFFq5kSiGBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGQBEP3A"

// encodeBase58 is decodeBase58's inverse, for building addresses no wallet
// has made: other tags, payment IDs, broken checksums.
func encodeBase58(b []byte) string {
	charsFor := [9]int{0, 2, 3, 5, 6, 7, 9, 10, 11}
	var s []byte
	for len(b) > 0 {
		n := min(len(b), 8)
		var block [8]byte
		copy(block[8-n:], b[:n])
		v := binary.BigEndian.Uint64(block[:])
		enc := make([]byte, charsFor[n])
		for i := len(enc) - 1; i >= 0; i-- {
			enc[i] = base58[v%58]
			v /= 58
		}
		s = append(s, enc...)
		b = b[n:]
	}
	return string(s)
}

// withChecksum appends Monero's checksum to an address body and encodes it.
func withChecksum(body []byte) string {
	h := keccak256(body)
	return encodeBase58(append(append([]byte{}, body...), h[:4]...))
}

func generalFundBody(t *testing.T) []byte {
	t.Helper()
	raw, ok := decodeBase58(generalFund)
	if !ok || len(raw) != 69 {
		t.Fatalf("the General Fund address decoded to %d bytes, ok %v", len(raw), ok)
	}
	return raw[:65]
}

func TestBase58RoundTrip(t *testing.T) {
	raw, _ := decodeBase58(generalFund)
	if got := encodeBase58(raw); got != generalFund {
		t.Errorf("round trip = %s", got)
	}
}

func TestCheckAddress(t *testing.T) {
	body := generalFundBody(t)
	retag := func(tag byte, extra ...byte) []byte {
		b := append([]byte{tag}, body[1:]...)
		return append(b, extra...)
	}
	// One character changed: the kind of typo the checksum is there for.
	typo := []byte(generalFund)
	typo[50] = map[bool]byte{true: 'b', false: 'a'}[typo[50] == 'a']

	cases := []struct {
		name string
		addr string
		want error
	}{
		{"a real standard address", generalFund, nil},
		{"surrounding whitespace from a paste", "  " + generalFund + "\n", nil},
		// Real addresses, but p2pool 4.18 aborts on both, as on a typo.
		{"an integrated address", withChecksum(retag(19, 1, 2, 3, 4, 5, 6, 7, 8)), ErrAddressIntegrated},
		{"a subaddress", withChecksum(retag(42)), ErrAddressSubaddress},
		{"a one-character typo", string(typo), ErrAddressChecksum},
		{"an address that only looks right", "4At3X5rvVypTofgmueN9s9QtrzdRe5BueFrskAZi17BoYbhzysozzoMFB6zWnTKdGC6AxEAbEE5czFR3hbEEJbsm4hVwCJk", ErrAddressChecksum},
		{"another network's tag with a valid checksum", withChecksum(retag(17)), ErrAddressPrefix},
		{"empty", "", ErrAddressEmpty},
		{"not Monero", "bc1qar0srrr7xfkvy5l643lydnw9re59gtzzwf5mdq", ErrAddressPrefix},
		{"cut short", generalFund[:94], ErrAddressLength},
		{"a character base58 leaves out", "0" + generalFund[1:], ErrAddressPrefix},
		{"a character base58 leaves out, inside", generalFund[:40] + "l" + generalFund[41:], ErrAddressCharset},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CheckAddress(c.addr); !errors.Is(got, c.want) {
				t.Errorf("CheckAddress(%q) = %v, want %v", c.addr, got, c.want)
			}
		})
	}
}

func TestDecodeBase58RefusesWhatNoAddressCanBe(t *testing.T) {
	cases := []struct{ name, in string }{
		{"a final block of an impossible length", "1111"},
		{"a full block that overflows 8 bytes", "zzzzzzzzzzz"},
		{"a short block that overflows its bytes", "zz"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, ok := decodeBase58(c.in); ok {
				t.Errorf("decodeBase58(%q) succeeded", c.in)
			}
		})
	}
}
