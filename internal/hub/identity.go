// Package hub lets one machine — typically a Nodo — run p2pool for the whole
// household, and every other machine mine to it with xmrig alone.
//
// The hub serves two things on the LAN: p2pool's stratum port over TLS, and a
// small HTTPS API with the household's statistics. Both present the same
// self-signed certificate, and a device trusts the hub by that certificate's
// fingerprint, which it learns from a pairing code the owner copies off the
// hub. There is no CA to ask: a home LAN has none, and a fingerprint pinned at
// pairing is stronger than one anyway. What it stops is a device on the same
// network answering as the hub — a phone on the guest Wi-Fi claiming
// moneronodo.lan — and taking the household's hashrate for its own wallet.
//
// One hub pays one wallet. Everyone who pairs mines to the hub owner's address.
package hub

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Default ports, inside the 18080–18090 block Monero software already lives
// in, so a firewall rule the owner wrote for their node likely covers them.
// 18080, 18081, 18083 and 18089 are monerod's and Nodo's; these are not.
const (
	DefaultStratumPort = 18087
	DefaultAPIPort     = 18088
)

// Identity is the hub's certificate and API token, as files p2pool and the
// API server read, plus the values a pairing code carries.
type Identity struct {
	CertPath, KeyPath string
	// Fingerprint is the SHA-256 of the certificate's DER encoding: exactly
	// what xmrig's tls-fingerprint compares against.
	Fingerprint [32]byte
	// Token authorises the statistics API. It is not a secret of the mining
	// itself — anyone on the LAN can already mine to the hub, which only pays
	// the owner — but the API lists every device in the house.
	Token string
	// Owner authorises changing the hub's config: its wallet. It is made when
	// a desktop sets the hub up, and only that desktop gets it — the pairing
	// code carries Token alone, so a housemate who paired a laptop cannot
	// redirect the household's earnings. Empty for a hub set up over SSH,
	// whose config is root's file and not the API's to change.
	Owner string

	dir string
}

// FingerprintHex is the fingerprint as xmrig wants it.
func (id Identity) FingerprintHex() string { return hex.EncodeToString(id.Fingerprint[:]) }

const (
	certFile  = "hub.crt"
	keyFile   = "hub.key"
	tokenFile = "token"
	ownerFile = "owner"
)

// certLifetime is long on purpose. The certificate is trusted by fingerprint,
// not by date — neither xmrig nor the API client checks expiry when pinning —
// so an expiry would only ever be a hub that stopped working one morning.
const certLifetime = 100 * 365 * 24 * time.Hour

// Ensure returns the hub identity kept in dir, creating it on first use.
//
// It is made once and then kept: every paired device pinned this certificate,
// and a new one would unpair the whole house.
func Ensure(dir string) (Identity, error) {
	if id, err := Load(dir); err == nil {
		return id, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return Identity{}, err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return Identity{}, err
	}
	certPEM, keyPEM, err := newCertificate(time.Now())
	if err != nil {
		return Identity{}, err
	}
	token, err := newToken()
	if err != nil {
		return Identity{}, err
	}
	// The key and the token before the certificate: Load treats the
	// certificate as the sign that the identity is complete, so a crash
	// half-way leaves nothing that looks usable.
	for _, f := range []struct {
		name string
		data []byte
	}{{keyFile, keyPEM}, {tokenFile, []byte(token + "\n")}, {certFile, certPEM}} {
		if err := writeFile(filepath.Join(dir, f.name), f.data); err != nil {
			return Identity{}, err
		}
	}
	return Load(dir)
}

// Load reads an existing identity. It returns an error wrapping
// os.ErrNotExist when the hub has never been set up in dir.
func Load(dir string) (Identity, error) {
	id := Identity{CertPath: filepath.Join(dir, certFile), KeyPath: filepath.Join(dir, keyFile), dir: dir}
	certPEM, err := os.ReadFile(id.CertPath)
	if err != nil {
		return Identity{}, err
	}
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return Identity{}, fmt.Errorf("%s is not a PEM certificate", id.CertPath)
	}
	id.Fingerprint = sha256.Sum256(block.Bytes)
	if _, err := os.Stat(id.KeyPath); err != nil {
		return Identity{}, err
	}
	token, err := os.ReadFile(filepath.Join(dir, tokenFile))
	if err != nil {
		return Identity{}, err
	}
	id.Token = strings.TrimSpace(string(token))
	if len(id.Token) != tokenLen*2 {
		return Identity{}, fmt.Errorf("%s is not a hub token", filepath.Join(dir, tokenFile))
	}
	switch owner, err := os.ReadFile(filepath.Join(dir, ownerFile)); {
	case err == nil:
		id.Owner = strings.TrimSpace(string(owner))
	case !errors.Is(err, os.ErrNotExist):
		return Identity{}, err
	}
	return id, nil
}

// errClaimed is returned by claimOwner when the hub already has an owner.
var errClaimed = errors.New("this hub is already set up")

// claimOwner makes the owner token, once. O_EXCL makes the file the lock: of
// two desktops setting the hub up at the same moment, exactly one gets a token.
func (id Identity) claimOwner() (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	f, err := os.OpenFile(filepath.Join(id.dir, ownerFile), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return "", errClaimed
	}
	if err != nil {
		return "", err
	}
	if _, err := f.WriteString(token + "\n"); err != nil {
		f.Close()
		id.releaseOwner()
		return "", err
	}
	if err := f.Close(); err != nil {
		id.releaseOwner()
		return "", err
	}
	return token, nil
}

// releaseOwner undoes a claim whose setup failed, so the owner can try again.
func (id Identity) releaseOwner() { _ = os.Remove(filepath.Join(id.dir, ownerFile)) }

// newCertificate makes a self-signed ECDSA P-256 certificate. P-256 rather
// than RSA: both xmrig's OpenSSL and p2pool's TLS accept it, and it keeps the
// handshake cheap on a Nodo's little cores.
func newCertificate(now time.Time) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "kind-miner hub"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(certLifetime),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

// tokenLen is the API token's size in bytes: 128 bits, far past guessing over
// a LAN, and short enough to keep the pairing code short.
const tokenLen = 16

func newToken() (string, error) {
	b := make([]byte, tokenLen)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// writeFile writes owner-only, through a rename so a reader never sees half.
func writeFile(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
