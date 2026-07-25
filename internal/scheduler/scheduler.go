// Package scheduler drives the mining state machine.
// Every tick it samples CPU load, battery, and temperature, then tells the
// engine to run at full speed, reduced speed, or pause entirely.
package scheduler

import (
	"fmt"
	"log"
	"math"
	"runtime"
	"sync"
	"time"

	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/monitor"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// State represents the current mining activity level.
type State int

const (
	StateFull    State = iota // mining at max configured threads
	StateReduced              // mining at half threads
	StateMinimal              // mining at 1 thread
	StatePaused               // XMRig suspended
)

func (s State) String() string {
	switch s {
	case StateFull:
		return "mining"
	case StateReduced:
		return "throttled"
	case StateMinimal:
		return "minimal"
	case StatePaused:
		return "paused"
	}
	return "unknown"
}

// StateChange is sent on the Events channel whenever the mining state changes.
type StateChange struct {
	State  State
	Reason string
}

// Override is a user instruction that takes precedence over the idle gate.
type Override int

const (
	// OverrideNone follows the normal policy: mine gently while the machine is
	// in use, go to full speed once the user has been idle long enough.
	OverrideNone Override = iota
	// OverridePause stops mining until the user resumes it.
	OverridePause
	// OverrideMine skips the wait for idle. It does NOT disable the CPU, battery,
	// or temperature backoff — mining still yields to whatever else is running,
	// so turning it on while browsing or watching video stays unobtrusive.
	OverrideMine
)

// ReasonWaitingForIdle is the state reason set while mining is held below full
// speed only because the user is still active. The GUI matches on it to show a
// countdown instead.
const ReasonWaitingForIdle = "waiting for you to go idle"

// Scheduler orchestrates the XMRig engine based on system conditions.
type Scheduler struct {
	cfg     *config.Config
	xmrig   miner
	cpu     *monitor.CPU
	battery *monitor.Battery
	temp    *monitor.Temp
	idle    idleSource
	history *stats.Ring

	// Live-tunable settings, guarded by mu. They are seeded from the config in
	// New and can be changed at runtime via UpdateRuntime (e.g. from the GUI
	// settings window) without restarting the miner.
	maxThreads     int
	profile        config.ThresholdProfile
	pauseOnBattery bool
	tempLimit      float64
	idleFullAfter  time.Duration

	mu       sync.Mutex
	state    State
	pausedBy string
	override Override
	stopCh   chan struct{}

	Events chan StateChange
}

// idleSource reports how long the user has been away from the keyboard. It is
// an interface so tests can drive the idle gate without a real desktop session.
type idleSource interface {
	IdleTime() (time.Duration, bool)
}

// miner is the slice of the XMRig engine the scheduler drives. Depending on the
// behaviour rather than the concrete type lets the state machine be tested
// without launching a mining subprocess.
type miner interface {
	Pause() error
	Resume() error
	SetThreads(int) error
	Stop()
	PID() int
}

// hashrateReporter is implemented by engines that can report a hashrate. It is
// separate from miner so a test stub does not have to provide one.
type hashrateReporter interface {
	Hashrate() float64
}

// historyCapacity is how many samples the rolling history keeps: one hour at
// the 5-second tick. Enough to show the miner reacting to a burst of work
// without holding data nobody will look at.
const historyCapacity = 720

// New creates a Scheduler. It does not start the mining loop.
func New(cfg *config.Config, xmrig *engine.XMRig) *Scheduler {
	maxThreads := cfg.MaxThreads
	if maxThreads <= 0 {
		maxThreads = int(math.Max(1, float64(runtime.NumCPU())/2))
	}
	profile := config.SensitivityProfiles[cfg.ThrottleSensitivity]
	return &Scheduler{
		cfg:            cfg,
		xmrig:          xmrig,
		cpu:            monitor.NewCPU(),
		battery:        monitor.NewBattery(),
		temp:           monitor.NewTemp(),
		idle:           monitor.NewIdle(),
		history:        stats.NewRing(historyCapacity),
		maxThreads:     maxThreads,
		profile:        profile,
		pauseOnBattery: cfg.PauseOnBattery,
		tempLimit:      cfg.TempLimitCelsius,
		idleFullAfter:  time.Duration(cfg.IdleFullAfterSeconds) * time.Second,
		state:          StatePaused,
		stopCh:         make(chan struct{}),
		Events:         make(chan StateChange, 16),
	}
}

// Start begins the polling loop. It blocks until Stop is called.
func (s *Scheduler) Start() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	// First tick immediately.
	s.tick()

	for {
		select {
		case <-ticker.C:
			s.tick()
		case <-s.stopCh:
			s.xmrig.Stop()
			return
		}
	}
}

// Stop halts the scheduler and shuts down XMRig.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// CurrentState returns the current mining state and the last reason it changed.
func (s *Scheduler) CurrentState() (State, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.pausedBy
}

// SetOverride records a user instruction. The two overrides are mutually
// exclusive, so setting one clears the other.
func (s *Scheduler) SetOverride(o Override) {
	s.mu.Lock()
	s.override = o
	s.mu.Unlock()
	if o == OverridePause {
		s.applyState(StatePaused, "manual pause")
		return
	}
	// The next tick re-evaluates and starts mining if conditions allow.
}

// Override returns the current user override.
func (s *Scheduler) Override() Override {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.override
}

// ManualPause suspends mining until ManualResume is called.
func (s *Scheduler) ManualPause() { s.SetOverride(OverridePause) }

// ManualResume allows the scheduler to mine again after a manual pause.
func (s *Scheduler) ManualResume() {
	if s.Override() == OverridePause {
		s.SetOverride(OverrideNone)
	}
}

// IsManuallyPaused reports whether the user has manually paused mining.
func (s *Scheduler) IsManuallyPaused() bool { return s.Override() == OverridePause }

// UpdateRuntime applies live-tunable settings from cfg — throttle sensitivity,
// max threads, pause-on-battery, and the temperature limit — taking effect on
// the next tick without restarting the miner. Changes that require relaunching
// a subprocess (wallet, mode, node) are not handled here.
func (s *Scheduler) UpdateRuntime(cfg *config.Config) {
	maxThreads := cfg.MaxThreads
	if maxThreads <= 0 {
		maxThreads = int(math.Max(1, float64(runtime.NumCPU())/2))
	}
	s.mu.Lock()
	s.maxThreads = maxThreads
	s.profile = config.SensitivityProfiles[cfg.ThrottleSensitivity]
	s.pauseOnBattery = cfg.PauseOnBattery
	s.tempLimit = cfg.TempLimitCelsius
	s.idleFullAfter = time.Duration(cfg.IdleFullAfterSeconds) * time.Second
	s.mu.Unlock()
}

// IdleCountdown returns the whole seconds remaining before mining is allowed to
// go to full speed. ok is false when the countdown does not apply: the gate is
// switched off, the user overrode it, the machine has no idle detection, or the
// user is already idle.
func (s *Scheduler) IdleCountdown() (int, bool) {
	s.mu.Lock()
	after := s.idleFullAfter
	override := s.override
	s.mu.Unlock()

	if after <= 0 || override != OverrideNone {
		return 0, false
	}
	idleFor, ok := s.idle.IdleTime()
	if !ok || idleFor >= after {
		return 0, false
	}
	remaining := after - idleFor
	// Round up: showing "0s" for anything under a second reads as stuck.
	return int((remaining + time.Second - 1) / time.Second), true
}

func (s *Scheduler) tick() {
	if s.Override() == OverridePause {
		s.applyState(StatePaused, "manual pause")
		return
	}

	// Snapshot live-tunable settings under the lock so a concurrent
	// UpdateRuntime (from the GUI) can't race with this tick.
	s.mu.Lock()
	pauseOnBattery := s.pauseOnBattery
	tempLimit := s.tempLimit
	profile := s.profile
	s.mu.Unlock()

	// Battery check — hard pause, user chose to protect battery life.
	if pauseOnBattery {
		onBat, err := s.battery.OnBattery()
		if err != nil {
			log.Printf("battery check error: %v", err)
		} else if onBat {
			s.applyState(StatePaused, "running on battery")
			return
		}
	}

	// Temperature guard — always enforced regardless of sensitivity setting.
	if tempLimit > 0 {
		t, err := s.temp.MaxCPU()
		if err != nil {
			log.Printf("temp check error: %v", err)
		} else if t > 0 && t >= tempLimit {
			s.applyState(StatePaused, fmt.Sprintf("CPU temp %.0f°C ≥ limit %.0f°C", t, tempLimit))
			return
		}
	}

	// CPU load — drive the throttle tiers.
	usage, minerUsage, err := s.cpu.Usage(s.xmrig.PID())
	if err != nil {
		log.Printf("cpu usage error: %v", err)
		return
	}
	s.record(usage, minerUsage)

	switch {
	case usage >= profile.PauseAt:
		s.applyState(StatePaused, fmt.Sprintf("system CPU %.0f%%", usage*100))
	case usage >= profile.ReduceAt:
		s.applyState(StateReduced, fmt.Sprintf("system CPU %.0f%%", usage*100))
	default:
		// The CPU is quiet, but that alone doesn't mean nobody is here: reading
		// a page or watching a video barely registers. Hold at reduced speed
		// until the user has actually been away, so a machine in use never has
		// every core taken by mining.
		if state, reason, gated := s.idleGate(); gated {
			s.applyState(state, reason)
			return
		}
		s.applyState(StateFull, "")
	}
}

// record appends one observation to the rolling history. The hashrate comes
// from the engine's own reading, so the three series in a Sample are all taken
// at the same moment.
func (s *Scheduler) record(otherCPU, minerCPU float64) {
	if s.history == nil {
		return
	}
	var hr float64
	if h, ok := s.xmrig.(hashrateReporter); ok {
		hr = h.Hashrate()
	}
	s.history.Add(stats.Sample{
		At:       time.Now(),
		MinerCPU: minerCPU,
		OtherCPU: otherCPU,
		Hashrate: hr,
	})
}

// History returns the rolling history of CPU and hashrate samples, oldest
// first. Intended for a chart showing the miner yielding to other work.
func (s *Scheduler) History() []stats.Sample {
	if s.history == nil {
		return nil
	}
	return s.history.Snapshot()
}

// idleGate reports whether full-speed mining should be held back because the
// user is still active. gated is false when the gate does not apply, in which
// case the caller proceeds to full speed.
func (s *Scheduler) idleGate() (State, string, bool) {
	s.mu.Lock()
	after := s.idleFullAfter
	override := s.override
	s.mu.Unlock()

	if after <= 0 || override == OverrideMine {
		return 0, "", false
	}
	idleFor, ok := s.idle.IdleTime()
	if !ok {
		// No idle detection on this system. Falling through to full speed keeps
		// the previous behaviour rather than throttling forever.
		return 0, "", false
	}
	if idleFor >= after {
		return 0, "", false
	}
	return StateReduced, ReasonWaitingForIdle, true
}

func (s *Scheduler) applyState(desired State, reason string) {
	s.mu.Lock()
	prev := s.state
	s.state = desired
	s.pausedBy = reason
	maxThreads := s.maxThreads
	s.mu.Unlock()

	switch desired {
	case StatePaused:
		if err := s.xmrig.Pause(); err != nil {
			log.Printf("pause error: %v", err)
		}
	case StateMinimal:
		if err := s.xmrig.Resume(); err != nil {
			log.Printf("resume error: %v", err)
		}
		if err := s.xmrig.SetThreads(1); err != nil {
			log.Printf("set threads error: %v", err)
		}
	case StateReduced:
		half := int(math.Max(1, float64(maxThreads)/2))
		if err := s.xmrig.Resume(); err != nil {
			log.Printf("resume error: %v", err)
		}
		if err := s.xmrig.SetThreads(half); err != nil {
			log.Printf("set threads error: %v", err)
		}
	case StateFull:
		if err := s.xmrig.Resume(); err != nil {
			log.Printf("resume error: %v", err)
		}
		if err := s.xmrig.SetThreads(maxThreads); err != nil {
			log.Printf("set threads error: %v", err)
		}
	}

	if desired != prev {
		select {
		case s.Events <- StateChange{State: desired, Reason: reason}:
		default:
		}
	}
}
