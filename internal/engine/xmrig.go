// Package engine manages the XMRig, p2pool, and monerod subprocesses.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// XMRig manages a single XMRig subprocess.
type XMRig struct {
	binPath  string
	poolURL  string // stratum URL: "127.0.0.1:3333" (p2pool) or "pool.host:port"
	wallet   string // only used for direct pool mode; p2pool manages the wallet itself
	threads  int
	apiPort  int

	mu       sync.Mutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	hashrate float64
	paused   bool
}

// xmrigSummary is the subset of XMRig's /1/summary we care about.
type xmrigSummary struct {
	Hashrate struct {
		Total []*float64 `json:"total"` // [10s, 1min, 15min]; pointers handle JSON null
	} `json:"hashrate"`
}

var hashrateLineRe = regexp.MustCompile(`(?i)(?:10s/60s/15m\s+)?(\d+(?:\.\d+)?)\s+[kKmMgG]?H/s`)

// NewXMRig creates a manager. binPath may be empty to auto-locate.
func NewXMRig(binPath, poolURL, wallet string, threads, apiPort int) *XMRig {
	return &XMRig{
		binPath: binPath,
		poolURL: poolURL,
		wallet:  wallet,
		threads: threads,
		apiPort: apiPort,
	}
}

// Start launches XMRig. Calling Start on an already-running instance is a no-op.
func (x *XMRig) Start() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd != nil {
		return nil
	}
	return x.start()
}

// start is the unlocked implementation.
func (x *XMRig) start() error {
	bin, err := x.resolve()
	if err != nil {
		return err
	}

	args := x.buildArgs()
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
		return fmt.Errorf("starting xmrig: %w", err)
	}

	if err := setPriority(cmd.Process); err != nil {
		log.Printf("warn: could not set xmrig priority: %v", err)
	}

	x.cmd = cmd
	x.cancel = cancel
	x.paused = false

	go x.readOutput(stdout)
	go x.pollHashrateAPI()
	return nil
}

func (x *XMRig) buildArgs() []string {
	args := []string{
		"--url", x.poolURL,
		"--cpu-priority", "0",
		"--no-color",
		"--http-host", "127.0.0.1",
		"--http-port", strconv.Itoa(x.apiPort),
	}
	if x.threads > 0 {
		args = append(args, "--threads", strconv.Itoa(x.threads))
	}
	// In pool mode, XMRig needs a worker identifier; use the wallet address.
	// In p2pool mode the wallet is configured in p2pool itself.
	if x.wallet != "" && !strings.HasPrefix(x.poolURL, "127.0.0.1") {
		args = append(args, "--user", x.wallet)
	}
	return args
}

// Stop gracefully terminates XMRig.
func (x *XMRig) Stop() {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.stop()
}

func (x *XMRig) stop() {
	if x.cmd == nil {
		return
	}
	x.cancel()
	_ = x.cmd.Wait()
	x.cmd = nil
	x.cancel = nil
	x.paused = false
}

// Pause suspends the XMRig process via OS-level signals/APIs.
func (x *XMRig) Pause() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd == nil || x.paused {
		return nil
	}
	if err := suspendProcess(x.cmd.Process); err != nil {
		return err
	}
	x.paused = true
	return nil
}

// Resume unsuspends the XMRig process.
func (x *XMRig) Resume() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd == nil || !x.paused {
		return nil
	}
	if err := resumeProcess(x.cmd.Process); err != nil {
		return err
	}
	x.paused = false
	return nil
}

// SetThreads restarts XMRig with the new thread count.
// It is a no-op if threads hasn't changed.
func (x *XMRig) SetThreads(n int) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.threads == n {
		return nil
	}
	x.threads = n
	if x.cmd == nil {
		return nil
	}
	x.stop()
	return x.start()
}

// Hashrate returns the most recent 10-second average hashrate in H/s.
func (x *XMRig) Hashrate() float64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.hashrate
}

// PID returns the process ID of the running XMRig, or 0 if not running.
func (x *XMRig) PID() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd == nil || x.cmd.Process == nil {
		return 0
	}
	return x.cmd.Process.Pid
}

// IsRunning reports whether XMRig is currently active (not stopped, not paused).
func (x *XMRig) IsRunning() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.cmd != nil && !x.paused
}

// IsPaused reports whether XMRig is suspended.
func (x *XMRig) IsPaused() bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.paused
}

// readOutput tails XMRig stdout and falls back to parsing hashrate lines when
// the HTTP API is unavailable (e.g., startup race).
func (x *XMRig) readOutput(r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		if m := hashrateLineRe.FindStringSubmatch(line); m != nil {
			if f, err := strconv.ParseFloat(m[1], 64); err == nil {
				x.mu.Lock()
				if x.hashrate == 0 {
					x.hashrate = f
				}
				x.mu.Unlock()
			}
		}
	}
}

// pollHashrateAPI queries XMRig's HTTP API every 15 seconds.
func (x *XMRig) pollHashrateAPI() {
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		x.mu.Lock()
		port := x.apiPort
		running := x.cmd != nil
		x.mu.Unlock()
		if !running {
			return
		}
		url := fmt.Sprintf("http://127.0.0.1:%d/1/summary", port)
		resp, err := client.Get(url)
		if err != nil {
			continue
		}
		var s xmrigSummary
		if err := json.NewDecoder(resp.Body).Decode(&s); err == nil {
			if len(s.Hashrate.Total) > 0 && s.Hashrate.Total[0] != nil {
				x.mu.Lock()
				x.hashrate = *s.Hashrate.Total[0]
				x.mu.Unlock()
			}
		}
		resp.Body.Close()
	}
}

// resolve finds the XMRig binary via (in order):
//  1. x.binPath if set
//  2. <dir-of-kind-miner-binary>/bin/xmrig[.exe]
//  3. PATH
func (x *XMRig) resolve() (string, error) {
	if x.binPath != "" {
		if _, err := os.Stat(x.binPath); err == nil {
			return x.binPath, nil
		}
		return "", fmt.Errorf("xmrig not found at configured path %q", x.binPath)
	}

	exe, _ := os.Executable()
	name := "xmrig"
	if runtime.GOOS == "windows" {
		name = "xmrig.exe"
	}
	bundled := filepath.Join(filepath.Dir(exe), "bin", name)
	if _, err := os.Stat(bundled); err == nil {
		return bundled, nil
	}

	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("xmrig binary not found; install it or set xmrig_path in config")
	}
	return path, nil
}
