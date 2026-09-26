package engine

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// P2Pool manages the p2pool subprocess.
type P2Pool struct {
	binPath     string
	wallet      string
	nodeHost    string
	rpcPort     int
	zmqPort     int
	rpcLogin    string // "user:password" for a node with RPC login; "" for none
	noRandomX   bool   // the Nodo profile; see P2PoolOptions.NoRandomX
	lightMode   bool   // see P2PoolOptions.LightMode
	chain       string // "mini" or "main"
	stratumPort int    // local Stratum port XMRig connects to
	socks5Proxy string // e.g. "127.0.0.1:9050" for Tor; empty = direct
	workDir     string // p2pool's cwd — it writes p2pool.cache/log/peer lists there
	dataAPIDir  string // --data-api target; empty disables the JSON statistics

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	ready  bool
	// closed is set by Close. Start refuses afterwards, so a Reconnect racing a
	// shutdown cannot bring p2pool back once it has been put away.
	closed   bool
	lastLine string // most recent output line, for early-exit diagnostics

	statsState
	statsStop chan struct{}
}

// P2PoolOptions configures a P2Pool manager.
type P2PoolOptions struct {
	// BinPath may be empty to auto-locate the binary.
	BinPath string
	Wallet  string

	// NodeHost is the monerod to follow, with its RPC and ZMQ ports.
	NodeHost string
	RPCPort  int
	ZMQPort  int
	// RPCLogin is "user:password" for a node that requires RPC login.
	RPCLogin string

	// NoRandomX is the profile for running beside the node on a Nodo. p2pool
	// skips its own RandomX dataset and asks monerod to check share PoW, and
	// keeps neither a cache file nor a log file. That is ~2 GB of RAM and
	// nearly all of p2pool's disk writes left to the node, on a board whose
	// memory is the node's page cache and whose disk is the node's database.
	// It needs monerod's unrestricted RPC, which on a Nodo is 127.0.0.1:18081.
	NoRandomX bool

	// LightMode skips p2pool's own ~2 GB RandomX dataset and verifies shares
	// from the 256 MB cache instead. p2pool checks a sidechain block every few
	// seconds, not millions of hashes a second, so the slower verification
	// costs nothing noticeable — while the dataset was the second copy of 2 GB
	// on a machine already giving 2 GB to the miner, and it took the huge
	// pages reserved for the miner because p2pool starts first. Ignored with
	// NoRandomX, which needs neither.
	LightMode bool

	// Chain is "mini" or "main" (see config.ChainMini).
	Chain string

	// StratumPort is the local port XMRig connects to.
	StratumPort int

	// SOCKS5Proxy routes p2pool through Tor for .onion nodes; "" is direct.
	SOCKS5Proxy string

	// WorkDir is where p2pool keeps its cache and logs (it writes them to its
	// cwd); "" inherits the parent's cwd.
	WorkDir string

	// DataAPIDir makes p2pool write its JSON statistics there. Empty disables
	// them, in which case Stats reports nothing.
	DataAPIDir string
}

// NewP2Pool creates a manager.
func NewP2Pool(o P2PoolOptions) *P2Pool {
	return &P2Pool{
		binPath:     o.BinPath,
		wallet:      o.Wallet,
		nodeHost:    o.NodeHost,
		rpcPort:     o.RPCPort,
		zmqPort:     o.ZMQPort,
		rpcLogin:    o.RPCLogin,
		noRandomX:   o.NoRandomX,
		lightMode:   o.LightMode,
		chain:       o.Chain,
		stratumPort: o.StratumPort,
		socks5Proxy: o.SOCKS5Proxy,
		workDir:     o.WorkDir,
		dataAPIDir:  o.DataAPIDir,
	}
}

// StratumAddr returns the address XMRig should use to connect to this p2pool instance.
func (p *P2Pool) StratumAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", p.stratumPort)
}

// Start launches p2pool and waits until it signals readiness (up to timeout).
func (p *P2Pool) Start(timeout time.Duration) error {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return errP2PoolClosed
	}
	if p.cmd != nil {
		p.mu.Unlock()
		return nil
	}

	bin, err := p.resolve()
	if err != nil {
		p.mu.Unlock()
		return err
	}

	args := p.buildArgs()
	// --data-api writes the JSON stat files; --local-api adds the local/ ones
	// (our own hashrate and shares). Both are needed for the reward estimate.
	// --data-api is deliberately not affected by p2pool's --data-dir.
	if p.dataAPIDir != "" {
		if err := os.MkdirAll(p.dataAPIDir, 0o755); err != nil {
			log.Printf("p2pool: cannot create stats dir %s: %v", p.dataAPIDir, err)
			p.dataAPIDir = ""
		} else {
			args = append(args, "--data-api", p.dataAPIDir, "--local-api")
		}
	}
	if log.Writer() == io.Discard {
		args = append(args, "--loglevel", "0")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()
	if p.workDir != "" {
		// Best effort: p2pool works from any writable cwd, so fall back to
		// inheriting ours rather than failing (e.g. a read-only install prefix).
		if err := os.MkdirAll(p.workDir, 0o755); err == nil {
			cmd.Dir = p.workDir
		} else {
			log.Printf("p2pool work dir %s unavailable (%v); using current directory", p.workDir, err)
		}
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		p.mu.Unlock()
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		p.mu.Unlock()
		return fmt.Errorf("starting p2pool: %w", err)
	}

	p.cmd = cmd
	p.cancel = cancel
	p.ready = false

	readyCh := make(chan struct{}, 1)
	exitCh := make(chan struct{})
	go p.readOutput(stdout, readyCh, exitCh)

	if p.dataAPIDir != "" {
		p.statsStop = make(chan struct{})
		go p.pollStats(p.statsStop)
	}
	p.mu.Unlock() // release before blocking — readOutput needs the lock to set p.ready

	select {
	case <-readyCh:
		return nil
	case <-exitCh:
		// p2pool died before the Stratum server came up (bad arguments,
		// unusable binary, port already in use, …). Surface its last words —
		// GUI users never see the [p2pool] log lines.
		p.mu.Lock()
		last := p.lastLine
		p.mu.Unlock()
		if last != "" {
			return fmt.Errorf("p2pool exited before becoming ready; last output: %s", last)
		}
		return fmt.Errorf("p2pool exited before becoming ready and produced no output")
	case <-time.After(timeout):
		return fmt.Errorf("p2pool did not become ready within %s; check node connectivity", timeout)
	}
}

// buildArgs is the command line for everything but the stats directory, which
// Start adds once it has managed to create it.
func (p *P2Pool) buildArgs() []string {
	args := []string{
		"--host", p.nodeHost,
		"--rpc-port", fmt.Sprintf("%d", p.rpcPort),
		"--zmq-port", fmt.Sprintf("%d", p.zmqPort),
	}
	// --rpc-login belongs to the --host before it, so it must follow it.
	if p.rpcLogin != "" {
		args = append(args, "--rpc-login", p.rpcLogin)
	}
	args = append(args,
		"--wallet", p.wallet,
		"--stratum", fmt.Sprintf("0.0.0.0:%d", p.stratumPort),
		"--p2p", fmt.Sprintf("0.0.0.0:%d", p2pPortForChain(p.chain)),
		"--no-color",
	)
	if p.socks5Proxy != "" {
		args = append(args, "--socks5", p.socks5Proxy)
	}
	if flag := chainFlag(p.chain); flag != "" {
		args = append(args, flag)
	}
	switch {
	case p.noRandomX:
		args = append(args, "--no-randomx", "--no-cache", "--no-log-file")
	case p.lightMode:
		args = append(args, "--light-mode")
	}
	return args
}

// Reconnect restarts p2pool against the same node with a new ZMQ port and RPC
// login — what the owner changes in Nodo's UI. p2pool reads both only at
// start-up. The miner rides out the gap: xmrig keeps retrying the stratum port
// until p2pool is back.
func (p *P2Pool) Reconnect(zmqPort int, rpcLogin string, timeout time.Duration) error {
	p.Stop()
	p.mu.Lock()
	p.zmqPort, p.rpcLogin = zmqPort, rpcLogin
	p.mu.Unlock()
	return p.Start(timeout)
}

var errP2PoolClosed = errors.New("p2pool has been shut down")

// Close stops p2pool for good: any later Start, including one inside a
// Reconnect already under way, returns an error instead of launching it. A
// Start already waiting for readiness returns as soon as its process dies.
func (p *P2Pool) Close() {
	p.mu.Lock()
	p.closed = true
	p.mu.Unlock()
	p.Stop()
}

// Stop terminates the p2pool subprocess.
func (p *P2Pool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		return
	}
	if p.statsStop != nil {
		close(p.statsStop)
		p.statsStop = nil
	}
	p.cancel()
	_ = p.cmd.Wait()
	p.cmd = nil
	p.cancel = nil
	p.ready = false
}

// Ready reports whether p2pool has finished its initial sync and is accepting shares.
func (p *P2Pool) Ready() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ready
}

// readOutput scans p2pool stdout, signals readyCh when the sidechain is ready,
// and closes exitCh when the stream ends (i.e. the process has died).
func (p *P2Pool) readOutput(r io.Reader, readyCh chan<- struct{}, exitCh chan<- struct{}) {
	scanner := bufio.NewScanner(r)
	signalled := false
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[p2pool] %s", line)
		p.mu.Lock()
		if strings.TrimSpace(line) != "" {
			p.lastLine = line
		}
		p.mu.Unlock()
		if !signalled && isP2PoolReady(line) {
			p.mu.Lock()
			p.ready = true
			p.mu.Unlock()
			readyCh <- struct{}{}
			signalled = true
		}
	}
	close(exitCh)
}

// chainFlag returns the p2pool argument that selects a sidechain. The main
// chain is p2pool's default and has no flag, so an unrecognised chain yields
// "" and joins main — which is why config validates the value first.
func chainFlag(chain string) string {
	switch chain {
	case "mini":
		return "--mini"
	case "nano":
		return "--nano"
	}
	return ""
}

// p2pPortForChain returns the sidechain's default peer-to-peer port. Each
// sidechain has its own peer network, so they must not share a port.
func p2pPortForChain(chain string) int {
	switch chain {
	case "mini":
		return 37888
	case "nano":
		return 37890
	}
	return 37889
}

// isP2PoolReady returns true when the p2pool log line indicates the Stratum
// server is up and accepting miner connections.
func isP2PoolReady(line string) bool {
	// p2pool v4+ logs "StratumServer event loop started" when ready.
	return strings.Contains(line, "StratumServer event loop started")
}

func (p *P2Pool) resolve() (string, error) {
	if p.binPath != "" {
		if _, err := os.Stat(p.binPath); err == nil {
			return p.binPath, nil
		}
		return "", fmt.Errorf("p2pool not found at configured path %q", p.binPath)
	}

	exe, _ := os.Executable()
	name := "p2pool"
	if runtime.GOOS == "windows" {
		name = "p2pool.exe"
	}
	bundled := filepath.Join(filepath.Dir(exe), "bin", name)
	if _, err := os.Stat(bundled); err == nil {
		return bundled, nil
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("p2pool binary not found; install it or set p2pool_path in config")
	}
	return path, nil
}
