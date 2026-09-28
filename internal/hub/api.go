package hub

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/proxy"

	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// Household is what the hub tells a paired device: the p2pool statistics for
// the one wallet everyone mines to, who is mining, and the payouts it has
// seen. A paired desktop runs no p2pool of its own, so this is where its
// reward estimate and payout list come from.
type Household struct {
	UpdatedAt time.Time `json:"updated_at"`
	Chain     string    `json:"chain"`
	// Stats is absent (StatsOK false) until the hub's p2pool has synced.
	Stats   engine.P2PoolStats `json:"stats"`
	StatsOK bool               `json:"stats_ok"`
	Workers []Worker           `json:"workers"`
	Payouts []stats.Payout     `json:"payouts"`
}

// The endpoints. Versioned so a desktop and a hub a release apart fail
// clearly rather than misreading each other.
const (
	householdPath = "/v1/household"
	helloPath     = "/v1/hello"
	setupPath     = "/v1/setup"
	walletPath    = "/v1/wallet"
)

// Hello is what the hub says about itself to anyone who asks: enough for a
// desktop to show "moneronodo, not set up yet" in a list and offer to set it
// up. Nothing in it is private — the same is in the LAN beacon.
type Hello struct {
	Name string `json:"name"`
	Nodo bool   `json:"nodo"`
	// SetUp is false only on a hub waiting for its first config.
	SetUp bool `json:"set_up"`
}

// Handlers are what the API answers with. Any may be nil, and that endpoint
// then refuses.
type Handlers struct {
	Household func() Household
	Hello     Hello
	// Setup configures a hub that has no config yet to pay address, and
	// returns its pairing. It is offered only then: once set up, a hub is
	// changed by its owner, never claimed again.
	Setup func(address string) (Pairing, error)
	// SetWallet changes the wallet the hub pays. Nil when the config is not
	// the API's to change — a hub set up over SSH keeps its config in root's
	// file, and the service cannot, and should not, write it.
	SetWallet func(address string) error
}

// Server is the hub's API.
type Server struct {
	srv *http.Server
	id  Identity
	h   Handlers

	mu sync.Mutex
	// owner is the owner token, once there is one. Set by a setup while
	// serving, so read under mu.
	owner string
}

// Serve starts the API on addr over TLS with the hub's certificate. It
// returns once the port is bound, so a port already in use is an error here
// rather than a log line later.
func Serve(addr string, id Identity, h Handlers) (*Server, error) {
	cert, err := tls.LoadX509KeyPair(id.CertPath, id.KeyPath)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{id: id, h: h, owner: id.Owner}
	s.srv = &http.Server{
		Handler:           s.handler(),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
		// Nothing here is large or slow; a connection held open is not a client.
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
		ErrorLog:     log.New(discard{}, "", 0),
	}
	go func() {
		if err := s.srv.ServeTLS(ln, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("hub API: %v", err)
		}
	}()
	return s, nil
}

// discard swallows net/http's own log: a LAN scanner's failed handshakes are
// not worth a line each in the journal.
type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

// Close stops the API.
func (s *Server) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
}

// hello is the Hello with SetUp filled in as it stands now.
func (s *Server) hello() Hello {
	s.mu.Lock()
	defer s.mu.Unlock()
	h := s.h.Hello
	h.SetUp = s.h.Setup == nil || s.owner != ""
	return h
}

// bearer reports whether r carries want as its bearer token. Constant-time,
// so a token cannot be learned a byte at a time from how long a wrong one
// takes to be refused.
func bearer(r *http.Request, want string) bool {
	return want != "" && subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+want)) == 1
}

// addressRequest is the body of setup and wallet: one address, nothing else.
type addressRequest struct {
	Address string `json:"address"`
}

// setupAnswer is what a setup returns: the pairing, for the desktop to mine
// to the hub, and the owner token, for it alone to keep.
type setupAnswer struct {
	Code  string `json:"code"`
	Owner string `json:"owner_token"`
}

func readAddress(r *http.Request) (string, error) {
	var req addressRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&req); err != nil {
		return "", errors.New("expected {\"address\": \"4…\"}")
	}
	return req.Address, nil
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+householdPath, func(w http.ResponseWriter, r *http.Request) {
		if !bearer(r, s.id.Token) {
			http.Error(w, "pair with this hub first: run kind-minerd pair on it", http.StatusUnauthorized)
			return
		}
		if s.h.Household == nil {
			http.Error(w, "this hub is not set up yet", http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, s.h.Household())
	})
	mux.HandleFunc("GET "+helloPath, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, s.hello())
	})
	mux.HandleFunc("POST "+setupPath, s.setup)
	mux.HandleFunc("POST "+walletPath, func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		owner := s.owner
		s.mu.Unlock()
		if owner == "" || s.h.SetWallet == nil {
			http.Error(w, "this hub was set up over SSH: change its wallet in its config file", http.StatusForbidden)
			return
		}
		if !bearer(r, owner) {
			http.Error(w, "only the desktop that set this hub up can change its wallet", http.StatusForbidden)
			return
		}
		addr, err := readAddress(r)
		if err == nil {
			err = s.h.SetWallet(addr)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	return mux
}

// setup gives a hub with no config its first one. Whoever asks first gets it:
// there is nothing to authenticate against before the hub has an owner, and
// a Nodo has no screen to show a code on. What bounds that is that it
// happens once, while the owner is setting the hub up, and that losing the
// race is loud — the owner's desktop is told the hub is already set up —
// rather than silent.
func (s *Server) setup(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.h.Setup == nil || s.owner != "" {
		http.Error(w, "this hub is already set up: ask whoever set it up for its pairing code", http.StatusConflict)
		return
	}
	addr, err := readAddress(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// The token first: the file is the claim, so a second setup racing this
	// one is refused even before this one's config is written.
	owner, err := s.id.claimOwner()
	if errors.Is(err, errClaimed) {
		http.Error(w, "this hub is already set up: ask whoever set it up for its pairing code", http.StatusConflict)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	p, err := s.h.Setup(addr)
	if err != nil {
		s.id.releaseOwner()
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.owner = owner
	writeJSON(w, setupAnswer{Code: p.Code(), Owner: owner})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// ErrWrongHub is returned when whatever answered at the hub's address did not
// present the certificate the device paired with.
var ErrWrongHub = errors.New("the hub's certificate does not match the one paired with")

// pinnedTLS trusts exactly one certificate, by fingerprint. The usual chain
// check is skipped because there is no chain — the certificate is
// self-signed — and the pin is the whole of the trust decision: it is
// checked on every connection, and nothing else is accepted.
func pinnedTLS(fingerprint [32]byte) *tls.Config {
	return &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // replaced by VerifyConnection below
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrWrongHub
			}
			got := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(got[:], fingerprint[:]) != 1 {
				return ErrWrongHub
			}
			return nil
		},
	}
}

// Fetch asks a paired hub for the household's statistics: at its LAN
// address, then — for a hub with an onion, when socks names a Tor — through
// Tor. viaTor says which answered.
//
// A LAN address that answers with the wrong certificate does not stop the
// onion being tried: away from home, 192.168.1.10 is somebody else's machine,
// and being refused there is the pin working. When both fail it is the
// onion's error that counts, since away from home the LAN one is expected.
func Fetch(ctx context.Context, p Pairing, socks string) (h Household, viaTor bool, err error) {
	// Short, because away from home the LAN address may never answer at all,
	// and a hub on the same LAN answers in milliseconds.
	lanCtx, cancel := context.WithTimeout(ctx, lanTimeout)
	h, err = fetch(lanCtx, p, p.APIAddr(), nil)
	cancel()
	if err == nil || p.Onion == "" || socks == "" {
		return h, false, err
	}
	dial, derr := socksDial(socks, p.Onion)
	if derr != nil {
		return Household{}, false, err
	}
	h, err = fetch(ctx, p, p.OnionAPIAddr(), dial)
	return h, err == nil, err
}

// lanTimeout bounds the attempt at the hub's LAN address.
const lanTimeout = 5 * time.Second

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// socksDial reaches host through Tor on a circuit of its own: Tor isolates
// streams by SOCKS credentials, and the username is keyed on the
// destination, the same way internal/nodes keeps each node on its own circuit.
func socksDial(socks, host string) (dialFunc, error) {
	d, err := proxy.SOCKS5("tcp", socks, &proxy.Auth{User: "kind-miner/" + host, Password: "kind-miner"}, proxy.Direct)
	if err != nil {
		return nil, err
	}
	cd, ok := d.(proxy.ContextDialer)
	if !ok {
		return nil, errors.New("SOCKS5 dialer cannot take a deadline")
	}
	return cd.DialContext, nil
}

func fetch(ctx context.Context, p Pairing, addr string, dial dialFunc) (Household, error) {
	var h Household
	if err := call(ctx, pinnedTLS(p.Fingerprint), dial, http.MethodGet, addr, householdPath, p.Token, nil, &h); err != nil {
		return Household{}, err
	}
	return h, nil
}

// ErrAlreadySetUp is returned by SetUp when another desktop set the hub up
// first.
var ErrAlreadySetUp = errors.New("this hub is already set up: ask whoever set it up for its pairing code")

// Probe asks whatever answers at addr (host:port of a hub's API) who it is,
// and returns the fingerprint of the certificate it presented. This is the
// one connection that trusts on first use: a hub that has never been paired
// with has nothing to pin yet. Everything after it — the setup, the pairing
// code — is pinned to the fingerprint it returns, so the desktop goes on
// talking to whatever it first met, and nothing else.
//
// want, when not zero, is the fingerprint the hub's beacon announced; a
// different certificate here is refused as ErrWrongHub.
func Probe(ctx context.Context, addr string, want [32]byte) (Hello, [32]byte, error) {
	var got [32]byte
	cfg := &tls.Config{
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true, // trust on first use; see above
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrWrongHub
			}
			got = sha256.Sum256(cs.PeerCertificates[0].Raw)
			if want != ([32]byte{}) && subtle.ConstantTimeCompare(got[:], want[:]) != 1 {
				return ErrWrongHub
			}
			return nil
		},
	}
	var h Hello
	if err := call(ctx, cfg, nil, http.MethodGet, addr, helloPath, "", nil, &h); err != nil {
		return Hello{}, got, err
	}
	return h, got, nil
}

// SetUp gives the hub at addr, whose certificate is fingerprint, its first
// config: to mine for address. It returns the pairing to mine to it with —
// its host the one this desktop reached it at, which is what worked from
// here — and the owner token that lets this desktop change the hub later.
func SetUp(ctx context.Context, addr string, fingerprint [32]byte, address string) (Pairing, string, error) {
	var ans setupAnswer
	err := call(ctx, pinnedTLS(fingerprint), nil, http.MethodPost, addr, setupPath, "", addressRequest{address}, &ans)
	if err != nil {
		return Pairing{}, "", err
	}
	p, err := ParseCode(ans.Code)
	if err != nil {
		return Pairing{}, "", fmt.Errorf("the hub's pairing code: %w", err)
	}
	if p.Fingerprint != fingerprint {
		// The code is the hub's own account of its certificate; the
		// connection is ours. They agree unless something is badly wrong.
		return Pairing{}, "", ErrWrongHub
	}
	if host, _, err := net.SplitHostPort(addr); err == nil {
		p.Host = host
	}
	return p, ans.Owner, nil
}

// SetWallet changes the wallet the paired hub pays, with the owner token its
// setup returned. At home only: it is not worth a Tor circuit, and away from
// home is not when anyone changes their hub's wallet.
func SetWallet(ctx context.Context, p Pairing, owner, address string) error {
	return call(ctx, pinnedTLS(p.Fingerprint), nil, http.MethodPost, p.APIAddr(), walletPath, owner, addressRequest{address}, nil)
}

// call makes one request to the hub API, sending in as JSON when not nil and
// decoding the answer into out when not nil. A refusal comes back as the
// hub's own sentence, which is written to be shown to the user.
func call(ctx context.Context, tlsCfg *tls.Config, dial dialFunc, method, addr, path, token string, in, out any) error {
	tr := &http.Transport{TLSClientConfig: tlsCfg, Proxy: nil}
	if dial != nil {
		tr.DialContext = dial
	}
	client := &http.Client{Transport: tr} // the caller's context bounds it
	defer client.CloseIdleConnections()
	var body io.Reader
	if in != nil {
		data, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "https://"+addr+path, body)
	if err != nil {
		return err
	}
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrWrongHub) {
			return ErrWrongHub
		}
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusConflict && path == setupPath:
		return ErrAlreadySetUp
	case resp.StatusCode/100 != 2:
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<10))
		if m := strings.TrimSpace(string(msg)); m != "" {
			return fmt.Errorf("the hub says: %s", m)
		}
		return fmt.Errorf("hub answered %s", resp.Status)
	case out == nil:
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("reading the hub's answer: %w", err)
	}
	return nil
}
