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
}

// StratumAddr is where xmrig connects.
func (p Pairing) StratumAddr() string {
	return net.JoinHostPort(p.Host, strconv.Itoa(p.StratumPort))
}

// APIAddr is where the household statistics are served.
func (p Pairing) APIAddr() string {
	return net.JoinHostPort(p.Host, strconv.Itoa(p.APIPort))
}

// FingerprintHex is the fingerprint as xmrig wants it.
func (p Pairing) FingerprintHex() string { return hex.EncodeToString(p.Fingerprint[:]) }

// PairingFor is the pairing a hub at host hands out.
func (id Identity) PairingFor(host string, stratumPort, apiPort int) Pairing {
	return Pairing{Host: host, StratumPort: stratumPort, APIPort: apiPort, Fingerprint: id.Fingerprint, Token: id.Token}
}

// codePrefix marks the code's format, so a later one can be told apart.
const codePrefix = "km1-"

// Code packs a pairing into one line to copy from an SSH session into the
// desktop's settings: the fixed-size parts first, the host last, base64url so
// nothing in it needs quoting in a shell or YAML.
func (p Pairing) Code() string {
	token, _ := hex.DecodeString(p.Token)
	buf := make([]byte, 0, 32+tokenLen+4+len(p.Host))
	buf = append(buf, p.Fingerprint[:]...)
	buf = append(buf, token...)
	buf = binary.BigEndian.AppendUint16(buf, uint16(p.StratumPort))
	buf = binary.BigEndian.AppendUint16(buf, uint16(p.APIPort))
	buf = append(buf, p.Host...)
	return codePrefix + base64.RawURLEncoding.EncodeToString(buf)
}

// ErrBadCode is returned for anything that is not a pairing code.
var ErrBadCode = errors.New("not a kind-miner pairing code")

// ParseCode reads a code made by Code. Whitespace around it is forgiven — it
// was copied out of a terminal.
func ParseCode(s string) (Pairing, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, codePrefix) {
		return Pairing{}, fmt.Errorf("%w (it should start with %s)", ErrBadCode, codePrefix)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s[len(codePrefix):])
	const fixed = 32 + tokenLen + 4
	if err != nil || len(raw) <= fixed {
		return Pairing{}, fmt.Errorf("%w (it looks cut short; copy the whole line)", ErrBadCode)
	}
	var p Pairing
	copy(p.Fingerprint[:], raw[:32])
	p.Token = hex.EncodeToString(raw[32 : 32+tokenLen])
	p.StratumPort = int(binary.BigEndian.Uint16(raw[32+tokenLen:]))
	p.APIPort = int(binary.BigEndian.Uint16(raw[32+tokenLen+2:]))
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
