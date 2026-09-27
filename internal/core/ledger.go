package core

import (
	"log"
	"path/filepath"
	"time"

	"github.com/kind-miner/kind-miner/internal/autoinstall"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// ledgerSaveInterval is how often the per-minute hashrate record is written.
// Losing up to ten minutes to a crash costs a sliver of a 24-hour average;
// writing every tick would cost a disk write every two seconds forever.
const ledgerSaveInterval = 10 * time.Minute

// ledgerPath is where the record lives: beside p2pool's data, in kind-miner's
// persistent data directory, which survives restarts inside a Flatpak too.
func ledgerPath() string {
	return filepath.Join(filepath.Dir(autoinstall.BinDir()), "ledger.json")
}

// restoreLedger loads yesterday's record into the scheduler's ledger, so the
// 24-hour rate carries on across a restart instead of starting from nothing.
// A record that cannot be read is logged and replaced: losing the averages is
// no reason not to mine.
func restoreLedger(l *stats.Ledger, path string) {
	if err := l.Load(path, time.Now()); err != nil {
		log.Printf("hashrate history: starting afresh (%v)", err)
	}
}

// saveLedgerEvery writes the ledger on a timer until stop closes.
func saveLedgerEvery(l *stats.Ledger, path string, interval time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			if err := l.Save(path); err != nil {
				log.Printf("hashrate history: could not save: %v", err)
			}
		}
	}
}
