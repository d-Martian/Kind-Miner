// Package advisory reads the signed advisory manifest: a small file of facts
// — the oldest engine versions still safe to run, upcoming fork heights, the
// default node list — that kind-miner fetches once a day over Tor.
//
// It carries facts, never code, which bounds what a leaked signing key can
// do: it can stop mining, not run anything. And it stops mining only on
// positive knowledge — an engine whose version is known to be below the
// floor, or a fork height that has passed without the version it needs.
// Anything unknown changes nothing, and failing to fetch never stops mining:
// a manifest that cannot be had is not news that something is wrong.
//
// Two limits keep a bad manifest from doing lasting harm. Each lives at most
// MaxLifetime, after which it is ignored; and its serial only goes up, so an
// old manifest — say, a floor since lowered — cannot be replayed.
package advisory

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Manifest is the advisory.
type Manifest struct {
	// Serial increases with every manifest; a lower one than already seen
	// is refused.
	Serial  int64     `json:"serial"`
	Issued  time.Time `json:"issued"`
	Expires time.Time `json:"expires"`
	// MinVersions is the oldest safe version of each engine, by name
	// ("xmrig", "p2pool").
	MinVersions map[string]string `json:"min_versions,omitempty"`
	Forks       []Fork            `json:"forks,omitempty"`
	// Nodes replaces the built-in default node list, when not empty.
	Nodes []Node `json:"nodes,omitempty"`
	// Notice is one sentence to show the user, if there is anything to say.
	Notice string `json:"notice,omitempty"`
}

// Fork is a height from which an engine needs a version.
type Fork struct {
	Name string `json:"name"`
	// Chain is "monero" (the main chain's height) or "sidechain" (p2pool's).
	Chain    string            `json:"chain"`
	Height   uint64            `json:"height"`
	Requires map[string]string `json:"requires"`
}

// Node is a default monerod, as the node list wants it.
type Node struct {
	Host    string `json:"host"`
	RPCPort int    `json:"rpc_port"`
	ZMQPort int    `json:"zmq_port"`
}

// MaxLifetime is the longest a manifest may claim to be good for. A leaked
// key's worst manifest stops mining for at most this long after the last one
// it can be served in place of — and a real one with a higher serial ends it
// sooner.
const MaxLifetime = 60 * 24 * time.Hour

// Parse reads and checks a manifest. It does not check the signature; see
// Verified.
func Parse(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("the advisory is not valid JSON: %w", err)
	}
	switch {
	case m.Serial <= 0:
		return Manifest{}, errors.New("the advisory has no serial")
	case m.Issued.IsZero() || m.Expires.IsZero() || !m.Expires.After(m.Issued):
		return Manifest{}, errors.New("the advisory's dates are missing or backwards")
	case m.Expires.Sub(m.Issued) > MaxLifetime:
		return Manifest{}, fmt.Errorf("the advisory claims to be good for more than %d days", int(MaxLifetime.Hours()/24))
	}
	for name, v := range m.MinVersions {
		if _, ok := parseVersion(v); !ok {
			return Manifest{}, fmt.Errorf("the advisory's minimum %s version %q is not a version", name, v)
		}
	}
	for _, f := range m.Forks {
		if f.Chain != "monero" && f.Chain != "sidechain" || f.Height == 0 || len(f.Requires) == 0 {
			return Manifest{}, fmt.Errorf("the advisory's fork %q is incomplete", f.Name)
		}
		for name, v := range f.Requires {
			if _, ok := parseVersion(v); !ok {
				return Manifest{}, fmt.Errorf("the advisory's fork %q requires %s %q, which is not a version", f.Name, name, v)
			}
		}
	}
	for _, n := range m.Nodes {
		if n.Host == "" || n.RPCPort <= 0 || n.RPCPort > 65535 || n.ZMQPort <= 0 || n.ZMQPort > 65535 {
			return Manifest{}, fmt.Errorf("the advisory's node %q is incomplete", n.Host)
		}
	}
	return m, nil
}

//go:embed minisign.pub
var embeddedKey string

// Key is the public key advisories are checked against. ok is false while
// none has been made (the file is empty), and then no advisory is trusted:
// there is nothing to verify one with.
func Key() (PublicKey, bool) {
	if strings.TrimSpace(embeddedKey) == "" {
		return PublicKey{}, false
	}
	pk, err := ParsePublicKey(embeddedKey)
	return pk, err == nil
}

// Verified parses data after checking sig against pk, and refuses a serial
// below floor — the highest already accepted — so an older manifest cannot
// be served in place of a newer one.
func Verified(pk PublicKey, data []byte, sig string, floor int64) (Manifest, error) {
	if _, err := pk.Verify(data, sig); err != nil {
		return Manifest{}, err
	}
	m, err := Parse(data)
	if err != nil {
		return Manifest{}, err
	}
	if m.Serial < floor {
		return Manifest{}, fmt.Errorf("the advisory's serial %d is older than %d, already seen", m.Serial, floor)
	}
	return m, nil
}

// Running is what is known about the engines in use: the version of each,
// by name, where known. A user's own build has no known version, and is
// then never stopped on the advisory's say-so.
type Running map[string]string

// Heights are the chains' current heights, zero when not known.
type Heights struct {
	Monero, Sidechain uint64
}

// Verdict is what an advisory means for this machine now.
type Verdict struct {
	// Stop is why mining must stop, if it must: an amber "Update needed".
	Stop string
	// Warnings are worth saying and change nothing.
	Warnings []string
}

// Evaluate applies m to the engines running at now. It is the whole policy,
// and stops mining only when it knows: an unknown version or height never
// does.
func Evaluate(m Manifest, now time.Time, run Running, h Heights) Verdict {
	var v Verdict
	if !now.Before(m.Expires) {
		v.Warnings = append(v.Warnings, fmt.Sprintf("the advisory expired on %s", m.Expires.Format("2 Jan 2006")))
		return v
	}
	if m.Notice != "" {
		v.Warnings = append(v.Warnings, m.Notice)
	}
	for _, name := range sortedKeys(m.MinVersions) {
		have, ok := run[name]
		if !ok || have == "" {
			continue
		}
		if below(have, m.MinVersions[name]) && v.Stop == "" {
			v.Stop = fmt.Sprintf("Update needed: %s %s is below the safe minimum, %s", name, have, m.MinVersions[name])
		}
	}
	for _, f := range m.Forks {
		height := h.Monero
		if f.Chain == "sidechain" {
			height = h.Sidechain
		}
		for _, name := range sortedKeys(f.Requires) {
			have, ok := run[name]
			if !ok || have == "" || !below(have, f.Requires[name]) {
				continue
			}
			switch {
			case height == 0:
				// Not knowing the height is not knowing it passed.
				v.Warnings = append(v.Warnings, fmt.Sprintf("%s needs %s %s from %s height %d", f.Name, name, f.Requires[name], f.Chain, f.Height))
			case height >= f.Height && v.Stop == "":
				v.Stop = fmt.Sprintf("Update needed: %s needs %s %s from %s height %d, which has passed", f.Name, name, f.Requires[name], f.Chain, f.Height)
			case height < f.Height:
				v.Warnings = append(v.Warnings, fmt.Sprintf("update before %s height %d: %s needs %s %s", f.Chain, f.Height, f.Name, name, f.Requires[name]))
			}
		}
	}
	return v
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// below reports whether version have is older than want. Versions are dotted
// numbers ("6.26.0", "4.18", "v4.18"); a version that does not parse is
// never below anything — not knowing is not knowing it is old.
func below(have, want string) bool {
	h, ok1 := parseVersion(have)
	w, ok2 := parseVersion(want)
	if !ok1 || !ok2 {
		return false
	}
	for i := 0; i < len(h) || i < len(w); i++ {
		var a, b int
		if i < len(h) {
			a = h[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			return a < b
		}
	}
	return false
}

func parseVersion(s string) ([]int, bool) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if s == "" {
		return nil, false
	}
	parts := strings.Split(s, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}
