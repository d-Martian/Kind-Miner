package stats

import "time"

// Series helpers for reading a history. They exist so the dashboard's numbers
// and its chart are computed from one definition of "the last minute" — a
// tooltip that disagreed with the line above it would undermine the point of
// showing the chart at all.

// Since returns the tail of samples falling within window, measured back from
// the newest sample rather than from now.
//
// Anchoring on the data rather than the clock means a stalled scheduler leaves
// the last picture standing instead of sliding it away into an empty plot — the
// user sees stale data, not a claim that nothing is happening.
func Since(samples []Sample, window time.Duration) []Sample {
	if len(samples) == 0 || window <= 0 {
		return nil
	}
	cutoff := samples[len(samples)-1].At.Add(-window)
	for i := len(samples) - 1; i >= 0; i-- {
		if samples[i].At.Before(cutoff) {
			return samples[i+1:]
		}
	}
	return samples
}

// Mean averages a field over the samples within window. ok is false when the
// window holds nothing to average.
func Mean(samples []Sample, window time.Duration, pick func(Sample) float64) (float64, bool) {
	in := Since(samples, window)
	if len(in) == 0 {
		return 0, false
	}
	var sum float64
	for _, s := range in {
		sum += pick(s)
	}
	return sum / float64(len(in)), true
}

// MeanAll averages a field over every sample held.
func MeanAll(samples []Sample, pick func(Sample) float64) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	var sum float64
	for _, s := range samples {
		sum += pick(s)
	}
	return sum / float64(len(samples)), true
}

// Peak returns the highest value a field reached. ok is false for an empty
// history, which is distinct from a genuine peak of zero.
func Peak(samples []Sample, pick func(Sample) float64) (float64, bool) {
	if len(samples) == 0 {
		return 0, false
	}
	max := pick(samples[0])
	for _, s := range samples[1:] {
		if v := pick(s); v > max {
			max = v
		}
	}
	return max, true
}

// Latest returns the most recent sample.
func Latest(samples []Sample) (Sample, bool) {
	if len(samples) == 0 {
		return Sample{}, false
	}
	return samples[len(samples)-1], true
}

// MeanWatts averages package power over window, skipping samples taken where
// the energy counter was unreadable. ok is false when none of them had one, so
// the caller shows "—" rather than an average of nothing.
func MeanWatts(samples []Sample, window time.Duration) (float64, bool) {
	var sum float64
	var n int
	for _, s := range Since(samples, window) {
		if s.WattsKnown {
			sum += s.Watts
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// CountBackoffs reports how many times within window the miner gave CPU back.
func CountBackoffs(samples []Sample, window time.Duration) int {
	n := 0
	for _, s := range Since(samples, window) {
		if s.BackedOff {
			n++
		}
	}
	return n
}

// Field accessors, so callers read Mean(h, time.Minute, stats.Hashrate) rather
// than restating a closure at every call site.
func Hashrate(s Sample) float64 { return s.Hashrate }
func MinerCPU(s Sample) float64 { return s.MinerCPU }
func OtherCPU(s Sample) float64 { return s.OtherCPU }
func TotalCPU(s Sample) float64 { return s.Total() }
func TempC(s Sample) float64    { return s.TempC }
