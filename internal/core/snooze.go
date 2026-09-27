package core

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/scheduler"
)

// The user's pause outlives the process. kind-miner starts with the computer,
// so a reboot after "until tomorrow" — or "until I resume" — would otherwise
// bring the miner straight back, which is the one thing a paused user did not
// ask for.

func snoozePath() string {
	return filepath.Join(filepath.Dir(autoinstall.BinDir()), "snooze.json")
}

type snoozeFile struct {
	Paused bool      `json:"paused"`
	Until  time.Time `json:"until,omitempty"` // zero: until the user resumes
}

func saveSnooze(path string, paused bool, until time.Time) {
	data, _ := json.Marshal(snoozeFile{Paused: paused, Until: until})
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err == nil {
		tmp := path + ".tmp"
		if err = os.WriteFile(tmp, data, 0o644); err == nil {
			err = os.Rename(tmp, path)
		}
		if err == nil {
			return
		}
		log.Printf("pause: could not save: %v", err)
	}
}

// loadSnooze reads the saved pause. A snooze whose time has passed is no pause
// at all; so is a missing or unreadable file — mining normally is the default
// the user already chose by installing it.
func loadSnooze(path string, now time.Time) (paused bool, until time.Time) {
	data, err := os.ReadFile(path)
	if err != nil {
		return false, time.Time{}
	}
	var f snoozeFile
	if json.Unmarshal(data, &f) != nil || !f.Paused {
		return false, time.Time{}
	}
	if !f.Until.IsZero() && !now.Before(f.Until) {
		return false, time.Time{}
	}
	return true, f.Until
}

// keepPauseAcrossRestarts restores a saved pause into sched and saves every
// later change. The hook goes in first, so the file always mirrors what the
// scheduler is doing, whichever path changed it.
func keepPauseAcrossRestarts(sched *scheduler.Scheduler, path string) {
	sched.SetPauseHook(func(paused bool, until time.Time) { saveSnooze(path, paused, until) })
	if paused, until := loadSnooze(path, time.Now()); paused {
		sched.PauseUntil(until)
		if until.IsZero() {
			log.Println("Mining stays paused, as you left it; resume from the tray")
		} else {
			log.Printf("Mining stays paused until %s, as you left it", until.Local().Format("Mon 15:04"))
		}
	}
}
