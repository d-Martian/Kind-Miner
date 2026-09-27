package stats

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

var t0 = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

// fill records a sample every 2 s from start for d at hashrate h, as the
// scheduler does.
func fill(l *Ledger, start time.Time, d time.Duration, h float64) time.Time {
	at := start
	for ; at.Before(start.Add(d)); at = at.Add(2 * time.Second) {
		l.Add(Sample{At: at, Hashrate: h})
	}
	return at
}

func TestLedgerRates(t *testing.T) {
	l := NewLedger()
	// Twelve hours at 2 kH/s, then half an hour paused (recorded as zero),
	// then half an hour at 3 kH/s.
	now := fill(l, t0, 12*time.Hour, 2000)
	now = fill(l, now, 30*time.Minute, 0)
	now = fill(l, now, 30*time.Minute, 3000)

	if r, ok := l.Rate(30*time.Minute, now); !ok || r < 2990 || r > 3010 {
		t.Errorf("30 min rate = %v, %v; want ~3000 from the last half hour", r, ok)
	}
	// Thirteen hours of history: the pause counts as zero, the time before
	// the ledger began does not count at all.
	want := (12*2000.0 + 0.5*0 + 0.5*3000) / 13
	if r, ok := l.Rate(24*time.Hour, now); !ok || r < want-5 || r > want+5 {
		t.Errorf("24 h rate = %v, %v; want ~%.0f", r, ok, want)
	}
}

func TestLedgerRateNeedsHalfItsWindow(t *testing.T) {
	l := NewLedger()
	now := fill(l, t0, 10*time.Minute, 2000)
	if _, ok := l.Rate(30*time.Minute, now); ok {
		t.Error("a 30 min rate was quoted from 10 minutes of history")
	}
	if _, ok := l.Rate(24*time.Hour, now); ok {
		t.Error("a 24 h rate was quoted from 10 minutes of history")
	}
	now = fill(l, now, 6*time.Minute, 2000)
	if _, ok := l.Rate(30*time.Minute, now); !ok {
		t.Error("16 minutes of history should be enough for a 30 min rate")
	}
}

func TestLedgerKeepsOnlyADay(t *testing.T) {
	l := NewLedger()
	now := fill(l, t0, 30*time.Hour, 1000)
	if got := len(l.minutes); got > 24*60+1 {
		t.Errorf("ledger holds %d minutes, want at most a day's", got)
	}
	if first := time.Unix(l.minutes[0].At, 0); now.Sub(first) > LedgerSpan+time.Minute {
		t.Errorf("oldest minute is %s old", now.Sub(first))
	}
}

func TestLedgerSurvivesARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ledger.json")
	l := NewLedger()
	now := fill(l, t0, 13*time.Hour, 2500)
	if err := l.Save(path); err != nil {
		t.Fatal(err)
	}
	before, _ := l.Rate(24*time.Hour, now)

	// Back an hour later, as after a reboot.
	restored := NewLedger()
	if err := restored.Load(path, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	after, ok := restored.Rate(24*time.Hour, now.Add(time.Hour))
	if !ok || after != before {
		t.Errorf("24 h rate after restart = %v, %v; want %v", after, ok, before)
	}

	// Two days later everything in it is stale.
	stale := NewLedger()
	if err := stale.Load(path, now.Add(48*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if len(stale.minutes) != 0 {
		t.Errorf("kept %d minutes from two days ago", len(stale.minutes))
	}
}

func TestLedgerLoadIsForgiving(t *testing.T) {
	dir := t.TempDir()
	l := NewLedger()
	if err := l.Load(filepath.Join(dir, "missing.json"), t0); err != nil {
		t.Errorf("a missing ledger is an error: %v", err)
	}
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{half"), 0o644)
	if err := l.Load(bad, t0); err == nil {
		t.Error("a corrupt ledger loaded silently")
	}
	if len(l.minutes) != 0 {
		t.Error("a corrupt ledger left data behind")
	}
	future := filepath.Join(dir, "future.json")
	os.WriteFile(future, []byte(`{"version":1,"minutes":[{"t":`+
		itoa(t0.Add(time.Hour).Unix())+`,"sum":100,"n":1}]}`), 0o644)
	if err := l.Load(future, t0); err != nil || len(l.minutes) != 0 {
		t.Errorf("a minute from the future was kept (err %v)", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
