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
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/nodes"
	"github.com/kind-miner/kind-miner/internal/scheduler"
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

	mu sync.Mutex
	// started is when mining actually began, which is what the dashboard's
	// uptime counts — not how long the window has been open.
	started time.Time
	// nodeAddr is the monerod p2pool is following, for display.
	nodeAddr string
	// viaTor records whether that node is reached over Tor.
	viaTor bool
}

// New creates a Supervisor for cfg. It does not start anything.
func New(cfg *config.Config) *Supervisor {
	return &Supervisor{cfg: cfg}
}

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

	if s.cfg.Mode != config.ModePool && s.cfg.ManageP2Pool {
		emit(StepInstallP2Pool)
		p2poolPath, err := autoinstall.EnsureP2Pool(binDir)
		if err != nil {
			return err
		}
		if s.cfg.P2PoolBinPath == "" {
			s.cfg.P2PoolBinPath = p2poolPath
		}
	}

	poolURL, err := resolvePool(s.cfg)
	if err != nil {
		return err
	}

	// Start optional managed subprocesses.
	if s.cfg.Mode == config.ModeP2PoolLocal && s.cfg.ManageMonerod {
		emit(StepStartMonerod)
		s.monerod = engine.NewMonerod(s.cfg.MonerodPath)
		log.Println("Starting monerod (this may take a while for initial sync)…")
		if err := s.monerod.Start(5 * time.Minute); err != nil {
			return err
		}
	}

	if s.cfg.Mode != config.ModePool && s.cfg.ManageP2Pool {
		s.ensureTor(emit)
		emit(StepSelectNode)
		node, err := resolveNode(s.cfg)
		if err != nil {
			return err
		}
		log.Printf("Using monerod node: %s", node.Addr())

		var socks5Proxy string
		if node.TorOnly || strings.HasSuffix(node.Host, ".onion") {
			socks5Proxy = "127.0.0.1:9050"
			log.Printf("Routing p2pool through Tor (%s)", socks5Proxy)
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
		s.p2pool = engine.NewP2Pool(engine.P2PoolOptions{
			BinPath:     s.cfg.P2PoolBinPath,
			Wallet:      s.cfg.Wallet,
			NodeHost:    node.Host,
			RPCPort:     node.RPCPort,
			ZMQPort:     node.ZMQPort,
			Chain:       s.cfg.P2PoolChain,
			StratumPort: stratumPort,
			SOCKS5Proxy: socks5Proxy,
			WorkDir:     p2poolWorkDir,
			// Statistics feed the reward estimate in the tray.
			DataAPIDir: filepath.Join(p2poolWorkDir, "api"),
		})
		log.Println("Starting p2pool (syncing sidechain…)")
		if err := s.p2pool.Start(3 * time.Minute); err != nil {
			return err
		}
		log.Println("p2pool ready.")
	}

	emit(StepStartXMRig)
	// xmrig runs at a fixed thread count and is throttled by duty-cycling, so
	// this is the ceiling of raw capacity — the whole machine unless the user
	// capped it. The scheduler resolves 0 the same way, so the two agree on the
	// full-tilt share that a duty fraction is measured against.
	threads := s.cfg.MaxThreads
	if threads <= 0 {
		threads = runtime.NumCPU()
	}
	s.xmrig = engine.NewXMRig(s.cfg.XMRigBinPath, poolURL, s.cfg.Wallet, threads, 8080)
	if err := s.xmrig.Start(); err != nil {
		return err
	}

	s.sched = scheduler.New(s.cfg, s.xmrig)
	go s.sched.Start()

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
// reached over Tor. The address is empty in pool mode and before Start.
func (s *Supervisor) Node() (addr string, viaTor bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.nodeAddr, s.viaTor
}

// Scheduler returns the running scheduler, or nil before Start succeeds.
func (s *Supervisor) Scheduler() *scheduler.Scheduler { return s.sched }

// XMRig returns the running miner, or nil before Start succeeds.
func (s *Supervisor) XMRig() *engine.XMRig { return s.xmrig }

// P2Pool returns the running p2pool manager, or nil in pool mode or before
// Start succeeds.
func (s *Supervisor) P2Pool() *engine.P2Pool { return s.p2pool }

// Config returns the active configuration.
func (s *Supervisor) Config() *config.Config { return s.cfg }

// Shutdown stops the scheduler (which stops XMRig) and any managed
// subprocesses, in reverse order of startup. It is safe to call even if Start
// failed partway through.
func (s *Supervisor) Shutdown() {
	if s.sched != nil {
		s.sched.Stop()
	}
	if s.p2pool != nil {
		s.p2pool.Stop()
	}
	if s.monerod != nil {
		s.monerod.Stop()
	}
	if s.tor != nil {
		s.tor.Stop()
	}
}

// resolvePool returns the XMRig pool URL based on the configured mode.
func resolvePool(cfg *config.Config) (string, error) {
	switch cfg.Mode {
	case config.ModePool:
		return cfg.PoolURL, nil
	default:
		// p2pool modes: XMRig connects to the local p2pool Stratum port.
		return fmt.Sprintf("127.0.0.1:%d", stratumPort), nil
	}
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
// .onion; a custom clearnet remote_node, or the local/pool modes, do not.
func (s *Supervisor) torNeeded() bool {
	if s.cfg.Mode != config.ModeP2PoolRemote {
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
