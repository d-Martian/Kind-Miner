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

// Tor manages a Tor client subprocess. kind-miner launches its own Tor when no
// system Tor is reachable, so users never have to install or start the Tor
// service themselves — and we avoid needing root for `systemctl start tor`.
type Tor struct {
	binPath   string
	dataDir   string
	socksPort int

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	ready  bool
}

// NewTor creates a manager. binPath may be empty to auto-locate (bundled, then
// PATH). dataDir is where Tor keeps its cached consensus/descriptors.
func NewTor(binPath, dataDir string, socksPort int) *Tor {
	return &Tor{binPath: binPath, dataDir: dataDir, socksPort: socksPort}
}

// SOCKSAddr returns the SOCKS5 proxy address this Tor exposes.
func (t *Tor) SOCKSAddr() string {
	return fmt.Sprintf("127.0.0.1:%d", t.socksPort)
}

// Start launches Tor and waits until it has bootstrapped (up to timeout).
func (t *Tor) Start(timeout time.Duration) error {
	t.mu.Lock()
	if t.cmd != nil {
		t.mu.Unlock()
		return nil
	}

	bin, err := t.resolve()
	if err != nil {
		t.mu.Unlock()
		return err
	}
	// Tor refuses a group/world-accessible DataDirectory, so create it 0700.
	if err := os.MkdirAll(t.dataDir, 0o700); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("creating tor data dir: %w", err)
	}

	// Write our own torrc and point Tor at it with an empty defaults file, so it
	// ignores the system /etc/tor configuration entirely. Otherwise Tor inherits
	// service-only directives (e.g. a ControlSocket under /run/tor) that fail
	// when it runs as an unprivileged user. A client-only Tor with a localhost
	// SOCKS port is all p2pool needs.
	torrc := filepath.Join(t.dataDir, "torrc")
	emptyDefaults := filepath.Join(t.dataDir, "defaults-torrc")
	conf := fmt.Sprintf(
		"SocksPort 127.0.0.1:%d\nDataDirectory %s\nClientOnly 1\nAvoidDiskWrites 1\nSafeLogging 1\nLog notice stdout\n",
		t.socksPort, t.dataDir,
	)
	if err := os.WriteFile(torrc, []byte(conf), 0o600); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("writing torrc: %w", err)
	}
	if err := os.WriteFile(emptyDefaults, nil, 0o600); err != nil {
		t.mu.Unlock()
		return fmt.Errorf("writing tor defaults: %w", err)
	}

	args := []string{"--defaults-torrc", emptyDefaults, "-f", torrc}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = torEnv(bin)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		t.mu.Unlock()
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		t.mu.Unlock()
		return fmt.Errorf("starting tor: %w", err)
	}

	t.cmd = cmd
	t.cancel = cancel
	t.ready = false

	readyCh := make(chan struct{}, 1)
	go t.readOutput(stdout, readyCh)
	t.mu.Unlock() // release before blocking — readOutput needs the lock to set t.ready

	select {
	case <-readyCh:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("tor did not bootstrap within %s", timeout)
	}
}

// Stop terminates the Tor subprocess.
func (t *Tor) Stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.cmd == nil {
		return
	}
	t.cancel()
	_ = t.cmd.Wait()
	t.cmd = nil
	t.cancel = nil
	t.ready = false
}

// Ready reports whether Tor has finished bootstrapping.
func (t *Tor) Ready() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready
}

// readOutput scans Tor stdout and signals readyCh once bootstrapping completes.
func (t *Tor) readOutput(r io.Reader, readyCh chan<- struct{}) {
	scanner := bufio.NewScanner(r)
	signalled := false
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[tor] %s", line)
		if !signalled && strings.Contains(line, "Bootstrapped 100%") {
			t.mu.Lock()
			t.ready = true
			t.mu.Unlock()
			readyCh <- struct{}{}
			signalled = true
		}
	}
}

func (t *Tor) resolve() (string, error) { return FindTor(t.binPath) }

// torEnv returns the child environment for tor, ensuring a bundled tor (the Tor
// Expert Bundle ships its own libevent/libssl/libcrypto with no $ORIGIN rpath)
// loads its sibling libraries rather than the host's incompatible ones.
func torEnv(bin string) []string {
	env := os.Environ()
	var key string
	switch runtime.GOOS {
	case "darwin":
		key = "DYLD_LIBRARY_PATH"
	case "windows":
		return env // Windows loads DLLs next to the exe automatically
	default:
		key = "LD_LIBRARY_PATH"
	}
	dir := filepath.Dir(bin)
	if existing := os.Getenv(key); existing != "" {
		dir = dir + string(os.PathListSeparator) + existing
	}
	return append(env, key+"="+dir)
}

// FindTor locates a tor binary: an explicit configPath, then a `bin/` directory
// next to the kind-miner executable, then PATH. It returns an error if none is
// found (the caller may then download one via autoinstall.EnsureTor).
func FindTor(configPath string) (string, error) {
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			return configPath, nil
		}
		return "", fmt.Errorf("tor not found at configured path %q", configPath)
	}

	name := "tor"
	if runtime.GOOS == "windows" {
		name = "tor.exe"
	}
	if exe, err := os.Executable(); err == nil {
		bundled := filepath.Join(filepath.Dir(exe), "bin", name)
		if _, err := os.Stat(bundled); err == nil {
			return bundled, nil
		}
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("tor binary not found")
	}
	return path, nil
}
