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
//
// Throttling is done by duty-cycling the running process with SIGSTOP/SIGCONT,
// not by changing its thread count. That choice is the difference between a
// miner that works and one that does not: RandomX spends ~3 seconds allocating
// its dataset on every launch, and the scheduler re-evaluates every couple of
// seconds, so a design that restarted xmrig to throttle it would spend all its
// time re-initialising and never actually hash. Pausing and resuming keeps the
// dataset warm and gives smooth, fine-grained control the thread count cannot.
type XMRig struct {
	binPath string
	poolURL string // stratum URL: "127.0.0.1:3333" (p2pool) or "pool.host:port"
	wallet  string // only used for direct pool mode; p2pool manages the wallet itself
	threads int
	apiPort int

	mu       sync.Mutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	hashrate float64
	paused   bool

	// duty is the fraction of wall-clock time the miner is allowed to run,
	// applied by dutyLoop. 1 runs flat out, 0 keeps it suspended. dutyStop ends
	// the loop when the process stops.
	duty     float64
	dutyStop chan struct{}
}

// dutyPeriod is one full run/suspend cycle. Short enough that a half-duty miner
// frees the machine in sub-second slices rather than visible half-second
// stalls; long enough that the syscall overhead and any stale-share cost stay
// negligible. Pausing the miner never stalls other apps — it hands them the
// cores — so this only shapes the miner's own throughput.
const dutyPeriod = 500 * time.Millisecond

// minRunSlice keeps the smallest "on" slice long enough for the HTTP API to
// answer a poll within it, so a heavily throttled miner still reports a
// hashrate instead of looking dead.
const minRunSlice = 80 * time.Millisecond

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
	x.dutyStop = make(chan struct{})

	go x.readOutput(stdout)
	go x.pollHashrateAPI()
	go x.dutyLoop(x.dutyStop, cmd.Process)
	return nil
}

// SetDuty sets the fraction of time the miner may run, in [0,1]. It takes
// effect within one dutyPeriod without restarting the process. Values outside
// the range are clamped.
func (x *XMRig) SetDuty(d float64) {
	if d < 0 {
		d = 0
	} else if d > 1 {
		d = 1
	}
	x.mu.Lock()
	x.duty = d
	x.mu.Unlock()
}

// Duty returns the current duty fraction.
func (x *XMRig) Duty() float64 {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.duty
}

// dutyLoop drives SIGSTOP/SIGCONT so the process runs for duty of each period.
// It is bound to one process: SetThreads restarts the loop against the new one,
// and a stale loop exits when its stop channel closes.
func (x *XMRig) dutyLoop(stop <-chan struct{}, proc *os.Process) {
	wait := func(d time.Duration) bool {
		if d <= 0 {
			return false
		}
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-stop:
			return true
		case <-t.C:
			return false
		}
	}

	for {
		d := x.Duty()
		switch {
		case d <= 0:
			x.suspend(proc)
			if wait(dutyPeriod) {
				return
			}
		case d >= 1:
			x.unsuspend(proc)
			if wait(dutyPeriod) {
				return
			}
		default:
			on := time.Duration(d * float64(dutyPeriod))
			if on < minRunSlice {
				on = minRunSlice
			}
			off := dutyPeriod - on
			x.unsuspend(proc)
			if wait(on) {
				return
			}
			x.suspend(proc)
			if wait(off) {
				return
			}
		}
	}
}

// suspend/unsuspend act on a specific process so the duty loop never signals a
// PID that Stop has already reaped and the OS may have recycled.
func (x *XMRig) suspend(proc *os.Process) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd == nil || x.cmd.Process != proc || x.paused {
		return
	}
	if err := suspendProcess(proc); err != nil {
		return
	}
	x.paused = true
}

func (x *XMRig) unsuspend(proc *os.Process) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.cmd == nil || x.cmd.Process != proc || !x.paused {
		return
	}
	if err := resumeProcess(proc); err != nil {
		return
	}
	x.paused = false
}

func (x *XMRig) buildArgs() []string {
	args := []string{
		"--url", x.poolURL,
		"--cpu-priority", "0",
		"--no-color",
		// Print a speed line every 10s so a hashrate is available from stdout
		// even if an API poll lands during a duty-cycle suspend.
		"--print-time", "10",
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
	if x.dutyStop != nil {
		close(x.dutyStop)
		x.dutyStop = nil
	}
	x.cancel()
	_ = x.cmd.Wait()
	x.cmd = nil
	x.cancel = nil
	x.paused = false
}

// SetThreads changes the thread count XMRig runs with. Because xmrig cannot be
// re-threaded live, this restarts the process — so it is reserved for a config
// change (the user editing max_threads), never used for moment-to-moment
// throttling, which SetDuty handles without a restart. It is a no-op if the
// count is unchanged.
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

// pollHashrateAPI queries XMRig's HTTP API every 5 seconds. The timeout
// comfortably exceeds one dutyPeriod, so a poll that lands during a duty-cycle
// suspend simply completes when the process next resumes rather than failing.
func (x *XMRig) pollHashrateAPI() {
	client := &http.Client{Timeout: 3 * time.Second}
	ticker := time.NewTicker(5 * time.Second)
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
