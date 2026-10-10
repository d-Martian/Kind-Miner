package config

import (
	"encoding/hex"
	"strings"
	"testing"
)

func TestKeccak256(t *testing.T) {
	// Reference values from golang.org/x/crypto/sha3.NewLegacyKeccak256 —
	// Keccak-256 as Monero uses it, not SHA3-256 — including inputs either
	// side of the 136-byte block.
	cases := []struct{ name, in, want string }{
		{"the empty string", "", "c5d2460186f7233c927e7db2dcc703c0e500b653ca82273b7bfad8045d85a470"},
		{"a short message", "abc", "4e03657aea45a94fc7d47ba826c8d667c0d1e6e33a64a036ec44f58fa12d6c45"},
		{"one byte short of a block", strings.Repeat("a", 135), "34367dc248bbd832f4e3e69dfaac2f92638bd0bbd18f2912ba4ef454919cf446"},
		{"exactly one block", strings.Repeat("a", 136), "a6c4d403279fe3e0af03729caada8374b5ca54d8065329a3ebcaeb4b60aa386e"},
		{"more than a block", strings.Repeat("a", 200), "96ea54061def936c4be90b518992fdc6f12f535068a256229aca54267b4d084d"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := keccak256([]byte(c.in))
			if h := hex.EncodeToString(got[:]); h != c.want {
				t.Errorf("keccak256 = %s, want %s", h, c.want)
			}
		})
	}
}
