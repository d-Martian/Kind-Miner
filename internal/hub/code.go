package hub

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
)

// Pairing is everything a device needs to mine to a hub and trust it: where
// it is, the certificate to expect, and the token for its statistics.
type Pairing struct {
	Host        string
	StratumPort int
	APIPort     int
	Fingerprint [32]byte
	Token       string
	// Onion is the hub's onion service ("….onion"), empty when it has none.
	// It serves the same two ports.
	Onion string
}

// StratumAddr is where xmrig connects.
func (p Pairing) StratumAddr() string {
	return net.JoinHostPort(p.Host, strconv.Itoa(p.StratumPort))
}

// APIAddr is where the household statistics are served.
func (p Pairing) APIAddr() string {
	return net.JoinHostPort(p.Host, strconv.Itoa(p.APIPort))
}

// OnionStratumAddr is where xmrig connects through Tor; "" without an onion.
func (p Pairing) OnionStratumAddr() string {
	if p.Onion == "" {
		return ""
	}
	return net.JoinHostPort(p.Onion, strconv.Itoa(p.StratumPort))
}

// OnionAPIAddr is where the statistics are served through Tor.
func (p Pairing) OnionAPIAddr() string {
	if p.Onion == "" {
		return ""
	}
	return net.JoinHostPort(p.Onion, strconv.Itoa(p.APIPort))
}

// FingerprintHex is the fingerprint as xmrig wants it.
func (p Pairing) FingerprintHex() string { return hex.EncodeToString(p.Fingerprint[:]) }

// PairingFor is the pairing a hub at host hands out; onion may be "".
func (id Identity) PairingFor(host, onion string, stratumPort, apiPort int) Pairing {
	return Pairing{Host: host, StratumPort: stratumPort, APIPort: apiPort, Fingerprint: id.Fingerprint, Token: id.Token, Onion: onion}
}

// The code's format is in its prefix. km1 is the LAN-only pairing; km2 adds
// the onion's 32-byte key after the ports. A hub with no onion still hands out
// km1, which every paired device already understands.
const (
	codePrefix      = "km1-"
	codePrefixOnion = "km2-"
)

// Code packs a pairing into one line to copy from an SSH session into the
// desktop's settings: the fixed-size parts first, the host last, base64url so
// nothing in it needs quoting in a shell or YAML.
func (p Pairing) Code() string {
	token, _ := hex.DecodeString(p.Token)
	buf := make([]byte, 0, 32+tokenLen+4+32+len(p.Host))
	buf = append(buf, p.Fingerprint[:]...)
	buf = append(buf, token...)
	buf = binary.BigEndian.AppendUint16(buf, uint16(p.StratumPort))
	buf = binary.BigEndian.AppendUint16(buf, uint16(p.APIPort))
	prefix := codePrefix
	if pub, err := onionKey(p.Onion); err == nil {
		prefix = codePrefixOnion
		buf = append(buf, pub[:]...)
	}
	buf = append(buf, p.Host...)
	return prefix + base64.RawURLEncoding.EncodeToString(buf)
}

// ErrBadCode is returned for anything that is not a pairing code.
var ErrBadCode = errors.New("not a kind-miner pairing code")

// ParseCode reads a code made by Code. Whitespace around it is forgiven — it
// was copied out of a terminal.
func ParseCode(s string) (Pairing, error) {
	s = strings.TrimSpace(s)
	withOnion := strings.HasPrefix(s, codePrefixOnion)
	if !withOnion && !strings.HasPrefix(s, codePrefix) {
		return Pairing{}, fmt.Errorf("%w (it should start with %s or %s)", ErrBadCode, codePrefix, codePrefixOnion)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[len(codePrefix):])
	fixed := 32 + tokenLen + 4
	if withOnion {
		fixed += 32
	}
	if err != nil || len(raw) <= fixed {
		return Pairing{}, fmt.Errorf("%w (it looks cut short; copy the whole line)", ErrBadCode)
	}
	var p Pairing
	copy(p.Fingerprint[:], raw[:32])
	p.Token = hex.EncodeToString(raw[32 : 32+tokenLen])
	p.StratumPort = int(binary.BigEndian.Uint16(raw[32+tokenLen:]))
	p.APIPort = int(binary.BigEndian.Uint16(raw[32+tokenLen+2:]))
	if withOnion {
		var pub [32]byte
		copy(pub[:], raw[32+tokenLen+4:])
		p.Onion = onionAddress(pub)
	}
	p.Host = string(raw[fixed:])
	if p.StratumPort == 0 || p.APIPort == 0 || !validHost(p.Host) {
		return Pairing{}, fmt.Errorf("%w (its address is damaged; copy the whole line)", ErrBadCode)
	}
	return p, nil
}

// validHost accepts an IP address or a DNS name, nothing that would change
// the meaning of host:port.
func validHost(h string) bool {
	if net.ParseIP(h) != nil {
		return true
	}
	if h == "" || len(h) > 253 {
		return false
	}
	for _, c := range h {
		if !(c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}
