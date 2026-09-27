// Package scheduler drives the mining state machine.
//
// Every tick it samples the machine — CPU used by everything except the miner,
// battery, temperature, thermal throttling, session lock — and decides how much
// CPU the miner is allowed to hold. The allowance is a continuous fraction
// rather than a set of tiers:
//
//	target  = kindness ceiling − CPU used by everything else
//	allowed = allowed + (target − allowed) × (falling ? Fall : Rise)
//
// The miner then runs at whatever thread count fits inside `allowed`. Falling
// fast and rising slowly (see internal/kindness) is what makes the miner
// unnoticeable: work arriving takes the machine back immediately, and the
// miner only creeps back once the machine has actually been quiet for a while.
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
	"github.com/kind-miner/kind-miner/internal/kindness"
	"github.com/kind-miner/kind-miner/internal/monitor"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// State represents the current mining activity level. It is a label for the
// UI — what actually reaches the miner is the thread count and the suspend.
type State int

const (
	StateFull    State = iota // mining at max configured threads
	StateReduced              // mining at some fraction of them
	StateMinimal              // mining on a single thread
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
	// in use, open up once the user has been idle long enough.
	OverrideNone Override = iota
	// OverridePause stops mining until the user resumes it.
	OverridePause
	// OverrideMine skips the wait for idle. It does NOT disable the CPU, battery,
	// or temperature backoff — mining still yields to whatever else is running,
	// so turning it on while browsing or watching video stays unobtrusive.
	OverrideMine
)

// ReasonWaitingForIdle is the state reason set while mining is held below its
// full allowance only because the user is still active. The GUI matches on it
// to show a countdown instead.
const ReasonWaitingForIdle = "waiting for you to go idle"

// ReasonGame and ReasonMemoryShort are the reasons for the two stops that
// come from outside the CPU reading: a game has GameMode on, and the machine is
// short of memory, so the miner has been stopped to give its dataset back.
const (
	ReasonGame        = "a game is running"
	ReasonMemoryShort = "memory is short; the miner has let go of its 2 GB"
)

// ReasonNodeSyncing is the state reason while mining on a Nodo waits for the
// node to finish syncing.
const ReasonNodeSyncing = "Monero node is syncing"

// tickInterval is how often the machine is sampled. Short enough that the
// dashboard chart shows the miner reacting, long enough that the sampling
// itself is not the load.
const tickInterval = 2 * time.Second

// referenceTick is the interval the kindness Fall and Rise rates are expressed
// against. Rates are rescaled to tickInterval so retuning the tick does not
// silently change how kind every preset is.
const referenceTick = 250 * time.Millisecond

// backoffDelta is how far the allowance must drop in one tick to count as
// backing off — the event the dashboard marks so the user can see the miner
// getting out of the way.
const backoffDelta = 0.07

// historyCapacity is how many samples the rolling history keeps: one hour at
// tickInterval. Enough to show the miner reacting to a burst of work without
// holding data nobody will look at.
const historyCapacity = int(time.Hour / tickInterval)

// Scheduler orchestrates the XMRig engine based on system conditions.
type Scheduler struct {
	cfg     *config.Config
	xmrig   miner
	cpu     *monitor.CPU
	battery *monitor.Battery
	temp    *monitor.Temp
	session *monitor.Session
	power   *monitor.Power
	idle    idleSource
	history *stats.Ring
	// ledger is the per-minute record behind the 30-minute and 24-hour rates.
	// It outlives the ring: the supervisor saves it and loads it back on start.
	ledger *stats.Ledger

	// gameMode reports a game holding Feral's GameMode; nil in tests.
	gameMode *monitor.GameMode
	// sampleMemory reads memory pressure; nil in tests that do not exercise
	// it. memGuard turns the readings into stop and restart decisions, and
	// memStopped remembers which side of that the miner is on. Only the tick
	// goroutine touches them.
	sampleMemory func() monitor.MemorySample
	memGuard     memoryGuard
	memStopped   bool

	// layouts are the present and idle thread layouts, when the topology
	// could be read (see layout.go); layoutNow is the one xmrig runs, touched
	// only by the tick goroutine. layoutThreads is its size, under mu, for the
	// duty arithmetic.
	layouts       layouts
	haveLayouts   bool
	layoutNow     []int
	layoutThreads int

	// nodo is the guest-on-a-node control, nil unless mine_on_nodo is set.
	nodo *nodoControl

	// cores is the machine's logical CPU count — the denominator that turns a
	// CPU allowance into a duty fraction. A field rather than a call to runtime
	// so the controller can be tested against a fixed machine size.
	cores int

	// Live-tunable settings, guarded by mu. They are seeded from the config in
	// New and can be changed at runtime via UpdateRuntime (e.g. from the GUI
	// settings window) without restarting the miner.
	maxThreads      int
	preset          kindness.Preset
	pauseOnBattery  bool
	onlyWhenLocked  bool
	tempLimit       float64
	thermalGovernor bool
	idleFullAfter   time.Duration

	mu       sync.Mutex
	state    State
	pausedBy string
	override Override
	// pauseUntil is when a snoozed pause ends; zero while the pause is "until
	// I resume", or when there is no pause at all.
	pauseUntil time.Time
	// onPauseChange is told whenever the pause starts, ends or changes, so the
	// supervisor can keep it across a restart. See SetPauseHook.
	onPauseChange func(paused bool, until time.Time)
	// hold is a fault outside the machine that makes mining pointless — p2pool
	// on an island, say — named for the user. Empty when nothing holds it.
	hold string
	// activeCores is how many cores the miner is currently confined to, or 0
	// when it may use the whole machine. It narrows the miner's full-tilt share
	// so the duty still maps onto the CPU the allowance is expressed in.
	activeCores int
	// allowed is the smoothed CPU allowance, a fraction of total capacity, and
	// duty is what that became after mapping onto the running miner.
	allowed float64
	duty    float64
	stopCh  chan struct{}
	// stopOnce guards stopCh: Shutdown can be reached twice on some exit paths,
	// and closing a closed channel panics.
	stopOnce sync.Once

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
//
// The control surface is a single duty fraction. The scheduler never changes
// the thread count to throttle — that would restart the miner and stall it in
// RandomX init — so SetThreads is not part of this interface; it belongs to the
// rare config-change path in the engine.
type miner interface {
	SetDuty(float64)
	Stop()
	PID() int
}

// affinitySetter is implemented by engines that can be confined to a set of
// cores while running. It is separate from miner for the same reason
// hashrateReporter is.
type affinitySetter interface {
	SetAffinity([]int) error
}

// hashrateReporter is implemented by engines that can report a hashrate. It is
// separate from miner so a test stub does not have to provide one.
type hashrateReporter interface {
	Hashrate() float64
}

// New creates a Scheduler. It does not start the mining loop.
func New(cfg *config.Config, xmrig *engine.XMRig) *Scheduler {
	s := &Scheduler{
		cfg:             cfg,
		xmrig:           xmrig,
		cpu:             monitor.NewCPU(),
		battery:         monitor.NewBattery(),
		temp:            monitor.NewTemp(),
		session:         monitor.NewSession(),
		power:           monitor.NewPower(),
		idle:            monitor.NewIdle(),
		history:         stats.NewRing(historyCapacity),
		ledger:          stats.NewLedger(),
		gameMode:        monitor.NewGameMode(),
		sampleMemory:    monitor.SampleMemory,
		memGuard:        memoryGuard{footprint: monitor.RandomXHugePageBytes(Threads(cfg))},
		cores:           runtime.NumCPU(),
		maxThreads:      Threads(cfg),
		preset:          cfg.Preset(),
		pauseOnBattery:  cfg.PauseOnBattery,
		onlyWhenLocked:  cfg.MineOnlyWhenLocked,
		tempLimit:       tempLimitFor(cfg),
		thermalGovernor: cfg.ThermalGovernor,
		idleFullAfter:   time.Duration(cfg.IdleFullAfterSeconds) * time.Second,
		state:           StatePaused,
		stopCh:          make(chan struct{}),
		Events:          make(chan StateChange, 16),
	}
	if cfg.MineOnNodo {
		s.nodo = newNodoControl()
	}
	if present, idle, ok := Layouts(cfg); ok {
		// The supervisor launches xmrig on the present layout: at start
		// someone is usually there, and the idle gate opens it up later.
		s.layouts, s.haveLayouts = layouts{present: present, idle: idle}, true
		s.layoutNow = present
		s.layoutThreads = len(present)
	}
	return s
}

// randomxScratchpad is the working set RandomX gives each mining thread. A
// thread only hashes at full speed while its scratchpad stays resident in the
// last-level cache.
const randomxScratchpad = 2 << 20

// ThreadCap resolves the configured maximum thread count, where 0 means "as
// much of the machine as RandomX can actually use".
//
// The kindness ceiling and the duty cycle are what hold mining back, so this is
// the ceiling of raw capacity a Full preset can reach. It deliberately reserves
// nothing for the OS — doing that here as well would stop Full from ever
// meaning full.
//
// It is still not the core count, because past a point extra threads subtract
// hashrate. Each one wants 2 MiB of L3, and once the scratchpads no longer fit
// the threads evict each other continuously: every thread gets slower and the
// machine hashes less in total than it would have with fewer. This was a real
// observed configuration, not a hypothetical — a 22-thread part with 24 MiB of
// L3 asked for 44 MiB of scratchpad against 24 MiB of cache, and two of those
// threads sat on low-power cores with no L3 at all. Capping at the cache is
// faster *and* kinder, so there is no trade being made here.
//
// Where the cache size cannot be read this falls back to the core count, which
// is what every platform did before.
func ThreadCap(configured int) int {
	if configured > 0 {
		return configured
	}
	l3, known := monitor.L3CacheBytes()
	return capForCache(runtime.NumCPU(), l3, known)
}

// capForCache is the pure half of ThreadCap: how many threads a machine with
// this many cores and this much last-level cache should mine with. An unknown
// cache size means "no opinion", which leaves the core count standing.
func capForCache(cores int, l3 int64, known bool) int {
	if known {
		if fits := int(l3 / randomxScratchpad); fits < cores {
			cores = fits
		}
	}
	return int(math.Max(1, float64(cores)))
}

// Start begins the polling loop. It blocks until Stop is called.
func (s *Scheduler) Start() {
	ticker := time.NewTicker(tickInterval)
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
//
// It stops the miner here rather than only signalling the tick loop to do it.
// Signalling alone was the bug: Stop returned immediately, Shutdown returned,
// the process exited — and the loop never got scheduled to run its own
// xmrig.Stop(), leaving a full-speed miner running with nothing supervising it
// and no window to close it from. Whether the race was lost depended on when
// the last tick landed, which is why it looked intermittent.
//
// The loop still stops the miner on its way out. XMRig.Stop is idempotent, so
// doing it twice costs nothing and neither path is load-bearing alone.
func (s *Scheduler) Stop() {
	s.stopOnce.Do(func() { close(s.stopCh) })
	s.xmrig.Stop()
}

// CurrentState returns the current mining state and the last reason it changed.
func (s *Scheduler) CurrentState() (State, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.pausedBy
}

// Allowance returns the miner's current smoothed CPU allowance as a fraction of
// the whole machine, and the duty fraction it is currently running at.
func (s *Scheduler) Allowance() (share, duty float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.allowed, s.duty
}

// ActiveThreads estimates how many cores the miner is effectively using —
// its full thread count scaled by the current duty. It is what the dashboard's
// "N of M threads" reports, so the figure tracks the throttling the user sees.
func (s *Scheduler) ActiveThreads() (active, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.layoutThreads > 0 {
		return int(float64(s.layoutThreads)*s.duty + 0.5), s.layoutThreads
	}
	running := s.maxThreads
	if s.activeCores > 0 && s.activeCores < running {
		running = s.activeCores
	}
	return int(float64(running)*s.duty + 0.5), s.maxThreads
}

// Preset returns the kindness preset currently in force.
func (s *Scheduler) Preset() kindness.Preset {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preset
}

// SetKindness switches preset in place, taking effect on the next tick. The
// allowance is not reset, so the change is felt as the miner drifting to its
// new ceiling rather than as a jump.
func (s *Scheduler) SetKindness(l kindness.Level) {
	s.mu.Lock()
	s.preset = kindness.Get(l)
	s.mu.Unlock()
}

// SetOverride records a user instruction. The two overrides are mutually
// exclusive, so setting one clears the other. A pause set this way lasts until
// the user resumes; PauseUntil sets one that ends by itself.
func (s *Scheduler) SetOverride(o Override) {
	s.setOverride(o, time.Time{})
}

// PauseUntil pauses mining until the given time, after which the scheduler
// resumes on its own — the tray's "1 hour" and "until tomorrow". A zero time
// is "until I resume".
func (s *Scheduler) PauseUntil(until time.Time) {
	s.setOverride(OverridePause, until)
}

func (s *Scheduler) setOverride(o Override, until time.Time) {
	if o != OverridePause {
		until = time.Time{}
	}
	s.mu.Lock()
	s.override = o
	s.pauseUntil = until
	hook := s.onPauseChange
	s.mu.Unlock()
	if hook != nil {
		hook(o == OverridePause, until)
	}
	if o == OverridePause {
		s.apply(0, "manual pause")
		return
	}
	// The next tick re-evaluates and starts mining if conditions allow.
}

// PausedUntil reports the user's pause: whether there is one, and when it
// ends. until is zero for a pause that lasts until the user resumes.
func (s *Scheduler) PausedUntil() (until time.Time, paused bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pauseUntil, s.override == OverridePause
}

// SetPauseHook registers fn to hear every change to the user's pause,
// including a snooze running out. It is how the supervisor keeps a pause
// across a restart: kind-miner starts with the computer, and a reboot must
// not quietly undo "until tomorrow".
func (s *Scheduler) SetPauseHook(fn func(paused bool, until time.Time)) {
	s.mu.Lock()
	s.onPauseChange = fn
	s.mu.Unlock()
}

// snoozeOver reports whether a timed pause has run out.
func snoozeOver(until, now time.Time) bool {
	return !until.IsZero() && !now.Before(until)
}

// endExpiredSnooze lifts a timed pause whose time has come. Called each tick,
// so a snooze ends within one tick of its deadline.
func (s *Scheduler) endExpiredSnooze(now time.Time) {
	s.mu.Lock()
	expired := s.override == OverridePause && snoozeOver(s.pauseUntil, now)
	s.mu.Unlock()
	if expired {
		s.setOverride(OverrideNone, time.Time{})
	}
}

// Override returns the current user override.
func (s *Scheduler) Override() Override {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.override
}

// SetHold stops mining for a reason outside this machine, such as p2pool
// mining a sidechain of its own; "" releases it. Unlike the user's pause it is
// lifted by whoever set it, as soon as the fault clears.
func (s *Scheduler) SetHold(reason string) {
	s.mu.Lock()
	s.hold = reason
	s.mu.Unlock()
}

// Held reports the fault holding mining, if any. The GUI uses it to tell a
// problem that needs the user's attention apart from an ordinary stand-down.
func (s *Scheduler) Held() (reason string, held bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hold, s.hold != ""
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

// UpdateRuntime applies live-tunable settings from cfg — kindness, max threads,
// the pause conditions, and the idle wait — taking effect on the next tick
// without restarting the miner. Changes that require relaunching a subprocess
// (wallet, mode, node) are not handled here.
func (s *Scheduler) UpdateRuntime(cfg *config.Config) {
	s.mu.Lock()
	s.maxThreads = Threads(cfg)
	s.preset = cfg.Preset()
	s.pauseOnBattery = cfg.PauseOnBattery
	s.onlyWhenLocked = cfg.MineOnlyWhenLocked
	s.tempLimit = tempLimitFor(cfg)
	s.thermalGovernor = cfg.ThermalGovernor
	s.idleFullAfter = time.Duration(cfg.IdleFullAfterSeconds) * time.Second
	s.mu.Unlock()
}

// IdleCountdown returns the whole seconds remaining before mining is allowed to
// use its full kindness ceiling. ok is false when the countdown does not apply:
// the gate is switched off, the user overrode it, the machine has no idle
// detection, or the user is already idle.
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

// policy is the settings one decision reads. Snapshotting it under the lock
// keeps a concurrent UpdateRuntime from changing the rules mid-tick.
type policy struct {
	hold            string
	preset          kindness.Preset
	pauseOnBattery  bool
	onlyWhenLocked  bool
	tempLimit       float64
	thermalGovernor bool
}

// conditions is what one tick observed about the machine.
type conditions struct {
	// otherCPU is the fraction of total capacity used by everything except the
	// miner.
	otherCPU  float64
	onBattery bool
	// tempC is the hottest CPU sensor, or 0 when none could be read.
	tempC float64
	// locked reports the session being locked; lockKnown is false where that
	// cannot be determined, in which case the restriction cannot be enforced.
	locked    bool
	lockKnown bool
	// idleGated reports the user still being present, which holds the miner to
	// the kindest ceiling rather than the configured one.
	idleGated bool
	// nodeSyncing reports the Monero node this machine serves still catching
	// up. Only set on a Nodo, and only when the node actually said so.
	nodeSyncing bool
	// gameActive reports a game holding GameMode.
	gameActive bool
	// memoryShort reports the memory guard holding the miner stopped.
	memoryShort bool
}

// decide turns a policy and an observation into the CPU allowance the miner may
// aim for, plus the reason when something blocks mining outright, plus whether
// that block is a hard stop.
//
// The hard/soft distinction is what lets the miner survive an ordinary desktop.
// A hard stop — the user paused, unplugged, the CPU hit its limit — zeroes the
// allowance at once, and coming back is the full slow climb. A soft zero, when
// a brief spike of other work leaves no headroom, only pulls the allowance down
// at the fall rate. Without that split, one background task every few seconds
// would reset the allowance to zero before it could ever accumulate, and the
// miner would idle forever on a machine that is 95% free — which is exactly
// what a live run showed.
//
// It is a pure function so the rules can be tested exhaustively without
// monitors, a subprocess, or a clock.
func decide(p policy, c conditions, override Override) (target float64, reason string, hard bool) {
	if override == OverridePause {
		return 0, "manual pause", true
	}
	// The most urgent machine condition: the miner is already stopped, not
	// just suspended, and saying so explains the missing process.
	if c.memoryShort {
		return 0, ReasonMemoryShort, true
	}
	// Ahead of every machine condition: while the hold stands, mining earns
	// nothing whatever the battery or the temperature say, and it is the one
	// reason the user may have to act on.
	if p.hold != "" {
		return 0, p.hold, true
	}
	if p.pauseOnBattery && c.onBattery {
		return 0, "running on battery", true
	}
	// Only enforceable where the lock state is actually observable; on a machine
	// that cannot report it, refusing to mine at all would be a silent failure.
	if p.onlyWhenLocked && c.lockKnown && !c.locked {
		return 0, "screen is unlocked", true
	}
	// A syncing node is verifying every block it downloads and needs all of the
	// machine; mining through it would stretch a sync of days into longer, on
	// the one box whose purpose is the node.
	if c.gameActive {
		return 0, ReasonGame, true
	}
	if c.nodeSyncing {
		return 0, ReasonNodeSyncing, true
	}

	ceiling := p.preset.Ceiling
	// The CPU being quiet does not mean nobody is here — reading a page barely
	// registers. Until the user has actually stepped away, hold to the kindest
	// ceiling so a machine in use never has most of it taken by mining.
	if c.idleGated {
		if ghost := kindness.Get(kindness.Ghost).Ceiling; ghost < ceiling {
			ceiling = ghost
			reason = ReasonWaitingForIdle
		}
	}

	target = ceiling - c.otherCPU
	if target <= 0 {
		// Soft: other work took the machine for a moment. Fall fast toward
		// zero, but do not wipe out an accumulated allowance a transient spike
		// should only dent.
		return 0, fmt.Sprintf("system busy · other apps at %.0f%%", c.otherCPU*100), false
	}

	// Temperature governs by degree, not by switch. This is the crux of being
	// kind: a machine that idles hot (some do — 80°C is normal for a laptop
	// under a charger) must not be all-or-nothing, mining full then slamming to
	// a dead stop. Near the limit the allowance is scaled down smoothly so the
	// miner eases off and holds a temperature, the way a thermostat does. Only
	// at the hard limit itself does it stop, as a last-resort safety.
	if p.tempLimit > 0 && c.tempC > 0 {
		if c.tempC >= p.tempLimit {
			return 0, fmt.Sprintf("cooling down · CPU at %.0f°C", c.tempC), true
		}
		if p.thermalGovernor {
			if factor := thermalFactor(c.tempC, p.tempLimit); factor < 1 {
				target *= factor
				// Say so only once the governor is really biting, so a
				// hot-idling machine that is barely affected does not nag.
				if factor < 0.85 && reason == "" {
					reason = fmt.Sprintf("easing off the heat · CPU at %.0f°C", c.tempC)
				}
			}
		}
	}
	return target, reason, false
}

// thermalBand is how many degrees below the hard limit the governor starts
// easing off. Narrow, so a machine that idles warm still mines freely and only
// backs off in the last stretch before its limit.
const thermalBand = 8

// thermalFactor scales the allowance from 1 (at or below the soft threshold)
// down to 0 (at the hard limit), linearly across thermalBand. It never returns
// a negative factor.
func thermalFactor(tempC, limit float64) float64 {
	soft := limit - thermalBand
	if tempC <= soft {
		return 1
	}
	if tempC >= limit {
		return 0
	}
	return (limit - tempC) / (limit - soft)
}

// approach moves allowed towards target at the preset's rate, using Fall when
// giving CPU back and Rise when taking it.
func approach(allowed, target float64, p kindness.Preset) float64 {
	rate := p.Rise
	if target < allowed {
		rate = p.Fall
	}
	return allowed + (target-allowed)*scaleRate(rate)
}

// scaleRate converts a per-referenceTick approach rate to the actual tick, so
// the presets mean the same thing whatever the sampling interval is.
func scaleRate(r float64) float64 {
	if r <= 0 {
		return 0
	}
	if r >= 1 {
		return 1
	}
	steps := float64(tickInterval) / float64(referenceTick)
	return 1 - math.Pow(1-r, steps)
}

func (s *Scheduler) tick() {
	s.endExpiredSnooze(time.Now())
	s.mu.Lock()
	p := policy{
		hold:            s.hold,
		preset:          s.preset,
		pauseOnBattery:  s.pauseOnBattery,
		onlyWhenLocked:  s.onlyWhenLocked,
		tempLimit:       s.tempLimit,
		thermalGovernor: s.thermalGovernor,
	}
	override := s.override
	s.mu.Unlock()

	c := s.observe(p, override)
	s.stopOrRestartForMemory(c.memoryShort)
	s.placeLayout(c.idleGated)
	s.placeOnCores()
	target, reason, hard := decide(p, c, override)

	s.mu.Lock()
	prev := s.allowed
	if hard {
		// A hard stop (paused, unplugged, at the temperature limit) zeroes the
		// allowance at once; coming back is the full slow climb.
		s.allowed = 0
	} else {
		// Everything else, including a transient loss of headroom, eases toward
		// the target at the preset's rate — fast down, slow up — so a brief
		// spike dents the allowance rather than wiping it out.
		s.allowed = approach(s.allowed, target, p.preset)
	}
	allowed := s.allowed
	// The duty still applied from the previous tick — what the miner was
	// actually doing during the window this sample observed.
	runningDuty := s.duty
	s.mu.Unlock()

	s.record(c, allowed, runningDuty, prev-allowed > backoffDelta)
	s.apply(allowed, reason)
}

// observe samples the machine. Each reading is best-effort: a monitor that
// cannot answer must not stop the miner, so failures log and fall back to the
// value that changes nothing.
func (s *Scheduler) observe(p policy, override Override) conditions {
	var c conditions

	if p.pauseOnBattery && s.battery != nil {
		onBat, err := s.battery.OnBattery()
		if err != nil {
			log.Printf("battery check error: %v", err)
		} else {
			c.onBattery = onBat
		}
	}
	if s.temp != nil {
		t, err := s.temp.MaxCPU()
		if err != nil {
			log.Printf("temp check error: %v", err)
		} else {
			c.tempC = t
		}
	}
	if p.onlyWhenLocked && s.session != nil {
		c.locked, c.lockKnown = s.session.Locked()
	}
	if s.cpu != nil {
		other, _, err := s.cpu.Usage(s.xmrig.PID())
		if err != nil {
			log.Printf("cpu usage error: %v", err)
		}
		c.otherCPU = other
	}
	c.idleGated = s.idleGated(override)
	if s.gameMode != nil {
		active, known := s.gameMode.Active()
		c.gameActive = known && active
	}
	if s.sampleMemory != nil {
		c.memoryShort = s.memGuard.step(s.sampleMemory(), time.Now())
	}
	if s.nodo != nil && s.nodo.sync != nil {
		synced, known := s.nodo.sync.Synced()
		// Unknown is not "syncing": a node behind an RPC login cannot answer,
		// and reading that as a sync would stop mining forever with no reason
		// the owner could act on.
		c.nodeSyncing = known && !synced
	}
	return c
}

// idleGated reports whether the user still being present should hold the miner
// to the kindest ceiling.
func (s *Scheduler) idleGated(override Override) bool {
	s.mu.Lock()
	after := s.idleFullAfter
	s.mu.Unlock()

	if after <= 0 || override == OverrideMine {
		return false
	}
	idleFor, ok := s.idle.IdleTime()
	if !ok {
		// No idle detection on this system. Falling through keeps the configured
		// kindness rather than throttling forever on a machine that cannot tell.
		return false
	}
	return idleFor < after
}

// record appends one observation to the rolling history. The hashrate comes
// from the engine's own reading, so every series in a Sample is taken at the
// same moment.
func (s *Scheduler) record(c conditions, allowed, runningDuty float64, backedOff bool) {
	if s.history == nil {
		return
	}
	var hr float64
	if h, ok := s.xmrig.(hashrateReporter); ok {
		hr = h.Hashrate()
	}
	// A suspended miner does exactly zero work, whatever the engine's cached
	// reading claims — SIGSTOP freezes its API mid-figure, so without this the
	// final pre-pause hashrate would haunt every average taken while paused.
	if runningDuty <= 0 {
		hr = 0
	}
	sample := stats.Sample{
		At:        time.Now(),
		MinerCPU:  allowed,
		OtherCPU:  c.otherCPU,
		Hashrate:  hr,
		TempC:     c.tempC,
		BackedOff: backedOff,
	}
	if s.power != nil {
		if w, ok := s.power.Watts(); ok {
			sample.Watts = w
			sample.WattsKnown = true
		}
	}
	s.history.Add(sample)
	if s.ledger != nil {
		s.ledger.Add(sample)
	}
}

// Ledger returns the per-minute hashrate record, for the rates and for the
// supervisor to save and restore.
func (s *Scheduler) Ledger() *stats.Ledger { return s.ledger }

// History returns the rolling history of CPU, hashrate and power samples,
// oldest first. Intended for the dashboard chart.
func (s *Scheduler) History() []stats.Sample {
	if s.history == nil {
		return nil
	}
	return s.history.Snapshot()
}

// BackoffCount reports how many times in the last d the miner gave CPU back.
// The dashboard shows it as the count of interruptions, which is the evidence
// that the miner is the one getting out of the way.
func (s *Scheduler) BackoffCount(d time.Duration) int {
	if s.history == nil {
		return 0
	}
	cutoff := time.Now().Add(-d)
	n := 0
	for _, sample := range s.history.Snapshot() {
		if sample.BackedOff && sample.At.After(cutoff) {
			n++
		}
	}
	return n
}

// apply turns a CPU allowance into a duty fraction and hands it to the miner,
// which runs at a fixed thread count and cycles itself to match. There is no
// restart and no thread thrash: the whole fall-fast/rise-slow shape lives in
// the smoothed allowance above, and the engine just tracks it. The state is a
// label derived from the same duty, for the UI.
func (s *Scheduler) apply(allowed float64, reason string) {
	s.mu.Lock()
	full := s.minerFullShare()
	s.mu.Unlock()

	duty := dutyFor(allowed, full)

	s.mu.Lock()
	s.duty = duty
	desired := stateForDuty(duty)
	prev := s.state
	s.state = desired
	s.pausedBy = reason
	s.mu.Unlock()

	s.xmrig.SetDuty(duty)

	if desired != prev {
		select {
		case s.Events <- StateChange{State: desired, Reason: reason}:
		default:
		}
	}
}

// minerFullShare is the fraction of the whole machine the miner occupies when
// running flat out — its thread count over the core count. It is the divisor
// that turns a machine-wide CPU allowance into a per-miner duty. Caller holds
// the lock.
func (s *Scheduler) minerFullShare() float64 {
	if s.cores <= 0 {
		return 1
	}
	threads := s.maxThreads
	if s.layoutThreads > 0 {
		threads = s.layoutThreads
	}
	// More threads than cores to run them on time-share those cores; they
	// cannot occupy more of the machine than the cores they are confined to.
	if s.activeCores > 0 && s.activeCores < threads {
		threads = s.activeCores
	}
	full := float64(threads) / float64(s.cores)
	if full > 1 {
		full = 1
	}
	return full
}

// dutyFor maps a machine-wide CPU allowance to the run fraction that achieves
// it, given how much of the machine the miner covers at full tilt.
func dutyFor(allowed, minerFullShare float64) float64 {
	if allowed <= 0 || minerFullShare <= 0 {
		return 0
	}
	d := allowed / minerFullShare
	if d > 1 {
		return 1
	}
	return d
}

// stateForDuty labels a duty fraction for the UI. The thresholds are cosmetic —
// what protects the machine is the duty itself reaching the engine.
func stateForDuty(duty float64) State {
	switch {
	case duty <= 0:
		return StatePaused
	case duty >= 0.9:
		return StateFull
	case duty <= 0.15:
		return StateMinimal
	default:
		return StateReduced
	}
}
