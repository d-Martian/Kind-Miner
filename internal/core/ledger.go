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

// payoutsPath is where the record of seen payouts lives, beside the ledger.
func payoutsPath() string {
	return filepath.Join(filepath.Dir(autoinstall.BinDir()), "payouts.json")
}

// recordPayout takes one payout line from p2pool. It saves straight away:
// payouts are rare, and one lost to a crash before the next timed save is
// exactly the one the user would have wanted to see.
func recordPayout(p *stats.Payouts, path string, height, atomic uint64, at time.Time) {
	if !p.Record(height, atomic, at) {
		return // the second log line of a payout already recorded
	}
	log.Printf("Payout seen: %d.%012d XMR in block %d", atomic/1_000_000_000_000, atomic%1_000_000_000_000, height)
	if err := p.Save(path); err != nil {
		log.Printf("payouts: could not save: %v", err)
	}
}
