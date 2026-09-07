package gui

import (
	"strings"
	"testing"
	"time"
)

// The house rule these tests protect: never show more precision than the number
// deserves, and never show a number at all when there isn't one.

func TestFormatHashrate(t *testing.T) {
	cases := map[float64]string{
		0:         emDash,
		-1:        emDash,
		850:       "850 H/s",
		5940:      "5.94 kH/s",
		2_500_000: "2.50 MH/s",
	}
	for in, want := range cases {
		if got := formatHashrate(in); got != want {
			t.Errorf("formatHashrate(%.0f) = %q, want %q", in, got, want)
		}
	}
}

func TestHashrateParts(t *testing.T) {
	value, unit := hashrateParts(5940)
	if value != "5.94" || unit != "kH/s" {
		t.Errorf("hashrateParts(5940) = %q, %q; want 5.94, kH/s", value, unit)
	}
	// A missing reading yields no unit, so the tile does not typeset "— H/s".
	if value, unit := hashrateParts(0); value != emDash || unit != "" {
		t.Errorf("hashrateParts(0) = %q, %q; want %q, \"\"", value, unit, emDash)
	}
}

// "0 W" claims a measurement. "—" admits there isn't one, which is the truth on
// every machine whose energy counters are root-only.
func TestUnknownValuesShowADash(t *testing.T) {
	if got := formatWatts(0, false); got != emDash {
		t.Errorf("formatWatts(unknown) = %q, want %q", got, emDash)
	}
	if got := formatWatts(41, false); got != emDash {
		t.Errorf("formatWatts with a stale figure = %q, want %q", got, emDash)
	}
	if got := formatWatts(41, true); got != "41" {
		t.Errorf("formatWatts(41) = %q, want 41", got)
	}
	if got := formatXMR(0, 5); got != emDash {
		t.Errorf("formatXMR(0) = %q, want %q", got, emDash)
	}
	if got := formatTemp(0); got != emDash {
		t.Errorf("formatTemp(0) = %q, want %q", got, emDash)
	}
	if got := formatEfficiency(5940, 0, true); got != emDash {
		t.Errorf("formatEfficiency with no power = %q, want %q", got, emDash)
	}
	if got := formatEfficiency(5940, 41, false); got != emDash {
		t.Errorf("formatEfficiency with an unknown reading = %q, want %q", got, emDash)
	}
	if got := formatEfficiency(5940, 41, true); got != "145 H/W" {
		t.Errorf("formatEfficiency(5940, 41) = %q, want 145 H/W", got)
	}
}

func TestFormatUptime(t *testing.T) {
	cases := map[time.Duration]string{
		0:                            emDash,
		14 * time.Minute:             "14m",
		3*time.Hour + 14*time.Minute: "3h 14m",
		25*time.Hour + 5*time.Minute: "25h 05m",
	}
	for in, want := range cases {
		if got := formatUptime(in); got != want {
			t.Errorf("formatUptime(%v) = %q, want %q", in, got, want)
		}
	}
}

// Backing off is the miner keeping its promise, so the wording says so rather
// than reading as an error count.
func TestFormatBackoff(t *testing.T) {
	cases := []struct {
		n      int
		paused bool
		want   string
	}{
		{0, false, "no interruptions this minute"},
		{1, false, "stepped aside once this minute"},
		{4, false, "stepped aside 4× this minute"},
		{4, true, "paused"},
	}
	for _, tt := range cases {
		if got := formatBackoff(tt.n, tt.paused); got != tt.want {
			t.Errorf("formatBackoff(%d, %v) = %q, want %q", tt.n, tt.paused, got, tt.want)
		}
	}
}

func TestFormatBigNumber(t *testing.T) {
	cases := map[float64]string{
		542:               "542",
		4_520:             "4.52 k",
		4_520_000:         "4.52 M",
		4_520_000_000:     "4.52 G",
		542_600_000_000:   "542.60 G",
		1_200_000_000_000: "1.20 T",
	}
	for in, want := range cases {
		if got := formatBigNumber(in); got != want {
			t.Errorf("formatBigNumber(%.0f) = %q, want %q", in, got, want)
		}
	}
}

// The countdown coarsens above 90 seconds so the label is not restless — a
// per-second tick that far out is noise, and on the tray it would rebuild the
// menu (closing it) every second.
func TestFormatCountdown(t *testing.T) {
	cases := map[int]string{
		5:   "5s",
		90:  "90s",
		91:  "~2m",
		300: "~5m",
	}
	for in, want := range cases {
		if got := formatCountdown(in); got != want {
			t.Errorf("formatCountdown(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestFormatPercent(t *testing.T) {
	if got := formatPercent(0.723); got != "72%" {
		t.Errorf("formatPercent(0.723) = %q, want 72%%", got)
	}
}

func TestChartWindowRoundTrip(t *testing.T) {
	for _, seconds := range []int{30, 60, 120} {
		label := chartWindowLabel(seconds)
		if got := chartWindowSeconds(label); got != seconds {
			t.Errorf("chartWindowSeconds(%q) = %d, want %d", label, got, seconds)
		}
	}
	if got := chartWindowLabel(120); got != "2m" {
		t.Errorf("chartWindowLabel(120) = %q, want 2m", got)
	}
	// An unrecognised label must fall back rather than yield a zero window,
	// which would divide the chart by nothing.
	if got := chartWindowSeconds("nonsense"); got <= 0 {
		t.Errorf("chartWindowSeconds(nonsense) = %d, want a positive fallback", got)
	}
}

// canvas.Text does not wrap, so its width becomes a minimum width for
// everything containing it, and Fyne sizes a window to its content's minimum.
// A live run proved the cost: one un-elided .onion address in the details panel
// stretched the window past 2300px.
func TestShortenMiddle(t *testing.T) {
	const onion = "44yfclfdry66bbpyolux6xmfkcapage7khfk5sax7xmmylwwmjaqukad.onion:18089 · via Tor"

	got := shortenMiddle(onion, valueWidth)
	if runes := len([]rune(got)); runes > valueWidth {
		t.Errorf("shortened to %d runes, want at most %d: %q", runes, valueWidth, got)
	}
	// Both ends identify the value at a glance, so both must survive.
	if !strings.HasPrefix(got, "44yfclf") {
		t.Errorf("lost the start of the address: %q", got)
	}
	if !strings.HasSuffix(got, "via Tor") {
		t.Errorf("lost the end of the value: %q", got)
	}
	if !strings.Contains(got, "…") {
		t.Errorf("no ellipsis to signal the elision: %q", got)
	}

	// Short values pass through untouched — eliding them would only lose
	// information for no gain.
	for _, s := range []string{"", "mini", "5.94 kH/s", emDash} {
		if got := shortenMiddle(s, valueWidth); got != s {
			t.Errorf("shortenMiddle(%q) = %q, want it unchanged", s, got)
		}
	}
	// A value exactly at the limit is not elided.
	exact := strings.Repeat("x", valueWidth)
	if got := shortenMiddle(exact, valueWidth); got != exact {
		t.Errorf("a value of exactly the limit was elided: %q", got)
	}
	// A nonsensical limit must not panic or produce garbage.
	if got := shortenMiddle(onion, 0); got != onion {
		t.Errorf("shortenMiddle with a zero limit = %q, want it unchanged", got)
	}
}

func TestNormaliseChain(t *testing.T) {
	for _, c := range []string{"main", "mini", "nano"} {
		if got := normaliseChain(c); got != c {
			t.Errorf("normaliseChain(%q) = %q, want %q", c, got, c)
		}
	}
	// A typo must land on mini, never on main, where a small miner may never
	// accumulate a payout.
	if got := normaliseChain("minni"); got != "mini" {
		t.Errorf("normaliseChain(minni) = %q, want mini", got)
	}
	if got := normaliseChain(""); got != "mini" {
		t.Errorf("normaliseChain(\"\") = %q, want mini", got)
	}
}

func TestWiFiPowerSaveText(t *testing.T) {
	cases := []struct {
		name    string
		iface   string
		enabled bool
		known   bool
		want    string
	}{
		{
			name: "a wired machine cannot be checked and must not read as off",
			want: emDash,
		},
		{
			name:  "an unreadable setting is unknown, not off",
			iface: "wlan0", enabled: true, known: false,
			want: emDash,
		},
		{
			name:  "power save on is called out",
			iface: "wlp47s0f0", enabled: true, known: true,
			want: WiFiPowerSaveOn,
		},
		{
			name:  "a link that was checked and is fine says so",
			iface: "wlp47s0f0", enabled: false, known: true,
			want: WiFiPowerSaveOff,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := wifiPowerSaveText(c.iface, c.enabled, c.known); got != c.want {
				t.Errorf("wifiPowerSaveText(%q,%v,%v) = %q, want %q",
					c.iface, c.enabled, c.known, got, c.want)
			}
		})
	}
}

// The row is elided past valueWidth, so the warning has to survive intact.
func TestWiFiPowerSaveTextFitsTheColumn(t *testing.T) {
	for _, s := range []string{WiFiPowerSaveOn, WiFiPowerSaveOff} {
		if got := shortenMiddle(s, valueWidth); got != s {
			t.Errorf("%q is elided to %q; shorten it", s, got)
		}
	}
}
