package gui

import (
	"testing"
	"time"

	"github.com/kind-miner/kind-miner/internal/stats"
)

func TestPayoutLines(t *testing.T) {
	now := time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC)
	list := []stats.Payout{
		{Height: 3412440, Atomic: 1_000_000_000_001, SeenAt: now.Add(-80 * time.Minute)},
		{Height: 3412341, Atomic: 812_345_678, SeenAt: now.Add(-3 * time.Hour)},
	}
	got := payoutLines(list, now)
	want := []string{
		// Still inside the 60-block coinbase lock: says when it can be spent.
		"1.0000 XMR · 1h ago · spendable in ≈ 40m",
		"0.0008123 XMR · 3h ago",
	}
	if len(got) != len(want) {
		t.Fatalf("got %q", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestPayoutLinesShowFiveAndSayWhenThereAreNone(t *testing.T) {
	now := time.Now()
	if got := payoutLines(nil, now); len(got) != 1 || got[0] != payoutsNoneYet {
		t.Errorf("empty record = %q", got)
	}
	var many []stats.Payout
	for i := 0; i < 8; i++ {
		many = append(many, stats.Payout{Height: uint64(100 - i), Atomic: 1e9, SeenAt: now.Add(-time.Duration(i+3) * time.Hour)})
	}
	if got := payoutLines(many, now); len(got) != payoutsShown {
		t.Errorf("listed %d payouts, want %d", len(got), payoutsShown)
	}
}

func TestFormatAge(t *testing.T) {
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now",
		25 * time.Minute: "25m ago",
		5 * time.Hour:    "5h ago",
		72 * time.Hour:   "3d ago",
	} {
		if got := formatAge(d); got != want {
			t.Errorf("formatAge(%s) = %q, want %q", d, got, want)
		}
	}
}
