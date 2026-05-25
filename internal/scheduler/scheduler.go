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

// Scheduler orchestrates the XMRig engine based on system conditions.
type Scheduler struct {
	cfg     *config.Config
	xmrig   *engine.XMRig
	cpu     *monitor.CPU
	battery *monitor.Battery
	temp    *monitor.Temp

	maxThreads int
	profile    config.ThresholdProfile

	mu        sync.Mutex
	state     State
	pausedBy  string
	stopCh    chan struct{}

	Events chan StateChange
}

// New creates a Scheduler. It does not start the mining loop.
func New(cfg *config.Config, xmrig *engine.XMRig) *Scheduler {
	maxThreads := cfg.MaxThreads
	if maxThreads <= 0 {
		maxThreads = int(math.Max(1, float64(runtime.NumCPU())/2))
	}
	profile := config.SensitivityProfiles[cfg.ThrottleSensitivity]
	return &Scheduler{
		cfg:        cfg,
		xmrig:      xmrig,
		cpu:        monitor.NewCPU(),
		battery:    monitor.NewBattery(),
		temp:       monitor.NewTemp(),
		maxThreads: maxThreads,
		profile:    profile,
		state:      StatePaused,
		stopCh:     make(chan struct{}),
		Events:     make(chan StateChange, 16),
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

// ManualPause suspends mining until ManualResume is called.
func (s *Scheduler) ManualPause() {
	s.applyState(StatePaused, "manual pause")
}

// ManualResume allows the scheduler to mine again after a manual pause.
func (s *Scheduler) ManualResume() {
	s.mu.Lock()
	if s.pausedBy == "manual pause" {
		s.pausedBy = ""
	}
	s.mu.Unlock()
	// The next tick will re-evaluate and start mining if conditions allow.
}

// IsManuallyPaused reports whether the user has manually paused mining.
func (s *Scheduler) IsManuallyPaused() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pausedBy == "manual pause"
}

func (s *Scheduler) tick() {
	if s.IsManuallyPaused() {
		s.applyState(StatePaused, "manual pause")
		return
	}

	// Battery check — hard pause, user chose to protect battery life.
	if s.cfg.PauseOnBattery {
		onBat, err := s.battery.OnBattery()
		if err != nil {
			log.Printf("battery check error: %v", err)
		} else if onBat {
			s.applyState(StatePaused, "running on battery")
			return
		}
	}

	// Temperature guard — always enforced regardless of sensitivity setting.
	if s.cfg.TempLimitCelsius > 0 {
		t, err := s.temp.MaxCPU()
		if err != nil {
			log.Printf("temp check error: %v", err)
		} else if t > 0 && t >= s.cfg.TempLimitCelsius {
			s.applyState(StatePaused, fmt.Sprintf("CPU temp %.0f°C ≥ limit %.0f°C", t, s.cfg.TempLimitCelsius))
			return
		}
	}

	// CPU load — drive the throttle tiers.
	usage, err := s.cpu.OtherUsage(s.xmrig.PID())
	if err != nil {
		log.Printf("cpu usage error: %v", err)
		return
	}

	switch {
	case usage >= s.profile.PauseAt:
		s.applyState(StatePaused, fmt.Sprintf("system CPU %.0f%%", usage*100))
	case usage >= s.profile.ReduceAt:
		s.applyState(StateReduced, fmt.Sprintf("system CPU %.0f%%", usage*100))
	default:
		s.applyState(StateFull, "")
	}
}

func (s *Scheduler) applyState(desired State, reason string) {
	s.mu.Lock()
	prev := s.state
	s.state = desired
	s.pausedBy = reason
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
		half := int(math.Max(1, float64(s.maxThreads)/2))
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
		if err := s.xmrig.SetThreads(s.maxThreads); err != nil {
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
