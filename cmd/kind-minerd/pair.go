package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/hub"
)

// serviceHubDir is the system service's hub identity: its StateDirectory=,
// which root reads straight through the DynamicUser= private directory.
const serviceHubDir = "/var/lib/kind-miner/hub"

// hubDirFor is where the running daemon keeps its hub identity. pair only
// reads it — the daemon makes it — so a root shell never leaves a key behind
// that the service's own user cannot read.
func hubDirFor(configPath string) string {
	if configPath == serviceConfig {
		return serviceHubDir
	}
	return core.HubDir()
}

// runPair prints the pairing code and, given a device name, the pools block
// for a machine running plain xmrig.
func runPair(w io.Writer, name, host string) error {
	cfg, err := config.Load()
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no config at %s; run: kind-minerd init --address 4…", config.Path())
	}
	if err != nil {
		return err
	}
	if !cfg.Hub.Serve {
		return fmt.Errorf("this machine is not serving the hub yet: add\n\n  hub:\n    serve: true\n\nto %s and restart kind-minerd", config.Path())
	}
	dir := hubDirFor(config.Path())
	id, err := hub.Load(dir)
	if errors.Is(err, os.ErrNotExist) {
		return errors.New("the hub has not started yet: restart kind-minerd (systemctl restart kind-minerd), then pair again")
	}
	if err != nil {
		return err
	}
	if host == "" {
		if host, err = lanAddress(); err != nil {
			return fmt.Errorf("cannot tell this machine's LAN address (%v); give it with --host", err)
		}
	}
	var onion string
	if cfg.Hub.Onion {
		if onion, err = hub.ReadOnion(dir); err != nil {
			return fmt.Errorf("reading the hub's onion address: %w", err)
		}
		if onion == "" {
			// Still a usable pairing — the LAN part works now — so it is
			// printed, with what it lacks said plainly.
			fmt.Fprintln(w, "The hub's onion service has not started yet, so this code works on the LAN only.\n"+
				"Pair again in a minute for one that also works away from home.")
			fmt.Fprintln(w)
		}
	}
	stratumPort, apiPort := cfg.HubPorts()
	p := id.PairingFor(host, onion, stratumPort, apiPort)

	fmt.Fprintf(w, "Pairing code for the hub at %s:\n\n  %s\n\n", host, p.Code())
	if onion != "" {
		fmt.Fprintf(w, "Away from home, paired devices reach it through Tor at %s.\n", onion)
	}
	fmt.Fprintln(w, "On a desktop: Settings → Connection, choose mode \"hub\", and paste the code.")
	fmt.Fprintf(w, "Certificate fingerprint (SHA-256): %s\n", p.FingerprintHex())
	if name == "" {
		fmt.Fprintln(w, "\nFor a machine running plain xmrig: kind-minerd pair --name <device>")
		return nil
	}
	fmt.Fprintf(w, "\nFor %q running plain xmrig, use these pools in its config.json.\n"+
		"It shows up as %q in kind-minerd status:\n\n%s\n", name, hub.WorkerName(name), hub.DeviceConfig(p, name))
	if onion != "" {
		fmt.Fprintf(w, "The second pool reaches the hub through a Tor running on that device at %s.\n", hub.TorSOCKS)
	}
	return nil
}

// lanAddress is the address other machines reach this one at: the source
// address of the default route. A UDP "connection" sends nothing; it only asks
// the kernel which interface it would use. 203.0.113.1 is a documentation
// address, never a real peer.
func lanAddress() (string, error) {
	c, err := net.Dial("udp4", "203.0.113.1:9")
	if err != nil {
		return "", err
	}
	defer c.Close()
	addr, ok := c.LocalAddr().(*net.UDPAddr)
	if !ok || addr.IP.IsUnspecified() {
		return "", errors.New("no route")
	}
	return addr.IP.String(), nil
}
