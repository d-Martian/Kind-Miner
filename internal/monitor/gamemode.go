package monitor

import (
	"sync"
	"time"
)

// GameMode reports whether Feral's GameMode daemon has a game registered.
// Games that support it (and anything launched with gamemoderun) register while
// they run, which is a clearer signal of "I need this machine" than any CPU
// reading: a game can be GPU-bound and leave cores idle that it will want the
// moment a level loads.
type GameMode struct {
	mu        sync.Mutex
	checkedAt time.Time
	active    bool
	known     bool
	backend   gameModeBackend
}

// gameModeBackend asks the daemon; a narrow interface so the cache and the
// "unknown" handling can be tested without a session bus.
type gameModeBackend interface {
	clientCount() (int, error)
}

// gameModeTTL is how long an answer is reused. The scheduler ticks every two
// seconds; a game starting is not so urgent that it needs a D-Bus call on
// every one of them.
const gameModeTTL = 5 * time.Second

// NewGameMode returns a GameMode sampler for this session.
func NewGameMode() *GameMode { return &GameMode{backend: newGameModeBackend()} }

// Active reports whether a game has GameMode engaged. known is false when
// there is no GameMode daemon to ask — not installed, no session bus, or a
// Flatpak without permission to talk to it — and a caller must not pause on
// that: most machines have no GameMode at all.
func (g *GameMode) Active() (active, known bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.checkedAt.IsZero() && time.Since(g.checkedAt) < gameModeTTL {
		return g.active, g.known
	}
	g.checkedAt = time.Now()
	if g.backend == nil {
		g.active, g.known = false, false
		return false, false
	}
	n, err := g.backend.clientCount()
	g.active, g.known = n > 0, err == nil
	return g.active, g.known
}
