package advisory

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// minisign's formats, verified with the standard library alone. Only the
// legacy algorithm is accepted — Ed25519 over the whole file, which
// `minisign -S -l` makes — because the default one signs a BLAKE2b hash of
// the file, and BLAKE2b would be kind-miner's first golang.org/x/crypto
// dependency. A manifest is a few hundred bytes; hashing it first buys
// nothing.
//
//	public key:  base64("Ed" ‖ key id[8] ‖ public key[32])
//	signature:   untrusted comment line
//	             base64("Ed" ‖ key id[8] ‖ Ed25519(file)[64])
//	             "trusted comment: " text
//	             base64(Ed25519(signature[64] ‖ text)[64])

// PublicKey is a minisign public key.
type PublicKey struct {
	ID  [8]byte
	Key ed25519.PublicKey
}

// ErrBadSignature covers every way a signature can fail to vouch for a file.
var ErrBadSignature = errors.New("the advisory's signature does not verify")

// ParsePublicKey reads a minisign public key: the base64 line on its own, or
// a .pub file with its comment line.
func ParsePublicKey(text string) (PublicKey, error) {
	var line string
	for _, l := range strings.Split(strings.TrimSpace(text), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "untrusted comment:") {
			line = l
		}
	}
	raw, err := base64.StdEncoding.DecodeString(line)
	if err != nil || len(raw) != 2+8+ed25519.PublicKeySize || string(raw[:2]) != "Ed" {
		return PublicKey{}, errors.New("not a minisign public key")
	}
	var pk PublicKey
	copy(pk.ID[:], raw[2:10])
	pk.Key = ed25519.PublicKey(append([]byte(nil), raw[10:]...))
	return pk, nil
}

// Verify checks sig, a .minisig file's contents, against msg, and returns
// its trusted comment.
func (pk PublicKey) Verify(msg []byte, sig string) (trusted string, err error) {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(sig), "\r\n", "\n"), "\n")
	if len(lines) != 4 || !strings.HasPrefix(lines[2], "trusted comment: ") {
		return "", fmt.Errorf("%w: not a minisign signature", ErrBadSignature)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[1]))
	if err != nil || len(raw) != 2+8+ed25519.SignatureSize {
		return "", fmt.Errorf("%w: not a minisign signature", ErrBadSignature)
	}
	switch string(raw[:2]) {
	case "Ed":
	case "ED":
		return "", fmt.Errorf("%w: signed with a BLAKE2b prehash; sign with minisign -l", ErrBadSignature)
	default:
		return "", fmt.Errorf("%w: unknown algorithm", ErrBadSignature)
	}
	if !bytes.Equal(raw[2:10], pk.ID[:]) {
		return "", fmt.Errorf("%w: signed by a different key", ErrBadSignature)
	}
	s := raw[10:]
	if !ed25519.Verify(pk.Key, msg, s) {
		return "", ErrBadSignature
	}
	trusted = strings.TrimPrefix(lines[2], "trusted comment: ")
	global, err := base64.StdEncoding.DecodeString(strings.TrimSpace(lines[3]))
	if err != nil || !ed25519.Verify(pk.Key, append(append([]byte(nil), s...), trusted...), global) {
		return "", fmt.Errorf("%w: its trusted comment was altered", ErrBadSignature)
	}
	return trusted, nil
}
