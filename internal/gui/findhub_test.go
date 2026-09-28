package gui

import (
	"context"
	"errors"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/hub"
)

const (
	ownWallet   = "4AdUndXHHZ6cfufTMvppY6JwXNouMBzSkbLYfpAV5Usx3skxNgYeYTRj5UzqtReoS44qo9mtmXCqY45DJ852K5Jv2684Rge"
	otherWallet = "44AFFq5kSiGBoZ4NMDwYtN18obc8AemS33DBLWs3H7otXft3XjrpDtQGv7SqSsaBYBb98uNbr2VBBEt7f2wfn3RVGQBEP3A"
)

// testHub is a hub with no config yet, as kind-minerd serves one, whose
// wallet changes are recorded in *wallets. refuse makes it refuse them.
func testHub(t *testing.T, refuse *error) (addr string, fp [32]byte, wallets *[]string) {
	t.Helper()
	id, err := hub.Ensure(filepath.Join(t.TempDir(), "hub"))
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr = ln.Addr().String()
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	var got []string
	srv, err := hub.Serve(addr, id, hub.Handlers{
		Hello: hub.Hello{Name: "moneronodo", Nodo: true},
		Setup: func(string) (hub.Pairing, error) {
			return id.PairingFor("192.0.2.1", "", hub.DefaultStratumPort, port), nil
		},
		SetWallet: func(a string) error {
			if refuse != nil && *refuse != nil {
				return *refuse
			}
			got = append(got, a)
			return nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return addr, id.Fingerprint, &got
}

func TestSetUpAHubFromSettings(t *testing.T) {
	test.NewApp()
	config.SetPath(filepath.Join(t.TempDir(), "config.yaml"))
	t.Cleanup(func() { config.SetPath("") })
	cfg := config.Defaults()
	cfg.Wallet = ownWallet
	u := &uiApp{sup: core.New(cfg)}
	var refuse error
	addr, fp, wallets := testHub(t, &refuse)
	ctx := context.Background()

	// What the confirm button does, less the dialogs.
	f := u.newSettingsForm(cfg)
	p, owner, err := hub.SetUp(ctx, addr, fp, strings.TrimSpace(f.wallet.Text))
	if err != nil {
		t.Fatal(err)
	}
	f.pairedBySetUp(p.Code(), owner)
	if msg := f.apply(u, cfg); msg != "" {
		t.Fatal(msg)
	}
	if cfg.Mode != config.ModeHub || cfg.HubCode != p.Code() || cfg.HubOwnerToken != owner {
		t.Fatalf("after setup: mode %s, code %q, owner kept %v", cfg.Mode, cfg.HubCode, cfg.HubOwnerToken == owner)
	}
	if len(*wallets) != 0 {
		t.Error("an unchanged wallet was sent to the hub")
	}

	t.Run("a wallet change reaches the hub without a restart here", func(t *testing.T) {
		f := u.newSettingsForm(cfg)
		f.wallet.SetText(otherWallet)
		if msg := f.apply(u, cfg); msg != "" {
			t.Fatal(msg)
		}
		if len(*wallets) != 1 || (*wallets)[0] != otherWallet || cfg.Wallet != otherWallet {
			t.Errorf("hub got %v, config has %q", *wallets, cfg.Wallet)
		}
		if f.needsRestart(cfg) {
			t.Error("a hubbed desktop asked to restart for the hub's wallet")
		}
	})
	t.Run("a refusal leaves the config as it was", func(t *testing.T) {
		refuse = errors.New("subaddresses cannot receive payouts")
		defer func() { refuse = nil }()
		f := u.newSettingsForm(cfg)
		f.wallet.SetText(ownWallet)
		msg := f.apply(u, cfg)
		if !strings.Contains(msg, "subaddresses") || cfg.Wallet != otherWallet {
			t.Errorf("msg %q, wallet now %q", msg, cfg.Wallet)
		}
	})
	t.Run("the token stays with its hub through a new code", func(t *testing.T) {
		f := u.newSettingsForm(cfg)
		again := p
		again.Onion = strings.Repeat("a", 56) + ".onion"
		if got := f.ownerTokenFor(again.Code()); got != owner {
			t.Error("pairing the same hub again lost the owner token")
		}
	})
	t.Run("and is dropped for another hub", func(t *testing.T) {
		otherAddr, otherFP, _ := testHub(t, nil)
		op, _, err := hub.SetUp(ctx, otherAddr, otherFP, ownWallet)
		if err != nil {
			t.Fatal(err)
		}
		f := u.newSettingsForm(cfg)
		f.hubCode.SetText(op.Code())
		if msg := f.apply(u, cfg); msg != "" {
			t.Fatal(msg)
		}
		if cfg.HubOwnerToken != "" {
			t.Error("the owner token of one hub was kept for another")
		}
	})
}

func TestHubAPIAddr(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "",
		" 192.168.1.20 ":    "192.168.1.20:" + strconv.Itoa(hub.DefaultAPIPort),
		"moneronodo.lan":    "moneronodo.lan:" + strconv.Itoa(hub.DefaultAPIPort),
		"192.168.1.20:9000": "192.168.1.20:9000",
		"[fe80::1]":         "[fe80::1]:" + strconv.Itoa(hub.DefaultAPIPort),
	} {
		if got := hubAPIAddr(in); got != want {
			t.Errorf("hubAPIAddr(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHubRowText(t *testing.T) {
	cases := []struct {
		f    hub.Found
		want string
	}{
		{hub.Found{Hello: hub.Hello{Name: "moneronodo", Nodo: true}, Host: "192.168.1.20"},
			"moneronodo (Nodo) at 192.168.1.20 — not set up yet"},
		{hub.Found{Hello: hub.Hello{Name: "garage", SetUp: true}, Host: "192.168.1.21"},
			"garage at 192.168.1.21 — " + findHubIsSetUp},
	}
	for _, c := range cases {
		if got := hubRowText(c.f); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
}
