package stats

import (
	"path/filepath"
	"testing"
	"time"
)

func TestPayoutsCountEachBlockOnce(t *testing.T) {
	p := NewPayouts()
	// p2pool logs every payout twice: from P2Pool and from SideChain.
	if !p.Record(3412341, 812345678, t0) {
		t.Fatal("the first sighting was not recorded")
	}
	if p.Record(3412341, 812345678, t0.Add(time.Millisecond)) {
		t.Error("the second log line of the same payout was recorded again")
	}
	if got := p.Recent(5); len(got) != 1 {
		t.Errorf("recent = %v, want one payout", got)
	}
}

func TestPayoutsNewestFirstAndBounded(t *testing.T) {
	p := NewPayouts()
	for h := uint64(1); h <= payoutsKept+10; h++ {
		p.Record(1000+h, h, t0.Add(time.Duration(h)*time.Hour))
	}
	recent := p.Recent(5)
	if len(recent) != 5 || recent[0].Height != 1000+payoutsKept+10 || recent[4].Height != 1000+payoutsKept+6 {
		t.Errorf("recent = %+v, want the five newest, newest first", recent)
	}
	if all := p.Recent(1000); len(all) != payoutsKept {
		t.Errorf("kept %d payouts, want %d", len(all), payoutsKept)
	}
}

func TestPayoutsSurviveARestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "payouts.json")
	p := NewPayouts()
	p.Record(3412341, 812345678, t0)
	p.Record(3412440, 1000000000001, t0.Add(3*time.Hour))
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	q := NewPayouts()
	if err := q.Load(path); err != nil {
		t.Fatal(err)
	}
	got := q.Recent(5)
	if len(got) != 2 || got[0].Atomic != 1000000000001 || !got[1].SeenAt.Equal(t0) {
		t.Errorf("restored %+v", got)
	}
	// A payout seen again after the restart is still the same payout.
	if q.Record(3412440, 1000000000001, time.Now()) {
		t.Error("a restored payout was recorded a second time")
	}
}
