// Package engine manages the XMRig, p2pool, and monerod subprocesses.
package engine

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
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
	// poolURL is the Stratum address: p2pool on loopback, or a household hub
	// on the LAN. The wallet is p2pool's business, so xmrig never sees it.
	poolURL string
	// poolUser and poolFingerprint are set for a hub; see UseHub.
	poolUser        string
	poolFingerprint string
	threads         int
	apiPort         int
	// configPath, when set, is the JSON config xmrig runs from and watches.
	// Rewriting it re-threads the running miner — see SetLayout. Empty runs
	// from command-line flags, as before config files were used.
	configPath string
	// layout is the list of CPUs xmrig runs one thread on each of. Empty lets
	// xmrig lay out `threads` threads itself.
	layout []int
	// randomxMode is passed to --randomx-mode when set: "fast" keeps the ~2 GB
	// dataset, "light" a 256 MB cache at a fraction of the hashrate.
	randomxMode string

	mu       sync.Mutex
	cmd      *exec.Cmd
	cancel   context.CancelFunc
	hashrate float64
	paused   bool
	// closed is set by Close: the app is shutting down, and a Start from the
	// scheduler's memory restart must not bring the miner back after it.
	closed bool

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
func NewXMRig(binPath, poolURL string, threads, apiPort int) *XMRig {
	return &XMRig{
		binPath: binPath,
		poolURL: poolURL,
		threads: threads,
		apiPort: apiPort,
	}
}

// Start launches XMRig. Calling Start on an already-running instance is a no-op.
func (x *XMRig) Start() error {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed {
		return errXMRigClosed
	}
	if x.cmd != nil {
		return nil
	}
	return x.start()
}

var errXMRigClosed = errors.New("xmrig has been shut down")

// Close stops the miner for good. Stop alone is not enough at shutdown: the
// scheduler stops the miner when memory runs short and starts it again later,
// and a restart racing the shutdown would leave a miner running with the app
// gone — the failure Stop's own comment records.
func (x *XMRig) Close() {
	x.mu.Lock()
	x.closed = true
	x.stop()
	x.mu.Unlock()
}

// start is the unlocked implementation.
func (x *XMRig) start() error {
	bin, err := x.resolve()
	if err != nil {
		return err
	}

	if x.configPath != "" {
		if err := x.writeConfig(); err != nil {
			return fmt.Errorf("writing xmrig config: %w", err)
		}
	}
	name, args, scoped := launchCommand(bin, x.buildArgs())
	cmd, stdout, cancel, err := launchProcess(name, args)
	if err != nil {
		return err
	}
	// systemd-run execs the miner only once the user manager has answered for
	// the scope. The duty loop's first act is often SIGSTOP (the miner starts
	// at duty 0), and stopping systemd-run mid-request left it frozen as
	// itself — never becoming the miner, and failing outright if the stop
	// outlasted D-Bus's timeout. So nothing touches the process until it is
	// the miner; a scope that cannot be made costs a direct launch, not mining.
	if scoped {
		if err := awaitExec(cmd.Process.Pid, bin, scopeExecTimeout); err != nil {
			log.Printf("xmrig: could not start in an idle scope (%v); starting it directly", err)
			cancel()
			_ = cmd.Wait()
			scoped = false
			if cmd, stdout, cancel, err = launchProcess(bin, x.buildArgs()); err != nil {
				return err
			}
		}
	}

	if err := setPriority(cmd.Process); err != nil {
		log.Printf("warn: could not set xmrig priority: %v", err)
	}
	demote(cmd.Process.Pid, !scoped)
	if how := describeLowPriority(scoped); how != "" {
		log.Printf("xmrig: %s", how)
	}

	x.cmd = cmd
	x.cancel = cancel
	x.paused = false
	x.dutyStop = make(chan struct{})

	go x.readOutput(stdout)
	go x.pollHashrateAPI()
	go x.dutyLoop(x.dutyStop, cmd.Process)
	go settle(cmd.Process.Pid, scoped, x.dutyStop)
	return nil
}

// scopeExecTimeout bounds the wait for systemd-run to become the miner. It
// normally takes tens of milliseconds; a user manager that has not answered
// in seconds is not going to.
const scopeExecTimeout = 10 * time.Second

// launchProcess starts name with its output piped for readOutput.
func launchProcess(name string, args []string) (*exec.Cmd, io.Reader, context.CancelFunc, error) {
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, nil, nil, err
	}
	cmd.Stderr = cmd.Stdout

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, nil, nil, fmt.Errorf("starting xmrig: %w", err)
	}
	return cmd, stdout, cancel, nil
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
// It is bound to one process: a restart starts a new loop against the new one,
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
	if x.configPath != "" {
		return []string{"--config=" + x.configPath}
	}
	args := []string{
		"--url", x.poolURL,
	}
	if x.poolUser != "" {
		args = append(args, "--user", x.poolUser)
	}
	if x.poolFingerprint != "" {
		args = append(args, "--tls", "--tls-fingerprint", x.poolFingerprint)
	}
	args = append(args,
		"--cpu-priority", "0",
		"--no-color",
		// Print a speed line every 10s so a hashrate is available from stdout
		// even if an API poll lands during a duty-cycle suspend.
		"--print-time", "10",
		"--http-host", "127.0.0.1",
		"--http-port", strconv.Itoa(x.apiPort),
	)
	if x.threads > 0 {
		args = append(args, "--threads", strconv.Itoa(x.threads))
	}
	if x.randomxMode != "" {
		args = append(args, "--randomx-mode", x.randomxMode)
	}
	return args
}

// SetRandomXMode chooses the RandomX mode for the next launch. It does not
// restart a running miner: the mode is fixed for the life of the dataset, and
// switching it live would cost the same re-initialisation SetDuty exists to
// avoid.
func (x *XMRig) SetRandomXMode(mode string) {
	x.mu.Lock()
	x.randomxMode = mode
	x.mu.Unlock()
}

// SetAffinity confines every thread of the running miner to cpus, without a
// restart. It re-applies to all threads on each call, so calling it every
// scheduler tick also catches the workers xmrig spawns once its dataset is
// ready. A nil or empty set is a no-op; so is a platform without per-thread
// affinity.
func (x *XMRig) SetAffinity(cpus []int) error {
	pid := x.PID()
	if pid == 0 || len(cpus) == 0 {
		return nil
	}
	return setAffinity(pid, cpus)
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

// UseHub makes xmrig mine to a household hub over TLS, trusting only the
// certificate with this SHA-256 fingerprint (hex), and logging in as user —
// the device's name in the hub's list. Call it before Start.
//
// xmrig checks the fingerprint itself and refuses the connection on a
// mismatch, so a machine on the LAN answering at the hub's address gets no
// work from this miner.
func (x *XMRig) UseHub(user, fingerprint string) {
	x.mu.Lock()
	x.poolUser, x.poolFingerprint = user, fingerprint
	x.mu.Unlock()
}

// SetUser sets the login xmrig sends: the name p2pool lists it under. It is
// never the wallet, which stays with p2pool. Call it before Start.
func (x *XMRig) SetUser(user string) {
	x.mu.Lock()
	x.poolUser = user
	x.mu.Unlock()
}

// UseConfigFile makes xmrig run from a JSON config at path, written on every
// start and watched by xmrig for changes. Call it before Start.
func (x *XMRig) UseConfigFile(path string) {
	x.mu.Lock()
	x.configPath = path
	x.mu.Unlock()
}

// SetLayout sets the CPUs xmrig runs a thread on, one each.
//
// On a running miner it rewrites the watched config and xmrig re-threads in
// place: measured on 6.26, the old threads stop and the new ones are hashing
// again about 7 ms later, and the RandomX dataset is kept — no re-init, no
// reallocation. (The note this replaces said xmrig could not be re-threaded
// live; that was true of the command-line flags, not of the config watch.)
// It is still not the throttle — the duty cycle is — but it lets the
// scheduler change which cores that duty cycle applies to.
//
// Before Start it only records the layout. It is an error on a miner running
// from flags, which has no file to rewrite.
func (x *XMRig) SetLayout(cpus []int) error {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.layout = append([]int(nil), cpus...)
	if x.cmd == nil {
		return nil
	}
	if x.configPath == "" {
		return errors.New("xmrig is running from flags; its layout cannot change live")
	}
	return x.writeConfig()
}

// xmrigConfig is the subset of xmrig's config.json kind-miner sets. Field
// names are xmrig's; re-check them when the xmrig pin moves.
type xmrigConfig struct {
	Autosave    bool         `json:"autosave"`
	Colors      bool         `json:"colors"`
	DonateLevel int          `json:"donate-level"`
	PrintTime   int          `json:"print-time"`
	Watch       bool         `json:"watch"`
	HTTP        xmrigHTTP    `json:"http"`
	CPU         xmrigCPU     `json:"cpu"`
	RandomX     xmrigRandomX `json:"randomx"`
	Pools       []xmrigPool  `json:"pools"`
}

type xmrigHTTP struct {
	Enabled    bool   `json:"enabled"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	Restricted bool   `json:"restricted"`
}

type xmrigCPU struct {
	Enabled   bool  `json:"enabled"`
	HugePages bool  `json:"huge-pages"`
	Priority  int   `json:"priority"`
	RX        []int `json:"rx,omitempty"`
}

type xmrigRandomX struct {
	Mode       string `json:"mode"`
	OneGBPages bool   `json:"1gb-pages"`
	RdMSR      bool   `json:"rdmsr"`
	WrMSR      bool   `json:"wrmsr"`
}

type xmrigPool struct {
	URL            string `json:"url"`
	User           string `json:"user,omitempty"`
	TLS            bool   `json:"tls,omitempty"`
	TLSFingerprint string `json:"tls-fingerprint,omitempty"`
	Keepalive      bool   `json:"keepalive"`
}

// configJSON renders the config for the current settings. Caller holds mu.
//
// It says what the command-line flags said, plus two things they could not:
// the thread layout, and that xmrig must not touch model-specific registers.
// MSR tweaks are system-wide and need root; kind-miner runs unprivileged and
// promises no system-wide tweaks, so they are off rather than merely failing.
func (x *XMRig) configJSON() ([]byte, error) {
	rx := x.layout
	if len(rx) == 0 && x.threads > 0 {
		// A count without a layout: that many threads, placed by xmrig
		// (-1 is xmrig's "no affinity").
		rx = make([]int, x.threads)
		for i := range rx {
			rx[i] = -1
		}
	}
	mode := x.randomxMode
	if mode == "" {
		mode = "auto"
	}
	return json.MarshalIndent(xmrigConfig{
		Colors:    false,
		PrintTime: 10,
		Watch:     true,
		HTTP:      xmrigHTTP{Enabled: true, Host: "127.0.0.1", Port: x.apiPort, Restricted: true},
		CPU:       xmrigCPU{Enabled: true, HugePages: true, Priority: 0, RX: rx},
		RandomX:   xmrigRandomX{Mode: mode},
		Pools: []xmrigPool{{
			URL:            x.poolURL,
			User:           x.poolUser,
			TLS:            x.poolFingerprint != "",
			TLSFingerprint: x.poolFingerprint,
			Keepalive:      true,
		}},
	}, "", "  ")
}

// writeConfig replaces the config file atomically. A rename, not a rewrite in
// place: xmrig reloads on the change, and must never read half a file. (The
// watch was checked to follow a rename.) Caller holds mu.
func (x *XMRig) writeConfig() error {
	data, err := x.configJSON()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(x.configPath), 0o755); err != nil {
		return err
	}
	tmp := x.configPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, x.configPath)
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
