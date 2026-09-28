package hub

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Real v3 addresses, so the encoding is checked against Tor's and not only
// against itself.
var knownOnions = []string{
	"duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczad.onion",
	"2gzyxa5ihm7nsggfxnu52rck2vv4rvmdlkiu3zzui5du4xyclen53wid.onion",
}

func TestOnionAddress(t *testing.T) {
	for _, addr := range knownOnions {
		pub, err := onionKey(addr)
		if err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
		if got := onionAddress(pub); got != addr {
			t.Errorf("round trip of %s gave %s", addr, got)
		}
	}
	// One character changed breaks the checksum: a mistyped address is
	// refused, not paired with.
	typo := "duckduckgogg42xjoc72x3sjasowoarfbgcmvfimaftt6twagswzczae.onion"
	for _, bad := range []string{typo, "example.com", "duckduckgo.onion", ""} {
		if _, err := onionKey(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestPairingCodeWithOnion(t *testing.T) {
	p := Pairing{Host: "192.168.8.192", StratumPort: 18087, APIPort: 18088, Fingerprint: [32]byte{1, 2, 3},
		Token: strings.Repeat("cd", tokenLen), Onion: knownOnions[0]}
	code := p.Code()
	if !strings.HasPrefix(code, codePrefixOnion) {
		t.Fatalf("code %q lacks the onion prefix", code)
	}
	got, err := ParseCode(code)
	if err != nil || got != p {
		t.Fatalf("round trip: %+v, %v", got, err)
	}
	if got.OnionStratumAddr() != knownOnions[0]+":18087" {
		t.Errorf("onion stratum %q", got.OnionStratumAddr())
	}
	// A hub without an onion keeps handing out the shorter km1 code.
	p.Onion = ""
	if code := p.Code(); !strings.HasPrefix(code, codePrefix) {
		t.Errorf("LAN-only code %q", code)
	}
}

func TestDeviceConfigFallsBackToTheOnion(t *testing.T) {
	p := Pairing{Host: "192.168.8.192", StratumPort: 18087, APIPort: 18088, Onion: knownOnions[0]}
	var c struct {
		Pools []devicePool `json:"pools"`
	}
	if err := json.Unmarshal(DeviceConfig(p, "garage"), &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Pools) != 2 {
		t.Fatalf("pools = %+v", c.Pools)
	}
	lan, onion := c.Pools[0], c.Pools[1]
	if lan.URL != "192.168.8.192:18087" || lan.SOCKS5 != "" {
		t.Errorf("the LAN comes first, direct: %+v", lan)
	}
	// Same login and the same pin: TLS runs end to end through Tor.
	if onion.URL != knownOnions[0]+":18087" || onion.SOCKS5 != TorSOCKS ||
		onion.User != lan.User || onion.TLSFingerprint != lan.TLSFingerprint || !onion.TLS {
		t.Errorf("fallback %+v", onion)
	}
}

// fakeTor is a SOCKS5 server standing in for Tor: whatever host it is asked
// for, it connects to target, and it records the host it was asked for.
func fakeTor(t *testing.T, target string) (addr string, asked func() []string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	var mu sync.Mutex
	var hosts []string
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				buf := make([]byte, 262)
				// greeting: ver, n, methods → no auth
				if _, err := io.ReadFull(c, buf[:2]); err != nil {
					return
				}
				io.ReadFull(c, buf[:buf[1]])
				c.Write([]byte{5, 0})
				// request: ver cmd rsv atyp=3 len host port
				if _, err := io.ReadFull(c, buf[:5]); err != nil || buf[3] != 3 {
					return
				}
				host := make([]byte, buf[4])
				io.ReadFull(c, host)
				io.ReadFull(c, buf[:2])
				mu.Lock()
				hosts = append(hosts, string(host)+":"+strconv.Itoa(int(binary.BigEndian.Uint16(buf[:2]))))
				mu.Unlock()
				up, err := net.Dial("tcp", target)
				if err != nil {
					c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
					return
				}
				defer up.Close()
				c.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
				go io.Copy(up, c)
				io.Copy(c, up)
			}()
		}
	}()
	return ln.Addr().String(), func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), hosts...)
	}
}

func TestFetchFallsBackToTheOnion(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "hub")
	hubAt := serveTest(t, dir, Household{Chain: "nano"})
	socks, asked := fakeTor(t, hubAt.APIAddr())

	// Away from home: nothing answers at the LAN address.
	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	deadPort := closed.Addr().(*net.TCPAddr).Port
	closed.Close()
	away := hubAt
	away.APIPort = deadPort
	away.Onion = knownOnions[0]

	h, viaTor, err := Fetch(context.Background(), away, socks)
	if err != nil || !viaTor || h.Chain != "nano" {
		t.Fatalf("got %+v viaTor=%v err=%v", h, viaTor, err)
	}
	if got := asked(); len(got) != 1 || got[0] != knownOnions[0]+":"+strconv.Itoa(deadPort) {
		t.Errorf("Tor was asked for %v", got)
	}

	t.Run("somebody else's machine at the LAN address does not stop it", func(t *testing.T) {
		// A café's 192.168.1.10 answering with its own certificate.
		stranger := serveTest(t, filepath.Join(t.TempDir(), "stranger"), Household{})
		p := hubAt
		p.APIPort = stranger.APIPort
		p.Onion = knownOnions[0]
		if _, viaTor, err := Fetch(context.Background(), p, socks); err != nil || !viaTor {
			t.Errorf("viaTor=%v err=%v", viaTor, err)
		}
	})
	t.Run("the pin holds through Tor", func(t *testing.T) {
		impostorSocks, _ := fakeTor(t, serveTest(t, filepath.Join(t.TempDir(), "imp"), Household{}).APIAddr())
		if _, _, err := Fetch(context.Background(), away, impostorSocks); !errors.Is(err, ErrWrongHub) {
			t.Errorf("err = %v, want ErrWrongHub", err)
		}
	})
	t.Run("without an onion Tor is never asked", func(t *testing.T) {
		before := len(asked())
		lanOnly := away
		lanOnly.Onion = ""
		if _, viaTor, err := Fetch(context.Background(), lanOnly, socks); err == nil || viaTor {
			t.Errorf("viaTor=%v err=%v", viaTor, err)
		}
		if len(asked()) != before {
			t.Error("a LAN-only pairing went through Tor")
		}
	})
}
