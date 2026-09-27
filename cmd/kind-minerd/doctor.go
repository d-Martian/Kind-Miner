package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/monitor"
	"github.com/kind-miner/kind-miner/internal/nodes"
	"github.com/kind-miner/kind-miner/internal/nodo"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

// doctor answers "why isn't it mining?" in one sentence. A headless box has
// no dashboard, and whoever asks is often on a phone over SSH; the most
// useful answer is the single most important problem and the command that
// fixes it, not a report.

// facts is what doctor looks at, gathered once so diagnose can be tested
// without a machine.
type facts struct {
	configMissing bool
	configErr     error
	nodoErr       error

	running bool
	status  daemonStatus

	torNeeded, torAvailable, manageTor bool

	hugePagesShort bool
	hugePagesWant  int64

	local nodes.LocalNode
}

// diagnose returns the most important problem, in priority order, and whether
// there is one. The order is what blocks mining first: no config stops
// everything, and a short huge page pool only slows it.
func diagnose(f facts) (string, bool) {
	switch {
	case f.configMissing:
		return "There is no config yet: run kind-minerd init --address 4… with your Monero address.", true
	case f.configErr != nil:
		return fmt.Sprintf("The config is not usable: %v. Edit %s.", f.configErr, config.Path()), true
	case f.nodoErr != nil:
		return fmt.Sprintf("This looks like a Nodo, but its settings could not be read: %v.", f.nodoErr), true
	case !f.running:
		return "kind-minerd is not running: start it with systemctl start kind-minerd, or kind-minerd run.", true
	case f.status.State == scheduler.StatePaused.String() && f.status.Reason != "":
		return fmt.Sprintf("Mining is stopped: %s.", f.status.Reason), true
	case f.torNeeded && !f.torAvailable && !f.manageTor:
		return "The node is an onion address but no Tor is running on 127.0.0.1:9050, and manage_tor is off: start Tor, or set manage_tor: true.", true
	case f.hugePagesShort:
		return fmt.Sprintf("Huge pages are short, so RandomX runs slower than it could: sudo sysctl -w vm.nr_hugepages=%d.", f.hugePagesWant), true
	case f.local.State != nodes.LocalAbsent && f.local.State != nodes.LocalUsable:
		// Not a fault — mining carries on against the remote node — but the
		// one thing that would make it better.
		d := f.local.Detail
		return strings.ToUpper(d[:1]) + d[1:] + ".", true
	}
	return fmt.Sprintf("Everything looks fine: %s at %.0f H/s.", f.status.State, f.status.Hashrate), false
}

// gatherFacts reads the machine.
func gatherFacts() facts {
	var f facts
	cfg, err := config.Load()
	switch {
	case errors.Is(err, os.ErrNotExist):
		f.configMissing = true
		return f
	case err != nil:
		f.configErr = err
		return f
	}
	if err := cfg.Validate(); err != nil {
		f.configErr = err
		return f
	}
	if _, _, err := nodo.Detect(); err != nil {
		f.nodoErr = err
	}
	f.status, f.running, _ = readStatus(readPath(), time.Now())
	f.manageTor = cfg.ManageTor
	if cfg.Mode == config.ModeP2PoolRemote {
		host := cfg.RemoteNode
		f.torNeeded = host == "" || strings.Contains(host, ".onion")
		f.torAvailable = nodes.TorAvailable()
	}
	f.hugePagesShort, f.hugePagesWant = monitor.HugePagesFor(scheduler.Threads(cfg))
	if cfg.Mode == config.ModeP2PoolRemote && cfg.RemoteNode == "" {
		f.local = nodes.ProbeLocal()
		if f.local.State == nodes.LocalUsable {
			f.torNeeded = false
		}
	}
	return f
}

// runDoctor prints the diagnosis and returns 0 when all is well, 1 when not.
func runDoctor(w io.Writer) int {
	msg, problem := diagnose(gatherFacts())
	fmt.Fprintln(w, msg)
	if problem {
		return 1
	}
	return 0
}
