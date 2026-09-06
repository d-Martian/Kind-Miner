package monitor

import (
	"math"
	"testing"
)

// Power is attributed proportionally, which is an approximation everywhere in
// between but must be exactly right at the two ends: all of it when only the
// miner is running, none of it when the miner is stopped.
func TestAttributeToMiner(t *testing.T) {
	tests := []struct {
		name            string
		watts           float64
		minerCPU, other float64
		want            float64
		wantOK          bool
	}{
		{"miner alone owns all of it", 60, 0.5, 0, 60, true},
		{"a stopped miner owns none of it", 60, 0, 0.5, 0, true},
		{"an even split", 60, 0.25, 0.25, 30, true},
		{"a quarter of the load", 80, 0.1, 0.3, 20, true},
		{"an idle machine cannot be split", 60, 0, 0, 0, false},
		{"no reading to attribute", 0, 0.5, 0.1, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := AttributeToMiner(tt.watts, tt.minerCPU, tt.other)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && math.Abs(got-tt.want) > 1e-9 {
				t.Errorf("watts = %.2f, want %.2f", got, tt.want)
			}
		})
	}
}

// A lock state that cannot be determined is not the same as "unlocked": the
// caller must be able to tell the difference, or the mine-only-when-locked
// setting would silently stop mining on every machine that cannot answer.
func TestSessionUnknownIsNotUnlocked(t *testing.T) {
	var none *Session
	locked, known := none.Locked()
	if locked || known {
		t.Errorf("nil Session reported locked=%v known=%v, want false/false", locked, known)
	}
}

// Constructing the monitors must never panic or block, whatever the machine
// underneath supports — they run on every desktop kind-miner is installed on.
func TestConstructorsAreSafeEverywhere(t *testing.T) {
	if NewSession() == nil {
		t.Error("NewSession returned nil")
	}
	p := NewPower()
	if p == nil {
		t.Fatal("NewPower returned nil")
	}
	// Whatever this machine supports, the pair must agree: an unavailable
	// counter can never produce a reading.
	if w, ok := p.Watts(); ok && !p.Available() {
		t.Errorf("Watts() returned %.1f while Available() is false", w)
	}
}
