package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testAddress = "4AdUndXHHZ6cfufTMvppY6JwXNouMBzSkbLYfpAV5Usx3skxNgYeYTRj5UzqtReoS44qo9mtmXCqY45DJ852K5Jv2684Rge"

// setupHub serves a hub that has no config yet, the way kind-minerd does
// while it waits for a desktop. It returns the API's address and the
// addresses Setup was called with.
func setupHub(t *testing.T, dir string, setup func(string) (Pairing, error)) (Identity, string, *[]string) {
	t.Helper()
	id, err := Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	addr := freeAddr(t)
	var calls []string
	srv, err := Serve(addr, id, Handlers{
		Hello: Hello{Name: "moneronodo", Nodo: true},
		Setup: func(a string) (Pairing, error) {
			calls = append(calls, a)
			return setup(a)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return id, addr, &calls
}

func freeAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().String()
}

func TestSetUpAHubFromTheDesktop(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "hub")
	var id Identity
	id, addr, calls := setupHub(t, dir, func(a string) (Pairing, error) {
		if !strings.HasPrefix(a, "4") {
			return Pairing{}, errors.New("that is not a Monero address")
		}
		// The hub names itself by the address it thinks it has; the desktop
		// replaces it with the one that reached it.
		return id.PairingFor("192.0.2.1", "", DefaultStratumPort, DefaultAPIPort), nil
	})

	hello, fp, err := Probe(ctx, addr, [32]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if hello.SetUp || hello.Name != "moneronodo" || !hello.Nodo {
		t.Errorf("hello = %+v, want an unset-up Nodo", hello)
	}
	if fp != id.Fingerprint {
		t.Fatal("Probe did not report the hub's certificate")
	}

	t.Run("a bad address is refused and leaves the hub unclaimed", func(t *testing.T) {
		_, _, err := SetUp(ctx, addr, fp, "not-an-address")
		if err == nil || !strings.Contains(err.Error(), "not a Monero address") {
			t.Fatalf("err = %v, want the hub's reason", err)
		}
		if h, _, _ := Probe(ctx, addr, fp); h.SetUp {
			t.Error("a failed setup left the hub claimed")
		}
	})

	p, owner, err := SetUp(ctx, addr, fp, testAddress)
	if err != nil {
		t.Fatal(err)
	}
	t.Run("the desktop gets a pairing it can use and an owner token", func(t *testing.T) {
		if p.Host != "127.0.0.1" || p.Fingerprint != id.Fingerprint || p.Token != id.Token {
			t.Errorf("pairing = %+v", p)
		}
		if owner == "" || owner == id.Token {
			t.Error("the owner token must exist and differ from the pairing token")
		}
		if (*calls)[len(*calls)-1] != testAddress {
			t.Errorf("Setup got %q", (*calls)[len(*calls)-1])
		}
		reloaded, err := Load(dir)
		if err != nil || reloaded.Owner != owner {
			t.Errorf("owner token not kept: %v, %q", err, reloaded.Owner)
		}
	})
	t.Run("a second desktop cannot claim it", func(t *testing.T) {
		n := len(*calls)
		if _, _, err := SetUp(ctx, addr, fp, testAddress); !errors.Is(err, ErrAlreadySetUp) {
			t.Errorf("err = %v, want ErrAlreadySetUp", err)
		}
		if len(*calls) != n {
			t.Error("a claimed hub ran Setup again")
		}
		if h, _, _ := Probe(ctx, addr, fp); !h.SetUp {
			t.Error("hello still says not set up")
		}
	})
	t.Run("an impostor is not set up", func(t *testing.T) {
		other, err := Ensure(filepath.Join(t.TempDir(), "other"))
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := Probe(ctx, addr, other.Fingerprint); !errors.Is(err, ErrWrongHub) {
			t.Errorf("Probe: err = %v, want ErrWrongHub", err)
		}
		if _, _, err := SetUp(ctx, addr, other.Fingerprint, testAddress); !errors.Is(err, ErrWrongHub) {
			t.Errorf("SetUp: err = %v, want ErrWrongHub", err)
		}
	})
}

func TestSetWallet(t *testing.T) {
	ctx := context.Background()
	serve := func(t *testing.T, owned bool, setWallet func(string) error) (Pairing, string) {
		t.Helper()
		id, err := Ensure(filepath.Join(t.TempDir(), "hub"))
		if err != nil {
			t.Fatal(err)
		}
		owner := ""
		if owned {
			if owner, err = id.claimOwner(); err != nil {
				t.Fatal(err)
			}
			id.Owner = owner
		}
		addr := freeAddr(t)
		srv, err := Serve(addr, id, Handlers{SetWallet: setWallet})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(srv.Close)
		host, port, _ := net.SplitHostPort(addr)
		var apiPort int
		fmt.Sscan(port, &apiPort)
		return id.PairingFor(host, "", DefaultStratumPort, apiPort), owner
	}

	t.Run("the owner changes the wallet", func(t *testing.T) {
		var got string
		p, owner := serve(t, true, func(a string) error { got = a; return nil })
		if err := SetWallet(ctx, p, owner, testAddress); err != nil {
			t.Fatal(err)
		}
		if got != testAddress {
			t.Errorf("SetWallet got %q", got)
		}
	})
	t.Run("the hub's reason for refusing an address reaches the desktop", func(t *testing.T) {
		p, owner := serve(t, true, func(string) error { return errors.New("subaddresses cannot receive payouts") })
		if err := SetWallet(ctx, p, owner, "8…"); err == nil || !strings.Contains(err.Error(), "subaddresses") {
			t.Errorf("err = %v", err)
		}
	})
	t.Run("a paired device cannot redirect the earnings", func(t *testing.T) {
		called := false
		p, _ := serve(t, true, func(string) error { called = true; return nil })
		err := SetWallet(ctx, p, p.Token, testAddress)
		if err == nil || called {
			t.Errorf("err = %v, called %v: the pairing token changed the wallet", err, called)
		}
	})
	t.Run("a hub set up over SSH keeps its config", func(t *testing.T) {
		p, _ := serve(t, false, func(string) error { return nil })
		err := SetWallet(ctx, p, "", testAddress)
		if err == nil || !strings.Contains(err.Error(), "over SSH") {
			t.Errorf("err = %v, want the SSH explanation", err)
		}
	})
}

func TestDiscover(t *testing.T) {
	id, err := Ensure(filepath.Join(t.TempDir(), "hub"))
	if err != nil {
		t.Fatal(err)
	}
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	a, err := Announce(port, func() Beacon {
		return Beacon{Hello: Hello{Name: "moneronodo", Nodo: true}, APIPort: 18099, Fingerprint: id.FingerprintHex()}
	})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	// Twice, as a hub on two broadcast addresses hears it twice.
	found, err := discover(ctx, []*net.UDPAddr{target, target})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("found %d hubs, want 1: %+v", len(found), found)
	}
	f := found[0]
	if f.Host != "127.0.0.1" || f.APIPort != 18099 || f.Fingerprint != id.Fingerprint || f.Name != "moneronodo" || f.SetUp {
		t.Errorf("found %+v", f)
	}
	if f.APIAddr() != "127.0.0.1:18099" {
		t.Errorf("APIAddr = %s", f.APIAddr())
	}

	t.Run("a short probe gets no answer", func(t *testing.T) {
		// Otherwise a spoofed small packet would earn a victim a larger one.
		c, err := net.DialUDP("udp4", nil, target)
		if err != nil {
			t.Fatal(err)
		}
		defer c.Close()
		c.Write([]byte("kind-miner-discover/1\n"))
		c.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
		if n, err := c.Read(make([]byte, 1024)); err == nil {
			t.Errorf("answered %d bytes to a short probe", n)
		}
	})
	t.Run("the probe is larger than any answer", func(t *testing.T) {
		if len(probe) <= maxBeacon {
			t.Errorf("probe %d bytes, answers up to %d", len(probe), maxBeacon)
		}
	})
}
