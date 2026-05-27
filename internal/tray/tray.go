// Package tray manages the system-tray icon and menu.
package tray

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"time"

	"github.com/getlantern/systray"
	"github.com/kind-miner/kind-miner/internal/config"
	"github.com/kind-miner/kind-miner/internal/engine"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

// Quit signals the system tray to exit. Safe to call from any goroutine.
func Quit() { systray.Quit() }

// Tray owns the system-tray lifecycle.
type Tray struct {
	cfg       *config.Config
	sched     *scheduler.Scheduler
	xmrig     *engine.XMRig
	stopCh    chan struct{}
}

// New creates a Tray. Call Run to start the event loop (blocks until quit).
func New(cfg *config.Config, sched *scheduler.Scheduler, xmrig *engine.XMRig) *Tray {
	return &Tray{
		cfg:    cfg,
		sched:  sched,
		xmrig:  xmrig,
		stopCh: make(chan struct{}),
	}
}

// Run starts the system-tray event loop on the calling goroutine.
// On macOS this must be called from the main thread.
func (t *Tray) Run() {
	systray.Run(t.onReady, t.onExit)
}

func (t *Tray) onReady() {
	systray.SetIcon(iconMining)
	systray.SetTitle("")
	systray.SetTooltip("kind-miner — starting…")

	mStatus := systray.AddMenuItem("Starting…", "")
	mStatus.Disable()
	systray.AddSeparator()

	mToggle := systray.AddMenuItem("Pause", "Pause or resume mining")
	mConfig := systray.AddMenuItem("Open Config", "Edit configuration file")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit kind-miner", "Stop mining and exit")

	// Refresh tray label/icon periodically.
	go t.refreshLoop(mStatus, mToggle)

	// Handle menu clicks.
	go func() {
		for {
			select {
			case <-mToggle.ClickedCh:
				if t.sched.IsManuallyPaused() {
					t.sched.ManualResume()
					mToggle.SetTitle("Pause")
				} else {
					t.sched.ManualPause()
					mToggle.SetTitle("Resume")
				}
			case <-mConfig.ClickedCh:
				openEditor(config.Path())
			case <-mQuit.ClickedCh:
				systray.Quit()
			}
		}
	}()
}

func (t *Tray) onExit() {
	close(t.stopCh)
}

// WaitForQuit blocks until the user clicks Quit.
func (t *Tray) WaitForQuit() {
	<-t.stopCh
}

func (t *Tray) refreshLoop(mStatus, mToggle *systray.MenuItem) {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			t.refresh(mStatus, mToggle)
		case <-t.stopCh:
			return
		}
	}
}

func (t *Tray) refresh(mStatus, mToggle *systray.MenuItem) {
	state, reason := t.sched.CurrentState()
	hr := t.xmrig.Hashrate()

	var icon []byte
	var tooltip, statusText string

	switch state {
	case scheduler.StateFull:
		icon = iconMining
		tooltip = fmt.Sprintf("kind-miner — %s", formatHashrate(hr))
		statusText = fmt.Sprintf("Mining  %s", formatHashrate(hr))
	case scheduler.StateReduced:
		icon = iconThrottle
		tooltip = fmt.Sprintf("kind-miner — throttled  %s", formatHashrate(hr))
		statusText = fmt.Sprintf("Throttled  %s", formatHashrate(hr))
	case scheduler.StateMinimal:
		icon = iconThrottle
		tooltip = fmt.Sprintf("kind-miner — minimal  %s", formatHashrate(hr))
		statusText = fmt.Sprintf("Minimal  %s", formatHashrate(hr))
	case scheduler.StatePaused:
		icon = iconPaused
		if reason != "" {
			tooltip = fmt.Sprintf("kind-miner — paused (%s)", reason)
			statusText = fmt.Sprintf("Paused — %s", reason)
		} else {
			tooltip = "kind-miner — paused"
			statusText = "Paused"
		}
	}

	systray.SetIcon(icon)
	systray.SetTooltip(tooltip)
	mStatus.SetTitle(statusText)

	if t.sched.IsManuallyPaused() {
		mToggle.SetTitle("Resume")
	} else {
		mToggle.SetTitle("Pause")
	}
}

// formatHashrate formats a H/s value into a human-readable string.
func formatHashrate(hs float64) string {
	switch {
	case hs >= 1_000_000:
		return fmt.Sprintf("%.2f MH/s", hs/1_000_000)
	case hs >= 1_000:
		return fmt.Sprintf("%.2f kH/s", hs/1_000)
	case hs > 0:
		return fmt.Sprintf("%.2f H/s", hs)
	default:
		return "— H/s"
	}
}

// openEditor opens the config file in the default system editor.
func openEditor(path string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("notepad", path)
	case "darwin":
		cmd = exec.Command("open", "-t", path)
	default:
		editor := os.Getenv("VISUAL")
		if editor == "" {
			editor = "xdg-open"
		}
		cmd = exec.Command(editor, path)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open editor: %v", err)
	}
}
