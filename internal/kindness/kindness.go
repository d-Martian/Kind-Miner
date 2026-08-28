// Package kindness describes how much of the machine the miner may take, and
// how fast it gets out of the way when the user needs it back.
//
// This is the one control kind-miner asks people to understand, so it is
// modelled as a small set of named presets rather than a pile of thresholds.
// Each preset is three numbers:
//
//	Ceiling — the share of total CPU that mining plus everything else may
//	          occupy. The miner only ever asks for what is left under it, so
//	          other work always has the rest of the machine reserved.
//	Fall    — how much of the gap to a lower target is closed per tick. High
//	          means stepping aside quickly the moment work arrives.
//	Rise    — the same for climbing back up. Deliberately much smaller than
//	          Fall: coming back slowly is what makes the miner unnoticeable,
//	          because a burst of typing is followed by more typing.
//
// The asymmetry between Fall and Rise is the whole idea. A symmetric
// controller oscillates against bursty desktop load and the user feels every
// swing; falling fast and rising slowly turns the same load into a miner that
// simply is not there while they work.
package kindness

import "fmt"

// Level is the stable identifier for a preset. It is what gets written to the
// config file, so the strings are part of the on-disk format.
type Level string

const (
	Ghost    Level = "ghost"
	Polite   Level = "polite"
	Balanced Level = "balanced"
	Full     Level = "full"
)

// aliases maps superseded level names to their current one. "greedy" was
// renamed to "full" because the miner is never greedy — even at this preset it
// yields to other work; "full" describes what it takes when nothing else wants
// the machine, without implying it fights for it.
var aliases = map[Level]Level{"greedy": Full}

// Canonical resolves an alias to the level it now stands for, leaving current
// levels untouched. Config migration runs stored values through it so an old
// "greedy" is rewritten as "full" the next time the file is saved.
func Canonical(l Level) Level {
	if to, ok := aliases[l]; ok {
		return to
	}
	return l
}

// Default is the preset new installs get, and the one most people keep.
const Default = Polite

// Preset is one named point on the kindness scale.
type Preset struct {
	Level Level
	// Label is the preset's name as shown in the UI.
	Label string
	// Ceiling is the total CPU fraction (0–1) that mining and everything else
	// may occupy together.
	Ceiling float64
	// Fall and Rise are per-tick approach rates in (0,1] towards a lower and a
	// higher target respectively.
	Fall float64
	Rise float64
	// Blurb is the one-sentence explanation shown under the preset picker.
	Blurb string
}

// CeilingPercent returns the ceiling as a whole percentage, for labels like
// "up to 78% CPU".
func (p Preset) CeilingPercent() int { return int(p.Ceiling*100 + 0.5) }

// Order is the presets from kindest to hungriest. UI lists render in this
// order, so it is the source of truth for presentation as well as lookup.
var Order = []Level{Ghost, Polite, Balanced, Full}

var presets = map[Level]Preset{
	Ghost: {
		Level: Ghost, Label: "Ghost", Ceiling: 0.52, Fall: 0.62, Rise: 0.010,
		Blurb: "Only genuinely idle cycles. You will never notice it; payouts are slow.",
	},
	Polite: {
		Level: Polite, Label: "Polite", Ceiling: 0.78, Fall: 0.45, Rise: 0.020,
		Blurb: "Steps aside the moment you touch the machine, and comes back slowly. The default, and the one most people keep.",
	},
	Balanced: {
		Level: Balanced, Label: "Balanced", Ceiling: 0.90, Fall: 0.30, Rise: 0.038,
		Blurb: "Shares the machine evenly. Heavy work still wins, but you may feel a short lag.",
	},
	Full: {
		Level: Full, Label: "Full", Ceiling: 1.00, Fall: 0.15, Rise: 0.075,
		Blurb: "Uses whatever is free, and gives it back slowly. Still yields to your apps — it just aims to fill the rest of the machine. For computers you are not sitting at.",
	},
}

// Get returns the preset for a level, resolving aliases first. Unknown levels
// fall back to Default so a hand-edited config can never leave the scheduler
// without a policy.
func Get(l Level) Preset {
	if p, ok := presets[Canonical(l)]; ok {
		return p
	}
	return presets[Default]
}

// Valid reports whether l names a known preset, including superseded aliases.
func Valid(l Level) bool {
	_, ok := presets[Canonical(l)]
	return ok
}

// All returns every preset, kindest first.
func All() []Preset {
	out := make([]Preset, 0, len(Order))
	for _, l := range Order {
		out = append(out, presets[l])
	}
	return out
}

// Labels returns the preset labels, kindest first — the option list for a
// segmented control or radio group.
func Labels() []string {
	out := make([]string, 0, len(Order))
	for _, l := range Order {
		out = append(out, presets[l].Label)
	}
	return out
}

// ByLabel maps a UI label back to its level. It exists because Fyne's Select
// and RadioGroup widgets hand back the displayed string, not an identifier.
func ByLabel(label string) (Level, bool) {
	for _, l := range Order {
		if presets[l].Label == label {
			return l, true
		}
	}
	return "", false
}

// Option is the preset's name with its ceiling, for pickers with room for it —
// the setup flow and the settings window. The dashboard's inline control uses
// the bare Label instead, where the row is shared with other controls.
func (p Preset) Option() string {
	return fmt.Sprintf("%s — up to %d%% CPU", p.Label, p.CeilingPercent())
}

// Options returns every preset's Option string, kindest first.
func Options() []string {
	out := make([]string, 0, len(Order))
	for _, l := range Order {
		out = append(out, presets[l].Option())
	}
	return out
}

// ByOption maps an Option string back to its level.
func ByOption(option string) (Level, bool) {
	for _, l := range Order {
		if presets[l].Option() == option {
			return l, true
		}
	}
	return "", false
}

// ParseLevel validates a level read from a config file, returning the
// canonical level so a stored alias is normalised on the way in.
func ParseLevel(s string) (Level, error) {
	l := Level(s)
	if !Valid(l) {
		return "", fmt.Errorf("kindness must be one of ghost, polite, balanced, full; got %q", s)
	}
	return Canonical(l), nil
}
