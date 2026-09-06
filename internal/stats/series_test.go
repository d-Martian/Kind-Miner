package stats

import (
	"math"
	"testing"
	"time"
)

// at builds a history whose samples are one second apart, ending now.
func at(values ...Sample) []Sample {
	now := time.Now()
	out := make([]Sample, len(values))
	for i, s := range values {
		s.At = now.Add(time.Duration(i-len(values)+1) * time.Second)
		out[i] = s
	}
	return out
}

// The window is measured back from the newest sample, not from the wall clock:
// a scheduler that has stalled should leave the last picture standing rather
// than sliding it away into an empty plot.
func TestSinceAnchorsOnTheData(t *testing.T) {
	stale := []Sample{
		{At: time.Now().Add(-2 * time.Hour), OtherCPU: 0.1},
		{At: time.Now().Add(-2*time.Hour + time.Second), OtherCPU: 0.2},
	}
	if got := Since(stale, 30*time.Second); len(got) != 2 {
		t.Errorf("Since() kept %d of 2 stale samples; old data must still be drawn", len(got))
	}
}

func TestSinceWindow(t *testing.T) {
	h := at(
		Sample{OtherCPU: 0.1},
		Sample{OtherCPU: 0.2},
		Sample{OtherCPU: 0.3},
		Sample{OtherCPU: 0.4},
	)
	got := Since(h, 2*time.Second)
	if len(got) != 3 {
		t.Fatalf("Since(2s) returned %d samples, want 3", len(got))
	}
	if got[0].OtherCPU != 0.2 {
		t.Errorf("oldest kept sample = %.1f, want 0.2", got[0].OtherCPU)
	}

	if got := Since(nil, time.Second); got != nil {
		t.Errorf("Since(nil) = %v, want nil", got)
	}
	if got := Since(h, 0); got != nil {
		t.Errorf("Since(0) = %v, want nil", got)
	}
}

func TestMeanAndPeak(t *testing.T) {
	h := at(
		Sample{Hashrate: 1000},
		Sample{Hashrate: 3000},
		Sample{Hashrate: 2000},
	)

	if got, ok := MeanAll(h, Hashrate); !ok || math.Abs(got-2000) > 1e-9 {
		t.Errorf("MeanAll = %.1f (ok=%v), want 2000", got, ok)
	}
	if got, ok := Mean(h, time.Second, Hashrate); !ok || math.Abs(got-2500) > 1e-9 {
		t.Errorf("Mean(1s) = %.1f (ok=%v), want 2500", got, ok)
	}
	if got, ok := Peak(h, Hashrate); !ok || got != 3000 {
		t.Errorf("Peak = %.1f (ok=%v), want 3000", got, ok)
	}

	// An empty history must report "no value", which is not the same as zero —
	// the dashboard shows "—" for one and a real number for the other.
	if _, ok := MeanAll(nil, Hashrate); ok {
		t.Error("MeanAll(nil) reported a value")
	}
	if _, ok := Peak(nil, Hashrate); ok {
		t.Error("Peak(nil) reported a value")
	}
	if _, ok := Mean(nil, time.Minute, Hashrate); ok {
		t.Error("Mean(nil) reported a value")
	}
}

// Most machines cannot read their energy counters at all, and some can only
// read them intermittently. Averaging in the gaps as zero would understate
// power draw; reporting "unknown" is the honest answer.
func TestMeanWattsIgnoresUnknownReadings(t *testing.T) {
	h := at(
		Sample{Watts: 40, WattsKnown: true},
		Sample{Watts: 0, WattsKnown: false},
		Sample{Watts: 60, WattsKnown: true},
	)
	got, ok := MeanWatts(h, time.Minute)
	if !ok {
		t.Fatal("MeanWatts reported no value despite two readings")
	}
	if math.Abs(got-50) > 1e-9 {
		t.Errorf("MeanWatts = %.1f, want 50", got)
	}

	none := at(Sample{WattsKnown: false}, Sample{WattsKnown: false})
	if _, ok := MeanWatts(none, time.Minute); ok {
		t.Error("MeanWatts reported a value with no readable counters")
	}
}

func TestCountBackoffs(t *testing.T) {
	h := at(
		Sample{BackedOff: true},
		Sample{BackedOff: false},
		Sample{BackedOff: true},
		Sample{BackedOff: true},
	)
	if got := CountBackoffs(h, time.Hour); got != 3 {
		t.Errorf("CountBackoffs(1h) = %d, want 3", got)
	}
	if got := CountBackoffs(h, time.Second); got != 2 {
		t.Errorf("CountBackoffs(1s) = %d, want 2", got)
	}
	if got := CountBackoffs(nil, time.Minute); got != 0 {
		t.Errorf("CountBackoffs(nil) = %d, want 0", got)
	}
}

func TestLatestAndTotal(t *testing.T) {
	h := at(Sample{MinerCPU: 0.1, OtherCPU: 0.2}, Sample{MinerCPU: 0.3, OtherCPU: 0.4})
	got, ok := Latest(h)
	if !ok {
		t.Fatal("Latest reported nothing for a non-empty history")
	}
	if math.Abs(got.Total()-0.7) > 1e-9 {
		t.Errorf("Total() = %.2f, want 0.70", got.Total())
	}
	if _, ok := Latest(nil); ok {
		t.Error("Latest(nil) reported a sample")
	}
}
