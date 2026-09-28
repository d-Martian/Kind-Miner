package hub

import (
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
	"time"

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

// householdPath is the one endpoint. Versioned so a desktop and a hub a
// release apart fail clearly rather than misreading each other.
const householdPath = "/v1/household"

// Server is the hub's statistics API.
type Server struct {
	srv *http.Server
}

// Serve starts the API on addr over TLS with the hub's certificate, answering
// with whatever source returns. It returns once the port is bound, so a port
// already in use is an error here rather than a log line later.
func Serve(addr string, id Identity, source func() Household) (*Server, error) {
	cert, err := tls.LoadX509KeyPair(id.CertPath, id.KeyPath)
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := &Server{srv: &http.Server{
		Handler:           handler(id.Token, source),
		TLSConfig:         &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 10 * time.Second,
		// Nothing here is large or slow; a connection held open is not a client.
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
		ErrorLog:     log.New(discard{}, "", 0),
	}}
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

func handler(token string, source func() Household) http.Handler {
	want := []byte("Bearer " + token)
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+householdPath, func(w http.ResponseWriter, r *http.Request) {
		// Constant-time, so the token cannot be learned a byte at a time from
		// how long a wrong one takes to be refused.
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), want) != 1 {
			http.Error(w, "pair with this hub first: run kind-minerd pair on it", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(source())
	})
	return mux
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

// Fetch asks a paired hub for the household's statistics.
func Fetch(ctx context.Context, p Pairing) (Household, error) {
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: pinnedTLS(p.Fingerprint), Proxy: nil},
	}
	defer client.CloseIdleConnections()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+p.APIAddr()+householdPath, nil)
	if err != nil {
		return Household{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.Token)
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrWrongHub) {
			return Household{}, ErrWrongHub
		}
		return Household{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Household{}, fmt.Errorf("hub answered %s", resp.Status)
	}
	var h Household
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&h); err != nil {
		return Household{}, fmt.Errorf("reading the hub's answer: %w", err)
	}
	return h, nil
}
