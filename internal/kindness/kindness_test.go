package kindness

import "testing"

// The presets are the product's one user-facing control, so their shape is
// worth pinning down: they must be ordered, bounded, and asymmetric.
func TestPresetsAreOrderedAndBounded(t *testing.T) {
	presets := All()
	if len(presets) != len(Order) {
		t.Fatalf("All() returned %d presets, want %d", len(presets), len(Order))
	}

	for i, p := range presets {
		if p.Ceiling <= 0 || p.Ceiling > 1 {
			t.Errorf("%s: ceiling %.2f out of (0,1]", p.Label, p.Ceiling)
		}
		if p.Fall <= 0 || p.Fall > 1 {
			t.Errorf("%s: fall %.3f out of (0,1]", p.Label, p.Fall)
		}
		if p.Rise <= 0 || p.Rise > 1 {
			t.Errorf("%s: rise %.3f out of (0,1]", p.Label, p.Rise)
		}
		// Backing off must always be faster than coming back. Reversing this on
		// any preset would make the miner noticeable, which is the one thing
		// kind-miner promises it will not be.
		if p.Fall <= p.Rise {
			t.Errorf("%s: fall %.3f must exceed rise %.3f", p.Label, p.Fall, p.Rise)
		}
		if p.Label == "" || p.Blurb == "" {
			t.Errorf("%s: presets are chosen from their labels and blurbs; both must be set", p.Level)
		}
		if i == 0 {
			continue
		}
		prev := presets[i-1]
		if p.Ceiling <= prev.Ceiling {
			t.Errorf("%s ceiling %.2f does not exceed %s's %.2f; the list must run kindest first",
				p.Label, p.Ceiling, prev.Label, prev.Ceiling)
		}
		// Kinder presets give ground faster and take it back slower.
		if p.Fall >= prev.Fall {
			t.Errorf("%s falls at %.3f, no slower than the kinder %s at %.3f", p.Label, p.Fall, prev.Label, prev.Fall)
		}
		if p.Rise <= prev.Rise {
			t.Errorf("%s rises at %.3f, no faster than the kinder %s at %.3f", p.Label, p.Rise, prev.Label, prev.Rise)
		}
	}
}

// An unknown level comes from a hand-edited config. Falling back beats leaving
// the scheduler with a zero-valued preset, whose ceiling of 0 never mines.
func TestGetFallsBackToDefault(t *testing.T) {
	got := Get("nonsense")
	if got.Level != Default {
		t.Errorf("Get(unknown) = %v, want the default %v", got.Level, Default)
	}
	if Get("").Ceiling <= 0 {
		t.Error("Get(\"\") returned a preset with no ceiling")
	}
}

func TestValidAndParseLevel(t *testing.T) {
	for _, l := range Order {
		if !Valid(l) {
			t.Errorf("Valid(%q) = false", l)
		}
		if got, err := ParseLevel(string(l)); err != nil || got != l {
			t.Errorf("ParseLevel(%q) = %q, %v; want %q, nil", l, got, err, l)
		}
	}
	if Valid("nonsense") {
		t.Error("Valid(\"nonsense\") = true")
	}
	if _, err := ParseLevel("nonsense"); err == nil {
		t.Error("ParseLevel(\"nonsense\") returned no error")
	}
}

// The UI hands back the displayed label, so the round trip has to hold for
// every preset or a selection would silently do nothing.
func TestLabelRoundTrip(t *testing.T) {
	labels := Labels()
	if len(labels) != len(Order) {
		t.Fatalf("Labels() returned %d, want %d", len(labels), len(Order))
	}
	for i, label := range labels {
		level, ok := ByLabel(label)
		if !ok {
			t.Errorf("ByLabel(%q) not found", label)
			continue
		}
		if level != Order[i] {
			t.Errorf("ByLabel(%q) = %q, want %q", label, level, Order[i])
		}
	}
	if _, ok := ByLabel("Ferocious"); ok {
		t.Error("ByLabel found a preset that does not exist")
	}
}

// The setup flow and settings window pick from the fuller "Ghost — up to 52%
// CPU" form, so that round trip has to hold too.
func TestOptionRoundTrip(t *testing.T) {
	options := Options()
	if len(options) != len(Order) {
		t.Fatalf("Options() returned %d, want %d", len(options), len(Order))
	}
	for i, option := range options {
		level, ok := ByOption(option)
		if !ok {
			t.Errorf("ByOption(%q) not found", option)
			continue
		}
		if level != Order[i] {
			t.Errorf("ByOption(%q) = %q, want %q", option, level, Order[i])
		}
		// The ceiling is the whole reason this form exists.
		if want := Get(level).Label; option == want {
			t.Errorf("Option(%q) is just the label; it must carry the ceiling too", option)
		}
	}
	if _, ok := ByOption("Ghost"); ok {
		t.Error("ByOption matched a bare label; the two forms must not be interchangeable")
	}
}

func TestCeilingPercent(t *testing.T) {
	cases := map[Level]int{Ghost: 52, Polite: 78, Balanced: 90, Full: 100}
	for level, want := range cases {
		if got := Get(level).CeilingPercent(); got != want {
			t.Errorf("%s ceiling = %d%%, want %d%%", level, got, want)
		}
	}
}

// "greedy" was renamed to "full". Old configs still carry it, so the alias must
// resolve everywhere a level is read, and canonicalise on the way in.
func TestGreedyAliasResolvesToFull(t *testing.T) {
	if Canonical("greedy") != Full {
		t.Errorf("Canonical(greedy) = %q, want full", Canonical("greedy"))
	}
	if !Valid("greedy") {
		t.Error("Valid(greedy) = false; the alias must still load")
	}
	if Get("greedy").Level != Full {
		t.Errorf("Get(greedy) = %q, want the Full preset", Get("greedy").Level)
	}
	got, err := ParseLevel("greedy")
	if err != nil || got != Full {
		t.Errorf("ParseLevel(greedy) = %q, %v; want full, nil", got, err)
	}
	// A current level is returned untouched.
	if Canonical(Polite) != Polite {
		t.Errorf("Canonical(polite) = %q, want polite", Canonical(Polite))
	}
}
