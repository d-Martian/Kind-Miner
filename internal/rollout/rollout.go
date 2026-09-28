// Package rollout decides whether a newly installed pair of engines — the
// xmrig and p2pool the .deb ships — is fit to keep, and which pair to run.
//
// Engines move only with a kind-minerd release, and a Nodo upgrades itself
// once a week with nobody watching. A p2pool that cannot sync, or an xmrig
// that crashes on the owner's CPU, would otherwise mine nothing until someone
// happened to run kind-minerd status. So the package keeps the previous pair
// on disk, the daemon puts a new pair on probation for its first minutes, and
// a pair that fails is set aside for the one before it, loudly.
//
// Policy is pure here, in the scheduler's style: the daemon supplies
// observations and a clock, and nothing in this package starts a process.
package rollout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Probation is how long a new pair has to show it works. Long enough for
// p2pool to sync the sidechain from a cold start and for the island check to
// have had its grace period; short enough that a bad release costs a quarter
// of an hour, not a week.
const Probation = 15 * time.Minute

// Observation is one look at a running pair.
type Observation struct {
	// P2PoolExpected is false on a machine that runs no p2pool of its own.
	P2PoolExpected bool
	// P2PoolSynced: p2pool is writing statistics for a sidechain.
	P2PoolSynced bool
	// Island is the island check's reason for holding the miner, if it is.
	Island string
	// MinerAllowed: the scheduler gave xmrig CPU at this moment. A Nodo
	// that does not mine on itself, or a desktop in use, never does, and
	// xmrig cannot then be judged.
	MinerAllowed bool
	// Hashing: xmrig reported a hashrate above zero.
	Hashing bool
}

// Verdict is where a probation stands.
type Verdict int

const (
	Pending Verdict = iota
	Healthy
	Failed
)

// Check is one pair's probation.
type Check struct {
	started time.Time
	synced  bool
	hashed  bool
	allowed bool
	last    Observation
}

// NewCheck starts a probation at now.
func NewCheck(now time.Time) *Check { return &Check{started: now} }

// Observe records o, taken at now, and returns the verdict so far, with the
// reason when it is Failed.
//
// Nothing is judged before Probation has passed. A pair that looks fine at
// minute two may still be on an island the check has not yet had the grace
// to name, and passing it early would record it as healthy for good.
func (c *Check) Observe(now time.Time, o Observation) (Verdict, string) {
	c.last = o
	c.synced = c.synced || !o.P2PoolExpected || o.P2PoolSynced
	c.allowed = c.allowed || o.MinerAllowed
	c.hashed = c.hashed || o.Hashing
	if now.Sub(c.started) < Probation {
		return Pending, ""
	}
	switch {
	case !c.synced:
		return Failed, fmt.Sprintf("p2pool did not sync within %s", Probation)
	case c.last.Island != "":
		return Failed, "p2pool is on an island: " + c.last.Island
	case c.allowed && !c.hashed:
		return Failed, fmt.Sprintf("xmrig did not hash in %s of being allowed to", Probation)
	}
	return Healthy, ""
}

// Engines is a pair on disk, as the .deb lays it out: xmrig, p2pool and
// engines.json side by side in one directory.
type Engines struct {
	Dir     string
	XMRig   string `json:"xmrig"`
	P2Pool  string `json:"p2pool"`
	Version string // "xmrig 6.26.0 · p2pool 4.18"
	// ID names this exact pair: the versions and the binaries' hashes. Two
	// builds of one version with different patches are different pairs.
	ID string
}

// manifest is engines.json, written by scripts/build-deb.sh.
type manifest struct {
	XMRig        string `json:"xmrig"`
	P2Pool       string `json:"p2pool"`
	XMRigSHA256  string `json:"xmrig_sha256"`
	P2PoolSHA256 string `json:"p2pool_sha256"`
}

// Load reads the pair in dir. A missing directory is (nil, nil): not every
// install is a .deb, and the others download their engines as before.
func Load(dir string) (*Engines, error) {
	data, err := os.ReadFile(filepath.Join(dir, "engines.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil || m.XMRig == "" || m.P2Pool == "" {
		return nil, fmt.Errorf("%s/engines.json is not an engine manifest", dir)
	}
	e := &Engines{
		Dir:     dir,
		XMRig:   filepath.Join(dir, "xmrig"),
		P2Pool:  filepath.Join(dir, "p2pool"),
		Version: fmt.Sprintf("xmrig %s · p2pool %s", m.XMRig, m.P2Pool),
	}
	sum := sha256.Sum256([]byte(m.XMRig + "\x00" + m.P2Pool + "\x00" + m.XMRigSHA256 + "\x00" + m.P2PoolSHA256))
	e.ID = hex.EncodeToString(sum[:8])
	for _, bin := range []string{e.XMRig, e.P2Pool} {
		if _, err := os.Stat(bin); err != nil {
			return nil, fmt.Errorf("engines in %s: %w", dir, err)
		}
	}
	return e, nil
}

// State is what the daemon remembers about pairs between runs.
type State struct {
	// Healthy is the ID of the last pair that passed probation.
	Healthy string `json:"healthy,omitempty"`
	// Rejected is the ID of a pair that failed it, and Reason why. It stays
	// rejected until a release brings a different pair.
	Rejected string    `json:"rejected,omitempty"`
	Reason   string    `json:"reason,omitempty"`
	At       time.Time `json:"at,omitempty"`
}

// ReadState reads the state at path; a missing file is the zero State.
func ReadState(path string) (State, error) {
	var st State
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return st, err
	}
	return st, json.Unmarshal(data, &st)
}

// WriteState writes st to path, through a rename so a crash never leaves
// half a file for the next start to misread.
func WriteState(path string, st State) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Choice is which pair to run, and what to say about it.
type Choice struct {
	Use *Engines
	// Probation: Use has never passed, and must now.
	Probation bool
	// RolledBack is set when Use is the previous pair because the current
	// one failed; it says so, with the reason — the amber the owner sees.
	RolledBack string
}

// Names are how to call two pairs in one sentence: by version, and by build
// as well when the versions are the same — a rebuild with a new patch is a
// different pair, and "6.26.0 failed, so back to 6.26.0" says nothing.
func Names(a, b *Engines) (string, string) {
	if a.Version != b.Version {
		return a.Version, b.Version
	}
	return a.Version + " (build " + a.ID[:8] + ")", b.Version + " (build " + b.ID[:8] + ")"
}

// CanRollBackTo reports whether previous is somewhere to go if current
// fails: a different pair, and one that passed here. The package keeps
// whatever pair it replaced, which after a rollback is the rejected one —
// going back to that would be no way back at all.
func CanRollBackTo(current, previous *Engines, st State) bool {
	return current != nil && previous != nil && previous.ID != current.ID && previous.ID == st.Healthy
}

// Choose picks between the pair the package installed and the one it
// replaced. current nil means this is not a packaged install: nothing to
// choose, engines are downloaded as ever.
func Choose(current, previous *Engines, st State) Choice {
	switch {
	case current == nil:
		return Choice{}
	case st.Rejected == current.ID && CanRollBackTo(current, previous, st):
		cur, prev := Names(current, previous)
		return Choice{Use: previous, RolledBack: fmt.Sprintf(
			"%s failed its health check (%s), so this machine went back to %s", cur, st.Reason, prev)}
	case st.Rejected == current.ID:
		// Nothing to go back to; running a doubtful pair beats running
		// none, and saying so is the most that can be done.
		return Choice{Use: current, RolledBack: fmt.Sprintf(
			"%s failed its health check (%s), and there is no earlier pair known to work to go back to", current.Version, st.Reason)}
	case st.Healthy == current.ID:
		return Choice{Use: current}
	}
	return Choice{Use: current, Probation: true}
}
