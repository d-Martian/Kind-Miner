package gui

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"fyne.io/fyne/v2/test"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/kindness"
)

// newSettingsTest is a desktop with a valid config and nothing saved yet,
// whose settings window has just opened.
func newSettingsTest(t *testing.T) (*uiApp, *config.Config, *settingsForm) {
	t.Helper()
	test.NewApp()
	config.SetPath(filepath.Join(t.TempDir(), "config.yaml"))
	t.Cleanup(func() { config.SetPath("") })
	cfg := config.Defaults()
	cfg.Wallet = ownWallet
	u := &uiApp{sup: core.New(cfg)}
	return u, cfg, u.newSettingsForm(cfg)
}

// A rejected Save must leave the running configuration as it was. The window
// edits the Supervisor's own config, so whatever a rejected form leaves in it
// is written out by the next save from anywhere else — a kindness change on
// the dashboard — and the launch after that stops at "edit the config file".
func TestRejectedSettingsLeaveTheConfigAlone(t *testing.T) {
	cases := []struct {
		name string
		edit func(f *settingsForm)
		want string // part of the message on screen
	}{
		{
			// Only Validate knows what a node address must look like, so this
			// is refused after every field has parsed.
			name: "a node with no port",
			edit: func(f *settingsForm) { f.nodeAddr.SetText("my-node.lan") },
			want: "host:port",
		},
		{
			name: "a temperature that is not a number",
			edit: func(f *settingsForm) { f.tempLimit.SetText("hot") },
			want: "Temperature limit",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			u, cfg, f := newSettingsTest(t)
			before := *cfg
			// A change the form would otherwise have applied.
			f.kindness.Selected = kindness.Get(kindness.Full).Option()
			c.edit(f)

			msg := f.apply(u, cfg)
			if !strings.Contains(msg, c.want) {
				t.Fatalf("message %q, want one mentioning %q", msg, c.want)
			}
			if !reflect.DeepEqual(*cfg, before) {
				t.Errorf("the rejected form reached the running config:\n got %+v\nwant %+v", *cfg, before)
			}
			if _, err := os.Stat(config.Path()); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("a rejected form was saved (stat: %v)", err)
			}
		})
	}
}

func TestAcceptedSettingsAreAppliedAndSaved(t *testing.T) {
	u, cfg, f := newSettingsTest(t)
	f.kindness.Selected = kindness.Get(kindness.Full).Option()
	f.nodeAddr.SetText(" my-node.lan:18089 ")

	if msg := f.apply(u, cfg); msg != "" {
		t.Fatal(msg)
	}
	if cfg.Kindness != kindness.Full || cfg.RemoteNode != "my-node.lan:18089" {
		t.Errorf("running config has kindness %q, node %q", cfg.Kindness, cfg.RemoteNode)
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	if saved.Kindness != kindness.Full || saved.RemoteNode != "my-node.lan:18089" {
		t.Errorf("saved config has kindness %q, node %q", saved.Kindness, saved.RemoteNode)
	}
	if !f.needsRestart(cfg) {
		t.Error("a node change did not ask for a restart")
	}
}
