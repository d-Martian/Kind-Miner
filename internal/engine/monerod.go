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

// Monerod manages an optional local monerod subprocess (Mode A only).
// Most users will not use this; they either run monerod themselves or use
// a remote node (Mode B).
type Monerod struct {
	binPath string

	mu     sync.Mutex
	cmd    *exec.Cmd
	cancel context.CancelFunc
	synced bool
}

// NewMonerod creates a manager. binPath may be empty to auto-locate.
func NewMonerod(binPath string) *Monerod {
	return &Monerod{binPath: binPath}
}

// Start launches monerod and waits until it reports being synchronized (up to timeout).
// This can take minutes on first run and days for initial blockchain sync.
func (m *Monerod) Start(timeout time.Duration) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd != nil {
		return nil
	}

	bin, err := m.resolve()
	if err != nil {
		return err
	}

	args := []string{
		"--non-interactive",
		"--restricted-rpc",
		"--no-igd",
		"--detach=0",
		"--log-level", "0",
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		return fmt.Errorf("starting monerod: %w", err)
	}

	m.cmd = cmd
	m.cancel = cancel
	m.synced = false

	syncedCh := make(chan struct{}, 1)
	go m.readOutput(stdout, syncedCh)

	select {
	case <-syncedCh:
		return nil
	case <-time.After(timeout):
		// Timeout is not fatal — monerod may still be syncing.
		// p2pool will connect and wait for it.
		log.Printf("warn: monerod did not report full sync within %s; continuing anyway", timeout)
		return nil
	}
}

// Stop terminates monerod.
func (m *Monerod) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cmd == nil {
		return
	}
	m.cancel()
	_ = m.cmd.Wait()
	m.cmd = nil
	m.cancel = nil
	m.synced = false
}

// Synced reports whether monerod has finished initial blockchain sync.
func (m *Monerod) Synced() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.synced
}

func (m *Monerod) readOutput(r io.Reader, syncedCh chan<- struct{}) {
	scanner := bufio.NewScanner(r)
	signalled := false
	for scanner.Scan() {
		line := scanner.Text()
		log.Printf("[monerod] %s", line)
		if !signalled && isMonerodSynced(line) {
			m.mu.Lock()
			m.synced = true
			m.mu.Unlock()
			syncedCh <- struct{}{}
			signalled = true
		}
	}
}

func isMonerodSynced(line string) bool {
	return strings.Contains(line, "SYNCHRONIZED OK") ||
		strings.Contains(line, "Synced") && strings.Contains(line, "100%")
}

func (m *Monerod) resolve() (string, error) {
	if m.binPath != "" {
		if _, err := os.Stat(m.binPath); err == nil {
			return m.binPath, nil
		}
		return "", fmt.Errorf("monerod not found at configured path %q", m.binPath)
	}

	exe, _ := os.Executable()
	name := "monerod"
	if runtime.GOOS == "windows" {
		name = "monerod.exe"
	}
	bundled := filepath.Join(filepath.Dir(exe), "bin", name)
	if _, err := os.Stat(bundled); err == nil {
		return bundled, nil
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("monerod binary not found; install it or set monerod_path in config")
	}
	return path, nil
}
