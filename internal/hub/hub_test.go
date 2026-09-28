package hub

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/engine"
)

func TestEnsureKeepsTheSameIdentity(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	if _, err := Load(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Load before Ensure: %v, want ErrNotExist", err)
	}
	a, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Every paired device pinned the first certificate; a second Ensure
	// making a new one would unpair the house.
	b, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a.Fingerprint != b.Fingerprint || a.Token != b.Token {
		t.Error("a second Ensure changed the identity")
	}

	// The fingerprint is xmrig's: SHA-256 of the certificate's DER.
	certPEM, _ := os.ReadFile(a.CertPath)
	block, _ := pem.Decode(certPEM)
	if sha256.Sum256(block.Bytes) != a.Fingerprint {
		t.Error("fingerprint is not the SHA-256 of the certificate DER")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if time.Until(cert.NotAfter) < 50*365*24*time.Hour {
		t.Errorf("certificate expires %s; a pinned hub must not stop working one morning", cert.NotAfter)
	}
	for _, f := range []string{a.KeyPath, filepath.Join(dir, tokenFile)} {
		if fi, err := os.Stat(f); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, mode %v; want owner-only", f, err, fi.Mode().Perm())
		}
	}
}

func TestPairingCode(t *testing.T) {
	var fp [32]byte
	for i := range fp {
		fp[i] = byte(i * 7)
	}
	for _, host := range []string{"192.168.8.192", "moneronodo.lan", "fd00::1"} {
		p := Pairing{Host: host, StratumPort: 18087, APIPort: 18088, Fingerprint: fp, Token: strings.Repeat("ab", tokenLen)}
		code := p.Code()
		got, err := ParseCode("  " + code + "\n")
		if err != nil {
			t.Fatalf("%s: %v", host, err)
		}
		if got != p {
			t.Errorf("round trip of %s: got %+v", host, got)
		}
		if strings.ContainsAny(code, " :'\"#") {
			t.Errorf("code %q needs quoting in a shell or YAML", code)
		}
	}
	if got := (Pairing{Host: "fd00::1", StratumPort: 18087}).StratumAddr(); got != "[fd00::1]:18087" {
		t.Errorf("IPv6 stratum address = %q", got)
	}

	good := Pairing{Host: "10.0.0.2", StratumPort: 18087, APIPort: 18088, Token: strings.Repeat("00", tokenLen)}.Code()
	for name, code := range map[string]string{
		"empty":                 "",
		"a wallet address":      "48n5Ygcu2EpGSKE4NhKcqpQftUYvcmCweWh2mAPngjPZR96UEE5mAwvCGv9QJLXPnFYjnnDhiZoqXUxmDd5CZrDP9t6onUg",
		"cut short":             good[:40],
		"not base64":            "km1-!!!!",
		"a host with a colon":   Pairing{Host: "evil:1", StratumPort: 1, APIPort: 1, Token: strings.Repeat("00", tokenLen)}.Code(),
		"a zero stratum port":   Pairing{Host: "10.0.0.2", APIPort: 1, Token: strings.Repeat("00", tokenLen)}.Code(),
		"a host with a slash":   Pairing{Host: "a/b", StratumPort: 1, APIPort: 1, Token: strings.Repeat("00", tokenLen)}.Code(),
		"a newline in the host": Pairing{Host: "a\nb", StratumPort: 1, APIPort: 1, Token: strings.Repeat("00", tokenLen)}.Code(),
	} {
		if _, err := ParseCode(code); !errors.Is(err, ErrBadCode) {
			t.Errorf("%s: err = %v, want ErrBadCode", name, err)
		}
	}
}

func TestParseWorker(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want Worker
		ok   bool
	}{
		{"a named device", "192.168.8.20:51234,3600,120000,4000,garage-server",
			Worker{Name: "garage-server", Addr: "192.168.8.20:51234", Connected: time.Hour, Hashrate: 4000}, true},
		{"an IPv6 device", "[fd00::5]:40000,60,1000,33,laptop",
			Worker{Name: "laptop", Addr: "[fd00::5]:40000", Connected: time.Minute, Hashrate: 33}, true},
		{"a connection that has not logged in reads as unnamed", "10.0.0.3:1,5,1000,0,not logged in",
			Worker{Addr: "10.0.0.3:1", Connected: 5 * time.Second}, true},
		{"too few fields", "10.0.0.3:1,5,1000", Worker{}, false},
		{"a garbled number", "10.0.0.3:1,five,1000,0,x", Worker{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseWorker(tt.in)
			if ok != tt.ok || got != tt.want {
				t.Errorf("ParseWorker(%q) = %+v, %v; want %+v, %v", tt.in, got, ok, tt.want, tt.ok)
			}
		})
	}
	if got := ParseWorkers([]string{"bad", "1.2.3.4:5,1,1,1,a"}); len(got) != 1 || got[0].Name != "a" {
		t.Errorf("ParseWorkers kept %+v", got)
	}
}

func TestWorkerName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"garage-server", "garage-server"},
		// p2pool would cut at the dot and read the rest as a difficulty.
		{"laptop.local", "laptop"},
		{"Dad's laptop.local", "Dad-s-laptop"},
		{"  spaced out  ", "spaced-out"},
		// p2pool keeps 31 characters; so does the name, rather than p2pool
		// cutting it somewhere the user did not choose.
		{strings.Repeat("x", 40), strings.Repeat("x", 31)},
		{"+100", "100"},
		{"...", ""},
	}
	for _, tt := range tests {
		if got := WorkerName(tt.in); got != tt.want {
			t.Errorf("WorkerName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDeviceConfig(t *testing.T) {
	p := Pairing{Host: "moneronodo.lan", StratumPort: 18087, APIPort: 18088, Fingerprint: [32]byte{0xab}}
	var c struct {
		Pools []map[string]any `json:"pools"`
	}
	if err := json.Unmarshal(DeviceConfig(p, "garage server"), &c); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"url": "moneronodo.lan:18087", "user": "garage-server", "tls": true,
		"tls-fingerprint": p.FingerprintHex(), "keepalive": true,
	}
	if len(c.Pools) != 1 || fmt.Sprint(c.Pools[0]) != fmt.Sprint(want) {
		t.Errorf("pools = %v, want [%v]", c.Pools, want)
	}
}

// serveTest runs the API on a free loopback port and returns its pairing.
func serveTest(t *testing.T, dir string, h Household) Pairing {
	t.Helper()
	id, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	srv, err := Serve(fmt.Sprintf("127.0.0.1:%d", port), id, Handlers{Household: func() Household { return h }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return id.PairingFor("127.0.0.1", "", 18087, port)
}

func TestHouseholdAPI(t *testing.T) {
	want := Household{
		Chain:   "nano",
		Stats:   engine.P2PoolStats{SharesFound: 7, SidechainDifficulty: 1234, BlockTime: 30 * time.Second},
		StatsOK: true,
		Workers: []Worker{{Name: "garage-server", Addr: "10.0.0.5:4000", Connected: time.Hour, Hashrate: 900}},
	}
	p := serveTest(t, filepath.Join(t.TempDir(), "hub"), want)
	ctx := context.Background()

	t.Run("a paired device reads the household", func(t *testing.T) {
		got, viaTor, err := Fetch(ctx, p, "")
		if err != nil || viaTor {
			t.Fatalf("err %v, viaTor %v", err, viaTor)
		}
		if got.Stats.SharesFound != 7 || got.Stats.BlockTime != 30*time.Second || !got.StatsOK ||
			len(got.Workers) != 1 || got.Workers[0] != want.Workers[0] {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("a wrong token is refused", func(t *testing.T) {
		bad := p
		bad.Token = strings.Repeat("00", tokenLen)
		if _, _, err := Fetch(ctx, bad, ""); err == nil || !strings.Contains(err.Error(), "pair with this hub first") {
			t.Errorf("err = %v, want the hub's refusal", err)
		}
	})
	t.Run("another hub at the same address is not trusted", func(t *testing.T) {
		// What a machine impersonating the hub on the LAN looks like: the
		// right address, a different certificate.
		other, err := Ensure(filepath.Join(t.TempDir(), "other"))
		if err != nil {
			t.Fatal(err)
		}
		impostor := p
		impostor.Fingerprint = other.Fingerprint
		if _, _, err := Fetch(ctx, impostor, ""); !errors.Is(err, ErrWrongHub) {
			t.Errorf("err = %v, want ErrWrongHub", err)
		}
	})
	t.Run("the token is required at all", func(t *testing.T) {
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: pinnedTLS(p.Fingerprint)}}
		resp, err := client.Get("https://" + p.APIAddr() + householdPath)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("status %d without a token", resp.StatusCode)
		}
	})
}
