package hub

import (
	"crypto/sha3"
	"encoding/base32"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The hub's stratum and statistics are also published as a Tor onion
// service, so a laptop paired at home keeps mining to the hub from anywhere:
// xmrig reaches the onion through the laptop's Tor, and TLS still runs end to
// end to p2pool, so the certificate pin holds on that path too.

// onionVersion is the v3 onion address version byte.
const onionVersion = 0x03

var onionEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// onionAddress is the v3 address of an onion service's ed25519 public key:
// base32(pubkey | checksum | version) + ".onion", where the checksum is the
// first two bytes of SHA3-256(".onion checksum" | pubkey | version) — Tor's
// rend-spec-v3.
func onionAddress(pub [32]byte) string {
	raw := append(append(pub[:0:0], pub[:]...), onionChecksum(pub)...)
	raw = append(raw, onionVersion)
	return strings.ToLower(onionEncoding.EncodeToString(raw)) + ".onion"
}

func onionChecksum(pub [32]byte) []byte {
	h := sha3.New256()
	h.Write([]byte(".onion checksum"))
	h.Write(pub[:])
	h.Write([]byte{onionVersion})
	return h.Sum(nil)[:2]
}

// errBadOnion is returned for anything that is not a v3 onion address.
var errBadOnion = errors.New("not a v3 onion address")

// onionKey returns the public key a v3 onion address encodes, checking its
// checksum, so a pairing code never carries a mistyped address.
func onionKey(addr string) ([32]byte, error) {
	var pub [32]byte
	name, ok := strings.CutSuffix(strings.ToLower(strings.TrimSpace(addr)), ".onion")
	if !ok || len(name) != 56 {
		return pub, errBadOnion
	}
	raw, err := onionEncoding.DecodeString(strings.ToUpper(name))
	if err != nil || len(raw) != 35 || raw[34] != onionVersion {
		return pub, errBadOnion
	}
	copy(pub[:], raw[:32])
	if sum := onionChecksum(pub); raw[32] != sum[0] || raw[33] != sum[1] {
		return pub, errBadOnion
	}
	return pub, nil
}

// OnionDir is where the hub's onion service keeps its key, inside the hub
// directory: the address is the household's to keep, so it lives and is
// purged with the certificate that goes with it.
func OnionDir(hubDir string) string { return filepath.Join(hubDir, "onion") }

// ReadOnion returns the hub's onion address, which Tor writes to the
// service's hostname file the first time it runs with it. Empty, with no
// error, when there is none yet.
func ReadOnion(hubDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(OnionDir(hubDir), "hostname"))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	addr := strings.TrimSpace(string(data))
	if _, err := onionKey(addr); err != nil {
		return "", err
	}
	return addr, nil
}
