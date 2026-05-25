package engine

import (
	"bufio"
	"context"
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
	chain       string // "mini" or "main"
	stratumPort int    // local Stratum port XMRig connects to
	socks5Proxy string // e.g. "127.0.0.1:9050" for Tor; empty = direct

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	ready  bool
}

// NewP2Pool creates a manager. binPath may be empty to auto-locate.
// socks5Proxy is the SOCKS5 proxy address for Tor .onion nodes; pass "" for direct connections.
func NewP2Pool(binPath, wallet, nodeHost string, rpcPort, zmqPort int, chain string, stratumPort int, socks5Proxy string) *P2Pool {
	return &P2Pool{
		binPath:     binPath,
		wallet:      wallet,
		nodeHost:    nodeHost,
		rpcPort:     rpcPort,
		zmqPort:     zmqPort,
		chain:       chain,
		stratumPort: stratumPort,
		socks5Proxy: socks5Proxy,
	}
}

// StratumAddr returns the address XMRig should use to connect to this p2pool instance.
func (p *P2Pool) StratumAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", p.stratumPort)
}

// Start launches p2pool and waits until it signals readiness (up to timeout).
func (p *P2Pool) Start(timeout time.Duration) error {
	p.mu.Lock()
	if p.cmd != nil {
		p.mu.Unlock()
		return nil
	}

	bin, err := p.resolve()
	if err != nil {
		p.mu.Unlock()
		return err
	}

	args := []string{
		"--host", p.nodeHost,
		"--rpc-port", fmt.Sprintf("%d", p.rpcPort),
		"--zmq-port", fmt.Sprintf("%d", p.zmqPort),
		"--wallet", p.wallet,
		"--stratum", fmt.Sprintf("0.0.0.0:%d", p.stratumPort),
		"--p2p", "0.0.0.0:37889",
		"--no-color",
	}
	if p.socks5Proxy != "" {
		args = append(args, "--socks5", p.socks5Proxy)
	}
	if p.chain == "mini" {
		args = append(args, "--mini")
	}
	if log.Writer() == io.Discard {
		args = append(args, "--loglevel", "0")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()

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
	go p.readOutput(stdout, readyCh)
	p.mu.Unlock() // release before blocking — readOutput needs the lock to set p.ready

	select {
	case <-readyCh:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("p2pool did not become ready within %s; check node connectivity", timeout)
	}
}

// Stop terminates the p2pool subprocess.
func (p *P2Pool) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd == nil {
		return
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

// readOutput scans p2pool stdout and signals readyCh when the sidechain is ready.
func (p *P2Pool) readOutput(r io.Reader, readyCh chan<- struct{}) {
	scanner := bufio.NewScanner(r)
	signalled := false
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[p2pool] %s", line)
		if !signalled && isP2PoolReady(line) {
			p.mu.Lock()
			p.ready = true
			p.mu.Unlock()
			readyCh <- struct{}{}
			signalled = true
		}
	}
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
