// Package core wires together the mining engine lifecycle — binary
// installation, the monerod/p2pool/xmrig subprocesses, and the scheduler —
// independently of any presentation layer (GUI, tray, or headless).
//
// A Supervisor performs the same bring-up that used to live inline in main(),
// but reports progress through a callback and returns errors instead of
// terminating the process, so a GUI front-end can show a progress screen and
// surface failures as dialogs rather than a silent exit.
package core

import (
	"errors"
	"fmt"
	"log"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/hub"
	"github.com/kind-miner/kind-miner/internal/monitor"
	"github.com/kind-miner/kind-miner/internal/nodes"
	"github.com/kind-miner/kind-miner/internal/nodo"
	"github.com/kind-miner/kind-miner/internal/scheduler"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// stratumPort is the local port p2pool exposes and XMRig connects to.
const stratumPort = 3333

// Step identifies a stage of startup, for progress reporting.
type Step int

const (
	StepInstallXMRig Step = iota
	StepInstallP2Pool
	StepStartMonerod
	StepStartTor
	StepSelectNode
	StepStartP2Pool
	StepStartXMRig
	StepReady
)

// String returns a short human-readable label for the step.
func (s Step) String() string {
	switch s {
	case StepInstallXMRig:
		return "Installing XMRig"
	case StepInstallP2Pool:
		return "Installing p2pool"
	case StepStartMonerod:
		return "Starting Monero node"
	case StepStartTor:
		return "Starting Tor"
	case StepSelectNode:
		return "Finding a Monero node"
	case StepStartP2Pool:
		return "Starting p2pool"
	case StepStartXMRig:
		return "Starting the miner"
	case StepReady:
		return "Ready"
	}
	return "Working"
}

// Supervisor owns the running mining stack for one configuration.
// It is created with New, brought up with Start, and torn down with Shutdown.
type Supervisor struct {
	cfg     *config.Config
	xmrig   *engine.XMRig
	sched   *scheduler.Scheduler
	monerod *engine.Monerod
	p2pool  *engine.P2Pool
	tor     *engine.Tor
	// relay carries RPC and ZMQ to an onion node over Tor; nil for any other.
	relay *nodes.Relay
	// localNode is the monerod found running on this machine, when automatic
	// node selection found a usable one; nil otherwise.
	localNode *nodes.Node
	// localWatchStop ends the watch for a local node to become usable.
	localWatchStop chan struct{}
	// ledgerStop ends the periodic save of the hashrate ledger.
	ledgerStop chan struct{}
	// payouts is the record of payouts seen in p2pool's log. Never nil, so the
	// tray can show "none yet" before mining starts.
	payouts *stats.Payouts
	// islandStop ends the sidechain health check; nil when p2pool is not ours.
	islandStop chan struct{}

	mu sync.Mutex
	// started is when mining actually began, which is what the dashboard's
	// uptime counts — not how long the window has been open.
	started time.Time
	// nodeAddr is the monerod p2pool is following, for display.
	nodeAddr string
	// viaTor records whether that node is reached over Tor.
	viaTor bool
	// wifiIface names the active wireless interface, and wifiPowerSave whether
	// it sleeps between beacons. wifiPowerSaveKnown separates "checked, it is
	// off" from "could not check" — a wired machine must not read as either.
	wifiIface          string
	wifiPowerSave      bool
	wifiPowerSaveKnown bool
	// nodo holds this machine's Nodo settings, nil on anything else. nodoStop
	// ends the watch on Nodo's config.json.
	nodo     *nodo.Node
	nodoStop chan struct{}

	// hubAPI serves the household statistics when this machine is the hub,
	// and hubTor publishes it as an onion service when hub.onion is on.
	hubAPI    *hub.Server
	hubBeacon *hub.Announcer
	hubTor    *engine.Tor
	// hubWallet is the owner's wallet change; see SetHubWallet.
	hubWallet func(address string) error
	// hhStop ends the polling of the hub a paired machine mines to; hh is its
	// last answer (hhOK once there is one) and hhErr the last failure.
	hhStop chan struct{}
	hh     hub.Household
	hhOK   bool
	hhErr  error
}

// New creates a Supervisor for cfg. It does not start anything.
func New(cfg *config.Config) *Supervisor {
	return &Supervisor{cfg: cfg, payouts: stats.NewPayouts()}
}

// Payouts returns the record of payouts seen while kind-miner was running.
func (s *Supervisor) Payouts() *stats.Payouts { return s.payouts }

// Start installs any missing binaries, launches the configured subprocesses,
// and starts the scheduler loop in a background goroutine. progress (may be
// nil) is invoked as each Step begins. Start blocks until mining has begun or
// an error occurs; it returns the first error without tearing down what it had
// already started — call Shutdown to clean up.
func (s *Supervisor) Start(progress func(Step)) error {
	emit := func(st Step) {
		if progress != nil {
			progress(st)
		}
	}

	s.checkWiFiPowerSave()
	onNodo, profile := s.detectNodo()

	// Auto-download any missing binaries before we need them.
	binDir := autoinstall.BinDir()
	emit(StepInstallXMRig)
	xmrigPath, err := autoinstall.EnsureXMRig(binDir)
	if err != nil {
		return err
	}
	if s.cfg.XMRigBinPath == "" {
		s.cfg.XMRigBinPath = xmrigPath
	}

	// A machine paired with a hub runs xmrig alone; everything else is the hub's.
	var pairing hub.Pairing
	if s.cfg.Mode == config.ModeHub {
		if pairing, err = hub.ParseCode(s.cfg.HubCode); err != nil {
			return fmt.Errorf("hub_code: %w", err)
		}
		s.mu.Lock()
		s.nodeAddr = pairing.StratumAddr()
		s.mu.Unlock()
	}
	var hubID hub.Identity
	if hubServing(s.cfg) {
		if hubID, err = hub.Ensure(HubDir()); err != nil {
			return fmt.Errorf("setting up the household hub: %w", err)
		}
	}

	if s.cfg.ManageP2Pool && s.cfg.Mode != config.ModeHub {
		emit(StepInstallP2Pool)
		p2poolPath, err := autoinstall.EnsureP2Pool(binDir)
		if err != nil {
			return err
		}
		if s.cfg.P2PoolBinPath == "" {
			s.cfg.P2PoolBinPath = p2poolPath
		}
	}

	// Start optional managed subprocesses.
	// A Nodo's monerod belongs to Nodo: it is already running, a second one
	// would fight it for the port and the database.
	if profile && s.cfg.ManageMonerod {
		log.Println("Nodo detected: leaving monerod to Nodo (ignoring manage_monerod)")
	} else if s.cfg.Mode == config.ModeP2PoolLocal && s.cfg.ManageMonerod {
		emit(StepStartMonerod)
		s.monerod = engine.NewMonerod(s.cfg.MonerodPath)
		log.Println("Starting monerod (this may take a while for initial sync)…")
		if err := s.monerod.Start(5 * time.Minute); err != nil {
			return err
		}
	}

	if s.cfg.ManageP2Pool && s.cfg.Mode != config.ModeHub {
		// Looked for before Tor: a usable node on this machine means Tor is
		// not needed at all.
		s.findLocalNode(profile)
		s.ensureTor(emit)
		emit(StepSelectNode)
		var node nodes.Node
		switch {
		case profile:
			node = nodoNode(onNodo)
		case s.localNode != nil:
			node = *s.localNode
		default:
			var err error
			if node, err = resolveNode(s.cfg); err != nil {
				return err
			}
		}
		log.Printf("Using monerod node: %s", node.Addr())

		// follow is what p2pool is pointed at: the node itself, or for an onion
		// node the loopback relay standing in for it.
		follow := node
		var socks5Proxy string
		if routesOverTor(node) {
			// p2pool's --socks5 keeps its peer traffic on Tor, but it cannot carry
			// ZMQ, so the node itself is reached through the relay.
			socks5Proxy = "127.0.0.1:9050"
			relay, err := nodes.StartRelay(node, 0)
			if err != nil {
				return fmt.Errorf("starting the Tor relay for %s: %w", node.Addr(), err)
			}
			s.relay = relay
			follow = relay.Local()
			log.Printf("Routing p2pool through Tor (%s), node via relay %s", socks5Proxy, follow.Addr())
		}

		s.mu.Lock()
		s.nodeAddr = node.Addr()
		s.viaTor = socks5Proxy != ""
		s.mu.Unlock()

		emit(StepStartP2Pool)
		// Keep p2pool's cache/log/peer files in a persistent data dir instead of
		// whatever cwd we inherited — in a Flatpak that cwd is a throwaway tmpfs
		// (the ~450 MB p2pool.cache would live in RAM and vanish on exit).
		p2poolWorkDir := filepath.Join(filepath.Dir(autoinstall.BinDir()), "p2pool")
		statsDir := filepath.Join(p2poolWorkDir, "api")
		if profile {
			statsDir = nodoStatsDir(statsDir)
		}
		opts := engine.P2PoolOptions{
			BinPath:   s.cfg.P2PoolBinPath,
			Wallet:    s.cfg.Wallet,
			NodeHost:  follow.Host,
			RPCPort:   follow.RPCPort,
			ZMQPort:   follow.ZMQPort,
			RPCLogin:  onNodo.RPCLogin,
			NoRandomX: profile,
			// Everywhere else p2pool verifies from the RandomX cache and leaves
			// the 2 GB dataset — and the huge pages — to the miner.
			LightMode:   true,
			Chain:       s.cfg.P2PoolChain,
			StratumPort: s.stratumPort(),
			SOCKS5Proxy: socks5Proxy,
			WorkDir:     p2poolWorkDir,
			// Statistics feed the reward estimate in the tray.
			DataAPIDir: statsDir,
		}
		if hubServing(s.cfg) {
			opts.TLSCert, opts.TLSKey = hubID.CertPath, hubID.KeyPath
		}
		s.p2pool = engine.NewP2Pool(opts)
		if err := s.payouts.Load(payoutsPath()); err != nil {
			log.Printf("payouts: starting afresh (%v)", err)
		}
		s.p2pool.SetPayoutHandler(func(height, atomic uint64) {
			recordPayout(s.payouts, payoutsPath(), height, atomic, time.Now())
		})
		log.Println("Starting p2pool (syncing sidechain…)")
		if err := s.p2pool.Start(3 * time.Minute); err != nil {
			return err
		}
		log.Println("p2pool ready.")
		if hubServing(s.cfg) {
			s.startHubAPI(hubID)
			if s.cfg.Hub.Onion {
				s.startHubOnion()
			}
		}
		s.startLocalWatch(profile)
		if profile {
			s.watchNodo(onNodo)
		}
	}

	emit(StepStartXMRig)
	// xmrig runs at a fixed thread count and is throttled by duty-cycling, so
	// this is the ceiling of raw capacity. It has to come from the same function
	// the scheduler uses, or the two disagree about the full-tilt share a duty
	// fraction is measured against and every reported percentage is wrong.
	threads := scheduler.Threads(s.cfg)
	// Checked here rather than at startup: p2pool has already taken its share
	// of the pool by now, and its share is the whole problem.
	s.checkHugePages(threads)
	// The household hub over pinned TLS, or else the local p2pool: the one
	// kind-miner manages, or with manage_p2pool off, the one the user runs on
	// the default port.
	if s.cfg.Mode == config.ModeHub {
		s.xmrig = engine.NewXMRig(s.cfg.XMRigBinPath, pairing.StratumAddr(), threads, 8080)
		name := workerName(s.cfg)
		s.xmrig.UseHub(name, pairing.FingerprintHex())
		if pairing.Onion != "" {
			// Tor only for the fallback; at home everything stays on the LAN.
			s.ensureTor(emit)
			s.xmrig.UseHubFallback(pairing.OnionStratumAddr(), torSOCKSFor(pairing))
		}
		log.Printf("Mining to the household hub at %s as %q", pairing.StratumAddr(), name)
		s.hhStop = make(chan struct{})
		go s.watchHousehold(pairing, s.hhStop)
	} else {
		s.xmrig = engine.NewXMRig(s.cfg.XMRigBinPath, fmt.Sprintf("127.0.0.1:%d", s.stratumPort()), threads, 8080)
		if hubServing(s.cfg) {
			// Named like every other device, so the hub's list includes itself.
			s.xmrig.SetUser(workerName(s.cfg))
		}
	}
	// xmrig runs from a config file it watches, so the scheduler can move it
	// between thread layouts without a restart. It starts on the small one.
	s.xmrig.UseConfigFile(filepath.Join(filepath.Dir(autoinstall.BinDir()), "xmrig.json"))
	if present, _, ok := scheduler.Layouts(s.cfg); ok {
		s.xmrig.SetLayout(present)
	}
	if s.cfg.MineOnNodo {
		total, available, ok := monitor.Memory()
		mode := randomXModeFor(available, ok)
		log.Printf("Mining on a Nodo: RandomX %s mode (%s available of %s)",
			mode, gib(available, ok), gib(total, ok))
		s.xmrig.SetRandomXMode(mode)
	}
	if err := s.xmrig.Start(); err != nil {
		return err
	}

	s.sched = scheduler.New(s.cfg, s.xmrig)
	restoreLedger(s.sched.Ledger(), ledgerPath())
	keepPauseAcrossRestarts(s.sched, snoozePath())
	s.ledgerStop = make(chan struct{})
	go saveLedgerEvery(s.sched.Ledger(), ledgerPath(), ledgerSaveInterval, s.ledgerStop)
	go s.sched.Start()
	if s.p2pool != nil {
		s.islandStop = make(chan struct{})
		go s.watchIslands(s.islandStop)
	}

	s.mu.Lock()
	s.started = time.Now()
	s.mu.Unlock()

	emit(StepReady)
	return nil
}

// Uptime returns how long mining has been running. ok is false before Start
// succeeds.
func (s *Supervisor) Uptime() (time.Duration, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started.IsZero() {
		return 0, false
	}
	return time.Since(s.started), true
}

// Node returns the monerod address p2pool is following and whether it is
// reached over Tor. The address is empty with manage_p2pool off and before Start.
func (s *Supervisor) Node() (addr string, viaTor bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodeAddr, s.viaTor
}

// checkWiFiPowerSave records and logs a warning when the active wireless link
// sleeps between beacons. See monitor.WiFiPowerSave for the measured failure
// this catches: mining load turns that sleep into multi-second network stalls,
// which is the opposite of getting out of the user's way.
//
// It only ever warns. Refusing to mine over a power-saving link would be a
// worse trade than a slow network, and the setting is the user's to make.
func (s *Supervisor) checkWiFiPowerSave() {
	iface, enabled, ok := monitor.WiFiPowerSave()
	s.mu.Lock()
	s.wifiIface, s.wifiPowerSave, s.wifiPowerSaveKnown = iface, enabled, ok
	s.mu.Unlock()
	if !ok || !enabled {
		return
	}
	log.Printf("warning: Wi-Fi power save is on for %s. Mining can stall this "+
		"machine's network for seconds at a time while the radio is asleep. "+
		"Disable it with: nmcli con modify <connection> wifi.powersave 2", iface)
}

// checkHugePages warns when the huge page pool will not cover the miner.
//
// Falling back to 4 KiB pages costs a large fraction of the hashrate and xmrig
// does it silently, so without this the machine simply mines slowly for no
// visible reason. See monitor.HugePagesFor for why p2pool is what usually
// exhausts a pool that looks big enough.
func (s *Supervisor) checkHugePages(threads int) {
	short, wantPages := monitor.HugePagesFor(threads)
	if !short {
		return
	}
	log.Printf("warning: not enough free huge pages for the miner's RandomX dataset, "+
		"so it will fall back to 4 KiB pages and hash considerably slower. p2pool "+
		"starts first and keeps its RandomX caches (about 0.5 GB) there too, so the "+
		"pool has to cover both: "+
		"sudo sysctl -w vm.nr_hugepages=%d", wantPages)
}

// pageCacheReserve is the memory a Nodo must still have free after the RandomX
// dataset is allocated. That memory is not idle: the kernel uses it as page
// cache for monerod's and the light-wallet server's LMDB databases, and every
// gigabyte the miner pins is a gigabyte of blockchain that has to come off the
// disk again. Four gigabytes keeps the hot part of the chain cached.
const pageCacheReserve int64 = 4 << 30

// randomXModeFor picks the RandomX mode on a Nodo from the memory available at
// launch. Fast mode's ~2 GB dataset is taken only when the page cache keeps
// its reserve afterwards; otherwise light mode's 256 MB cache hashes slower
// but leaves the node its memory. Unknown memory takes the kind answer.
//
// It is decided once, at launch, because the mode is fixed for the life of the
// miner and changing it means re-initialising RandomX.
func randomXModeFor(available int64, known bool) string {
	if known && available-monitor.RandomXDatasetBytes >= pageCacheReserve {
		return "fast"
	}
	return "light"
}

func gib(n int64, known bool) string {
	if !known {
		return "unknown"
	}
	return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
}

// WiFiPowerSave reports the active wireless interface and whether it runs with
// power save on. ok is false on a wired machine, or where the setting could not
// be read; callers must not render that as "off".
func (s *Supervisor) WiFiPowerSave() (iface string, enabled, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.wifiIface, s.wifiPowerSave, s.wifiPowerSaveKnown
}

// Scheduler returns the running scheduler, or nil before Start succeeds.
func (s *Supervisor) Scheduler() *scheduler.Scheduler { return s.sched }

// XMRig returns the running miner, or nil before Start succeeds.
func (s *Supervisor) XMRig() *engine.XMRig { return s.xmrig }

// P2Pool returns the running p2pool manager, or nil with manage_p2pool off or before
// Start succeeds.
func (s *Supervisor) P2Pool() *engine.P2Pool { return s.p2pool }

// Config returns the active configuration.
func (s *Supervisor) Config() *config.Config { return s.cfg }

// Shutdown stops the scheduler (which stops XMRig) and any managed
// subprocesses, in reverse order of startup. It is safe to call even if Start
// failed partway through.
func (s *Supervisor) Shutdown() {
	// Order matters. The scheduler goes first so nothing resumes the miner
	// underneath us, then the miner itself, and only then p2pool — stopping the
	// pool first would leave xmrig hashing against a dead stratum for as long as
	// p2pool takes to write its ~450 MB cache out.
	if s.islandStop != nil {
		close(s.islandStop)
		s.islandStop = nil
	}
	if s.sched != nil {
		s.sched.Stop()
		// After the scheduler, so its last tick is in the record.
		if s.ledgerStop != nil {
			close(s.ledgerStop)
			s.ledgerStop = nil
		}
		if err := s.sched.Ledger().Save(ledgerPath()); err != nil {
			log.Printf("hashrate history: could not save: %v", err)
		}
	}
	// Explicit rather than left to the scheduler: Shutdown has to stop
	// everything Start launched, or a future change to either one silently
	// orphans a mining process.
	if s.xmrig != nil {
		s.xmrig.Close()
	}
	s.mu.Lock()
	if s.nodoStop != nil {
		close(s.nodoStop)
		s.nodoStop = nil
	}
	if s.localWatchStop != nil {
		close(s.localWatchStop)
		s.localWatchStop = nil
	}
	if s.hhStop != nil {
		close(s.hhStop)
		s.hhStop = nil
	}
	hubAPI, hubBeacon, hubTor := s.hubAPI, s.hubBeacon, s.hubTor
	s.hubAPI, s.hubBeacon, s.hubTor = nil, nil, nil
	s.mu.Unlock()
	if hubBeacon != nil {
		hubBeacon.Close()
	}
	if hubTor != nil {
		hubTor.Close()
	}
	// Before p2pool: a paired desktop asking for numbers while p2pool writes
	// its cache out would be told the household had gone quiet.
	if hubAPI != nil {
		hubAPI.Close()
	}
	// Close rather than Stop: the Nodo watch may be restarting p2pool at this
	// very moment, and Close is what stops that restart from outliving us.
	if s.p2pool != nil {
		s.p2pool.Close()
	}
	if s.monerod != nil {
		s.monerod.Stop()
	}
	// After p2pool, which would otherwise log a burst of failed RPC calls
	// against a relay that had vanished underneath it. Taken under the lock:
	// a switch to the local node may have handed them off already.
	s.mu.Lock()
	relay, tor := s.relay, s.tor
	s.relay, s.tor = nil, nil
	s.mu.Unlock()
	if relay != nil {
		relay.Close()
	}
	if tor != nil {
		tor.Stop()
	}
}

// stratumPort is the port our p2pool serves stratum on: the hub's, when this
// machine is the hub. p2pool listens on a single port — given two it
// refuses to run — so the hub's own miner connects to the LAN port too, in
// the clear over loopback.
func (s *Supervisor) stratumPort() int {
	if hubServing(s.cfg) {
		port, _ := s.cfg.HubPorts()
		return port
	}
	return stratumPort
}

// StratumAddr is where the miner connects when kind-miner does not run p2pool
// itself (manage_p2pool off): the default Stratum port on loopback.
func StratumAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", stratumPort)
}

// resolveNode returns the monerod node for p2pool to connect to.
// For remote modes it ensures Tor is running before selecting a node, since
// the primary node is only reachable via .onion. Retries up to 3 times with a
// 30s delay to handle transient Tor circuit failures or a briefly-rebooting
// Nodo.
func resolveNode(cfg *config.Config) (nodes.Node, error) {
	switch cfg.Mode {
	case config.ModeP2PoolLocal:
		return nodes.Node{Host: "127.0.0.1", RPCPort: 18081, ZMQPort: 18083}, nil
	default:
		const maxAttempts = 3
		const retryDelay = 30 * time.Second
		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			node, err := nodes.SelectBest(cfg.RemoteNode)
			if err == nil {
				return node, nil
			}
			// Only unreachability is worth a retry. A malformed address parses
			// the same way every time, so retrying it just spends 60s before
			// showing the user the error they could have had immediately.
			if errors.Is(err, nodes.ErrBadAddr) {
				return nodes.Node{}, err
			}
			lastErr = err
			if attempt < maxAttempts {
				log.Printf("Node selection failed (attempt %d/%d): %v", attempt, maxAttempts, err)
				log.Printf("Retrying in %s…", retryDelay)
				time.Sleep(retryDelay)
			}
		}
		return nodes.Node{}, lastErr
	}
}

// torSOCKSPort is the local SOCKS5 port kind-miner's Tor (managed or system)
// listens on; p2pool uses it to reach .onion nodes.
const torSOCKSPort = 9050

// ensureTor makes a Tor SOCKS proxy available for .onion node access when the
// configuration needs one. It is best-effort: if Tor can't be brought up, node
// selection surfaces a clear, actionable error. Preference order:
//  1. a Tor already listening on 127.0.0.1:9050,
//  2. a Tor process kind-miner launches and manages itself (no root needed),
//  3. the system Tor service (systemctl/service/brew — may need privileges).
func (s *Supervisor) ensureTor(emit func(Step)) {
	if !s.torNeeded() || nodes.TorAvailable() {
		return
	}
	if s.cfg.ManageTor {
		emit(StepStartTor)
		// Locate a tor binary; download a verified copy if there isn't one.
		torBin, err := engine.FindTor(s.cfg.TorBinPath)
		if err != nil {
			if p, derr := autoinstall.EnsureTor(autoinstall.BinDir()); derr == nil {
				torBin = p
			} else {
				log.Printf("could not obtain tor: %v", derr)
			}
		}
		if torBin != "" {
			dataDir := filepath.Join(autoinstall.BinDir(), "tor-data")
			t := engine.NewTor(torBin, dataDir, torSOCKSPort)
			log.Println("No Tor detected — starting a managed Tor process…")
			if err := t.Start(90 * time.Second); err != nil {
				log.Printf("managed Tor unavailable (%v); trying the system Tor service…", err)
			} else {
				s.tor = t
				log.Println("Tor ready.")
				return
			}
		}
	}
	if nodes.EnsureTor() {
		log.Println("System Tor service is running.")
	}
}

// torNeeded reports whether the active configuration will connect to a .onion
// monerod and therefore needs Tor. The default remote node (the Nodo) is
// .onion; a custom clearnet remote_node, or the local mode, do not.
func (s *Supervisor) torNeeded() bool {
	if s.cfg.Mode == config.ModeHub {
		// Only for the hub's onion, the way back to it from away.
		p, err := hub.ParseCode(s.cfg.HubCode)
		return err == nil && p.Onion != ""
	}
	if s.cfg.Mode != config.ModeP2PoolRemote || s.localNode != nil {
		return false
	}
	if s.cfg.RemoteNode == "" {
		return true
	}
	host := s.cfg.RemoteNode
	if h, _, err := net.SplitHostPort(s.cfg.RemoteNode); err == nil {
		host = h
	}
	return strings.HasSuffix(host, ".onion")
}
