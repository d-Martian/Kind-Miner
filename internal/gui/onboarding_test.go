package gui

import (
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/kindness"
)

// First-run setup asks one question and discloses one more. Everything else it
// used to ask must land on the defaults the plan promises, and the disclosure
// must mean what it says.

func newOnboarding(t *testing.T) *onboarding {
	t.Helper()
	test.NewApp()
	o := &onboarding{cfg: config.Defaults()}
	o.build()
	return o
}

func TestOnboardingWritesTheSimpleDefaults(t *testing.T) {
	o := newOnboarding(t)
	o.wallet.SetText(" " + sampleAddress + " ")
	cfg := config.Defaults()
	o.answers(cfg)

	if cfg.Wallet != sampleAddress {
		t.Errorf("wallet = %q, want it trimmed", cfg.Wallet)
	}
	checks := []struct {
		what      string
		got, want any
	}{
		{"sidechain", cfg.P2PoolChain, config.ChainNano},
		{"node", cfg.Mode, config.ModeP2PoolRemote},
		{"kindness", cfg.Kindness, kindness.Polite},
		{"pause on battery", cfg.PauseOnBattery, true},
		{"bundled xmrig", cfg.XMRigBinPath, ""},
		{"bundled p2pool", cfg.P2PoolBinPath, ""},
		{"starts with the computer", cfg.RunAtStartup, true},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.what, c.got, c.want)
		}
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("the config first run writes does not validate: %v", err)
	}
}

func TestOnboardingStartupDisclosureIsOnButCanBeRefused(t *testing.T) {
	o := newOnboarding(t)
	if !o.startup.Checked {
		t.Fatal("starting with the computer must be pre-checked")
	}
	o.wallet.SetText(sampleAddress)
	o.startup.SetChecked(false)
	cfg := config.Defaults()
	o.answers(cfg)
	if cfg.RunAtStartup {
		t.Error("unticking the box still registers kind-miner at login")
	}
}

func TestOnboardingOwnNodeIsUnderMoreOptions(t *testing.T) {
	o := newOnboarding(t)
	if o.node.Selected != nodeAutomatic {
		t.Errorf("default node = %q, want the automatic one", o.node.Selected)
	}
	o.wallet.SetText(sampleAddress)
	o.node.SetSelected(nodeOwn)
	cfg := config.Defaults()
	o.answers(cfg)
	if cfg.Mode != config.ModeP2PoolLocal {
		t.Errorf("mode = %q after choosing my own node", cfg.Mode)
	}
}

func TestOnboardingStartWaitsForAValidAddress(t *testing.T) {
	o := newOnboarding(t)
	if !o.start.Disabled() {
		t.Error("Start is enabled with no address")
	}
	o.wallet.SetText("4notanaddress")
	if !o.start.Disabled() {
		t.Error("Start is enabled with a malformed address")
	}
	// Shaped like an address, and each would make p2pool abort without a
	// word: Start must stay off, and the line must say which mistake it is.
	typo := []byte(sampleAddress)
	typo[40] = map[bool]byte{true: 'c', false: 'b'}[typo[40] == 'b']
	for _, c := range []struct{ name, addr, want string }{
		{"a one-character typo", string(typo), WalletErrChecksum},
		{"a subaddress", sampleSubaddress, WalletErrSubaddress},
	} {
		o.wallet.SetText(c.addr)
		if !o.start.Disabled() {
			t.Errorf("Start is enabled for %s", c.name)
		}
		if !strings.Contains(o.feedback.Text, c.want) {
			t.Errorf("for %s the feedback says %q", c.name, o.feedback.Text)
		}
	}
	o.wallet.SetText(sampleAddress)
	if o.start.Disabled() {
		t.Error("Start stays disabled for a valid address")
	}
}
