package stats

import (
	"sync"
	"testing"
	"time"
)

func sampleAt(i int) Sample {
	return Sample{
		At:       time.Unix(int64(i), 0),
		MinerCPU: float64(i) / 100,
		OtherCPU: float64(i) / 200,
		Hashrate: float64(i * 10),
	}
}

func TestRingBelowCapacity(t *testing.T) {
	r := NewRing(5)
	for i := 0; i < 3; i++ {
		r.Add(sampleAt(i))
	}

	if got := r.Len(); got != 3 {
		t.Errorf("Len() = %d, want 3", got)
	}
	got := r.Snapshot()
	if len(got) != 3 {
		t.Fatalf("Snapshot() length = %d, want 3", len(got))
	}
	for i, s := range got {
		if s.At != time.Unix(int64(i), 0) {
			t.Errorf("sample %d = %v, want %v", i, s.At, time.Unix(int64(i), 0))
		}
	}
}

// Once full, the oldest samples are dropped and the rest stay in order — the
// wrap-around is where a ring buffer usually goes wrong.
func TestRingWrapsAndKeepsOrder(t *testing.T) {
	r := NewRing(4)
	for i := 0; i < 10; i++ {
		r.Add(sampleAt(i))
	}

	if got := r.Len(); got != 4 {
		t.Errorf("Len() = %d, want 4", got)
	}
	got := r.Snapshot()
	if len(got) != 4 {
		t.Fatalf("Snapshot() length = %d, want 4", len(got))
	}
	// Samples 6,7,8,9 should remain, oldest first.
	for i, s := range got {
		want := time.Unix(int64(6+i), 0)
		if s.At != want {
			t.Errorf("sample %d = %v, want %v", i, s.At, want)
		}
	}
}

func TestRingEmpty(t *testing.T) {
	r := NewRing(3)
	if got := r.Len(); got != 0 {
		t.Errorf("Len() = %d, want 0", got)
	}
	if got := r.Snapshot(); len(got) != 0 {
		t.Errorf("Snapshot() length = %d, want 0", len(got))
	}
}

func TestRingCapacityFloor(t *testing.T) {
	// A zero or negative capacity must not panic on the first Add.
	for _, capacity := range []int{0, -1} {
		r := NewRing(capacity)
		r.Add(sampleAt(1))
		if got := r.Len(); got != 1 {
			t.Errorf("NewRing(%d): Len() = %d, want 1", capacity, got)
		}
	}
}

// Snapshot returns a copy, so a caller rendering it cannot be mutated underneath.
func TestSnapshotIsACopy(t *testing.T) {
	r := NewRing(3)
	r.Add(sampleAt(1))
	got := r.Snapshot()
	got[0].MinerCPU = 999

	if again := r.Snapshot(); again[0].MinerCPU == 999 {
		t.Error("mutating a snapshot changed the ring's own data")
	}
}

// The scheduler writes while the GUI reads; -race should stay quiet.
func TestRingConcurrentUse(t *testing.T) {
	r := NewRing(64)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			r.Add(sampleAt(i))
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = r.Snapshot()
			_ = r.Len()
		}
	}()
	wg.Wait()
}
