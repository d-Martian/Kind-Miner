package hub

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// Worker is one device connected to the hub's stratum port.
type Worker struct {
	// Name is the xmrig user field the device logged in with — its label.
	// Empty for a device that has connected but not logged in yet.
	Name      string        `json:"name"`
	Addr      string        `json:"addr"`
	Connected time.Duration `json:"connected_ns"`
	// Hashrate is p2pool's estimate from the difficulty it settled the
	// device on, in H/s. It lags a change in pace by a minute or so.
	Hashrate uint64 `json:"hashrate"`
}

// notLoggedIn is what p2pool writes in place of the user for a connection
// that has not sent its login yet.
const notLoggedIn = "not logged in"

// ParseWorker reads one entry of the "workers" array in p2pool's
// local/stratum file: "addr,connected_seconds,difficulty,hashrate,user"
// (p2pool 4.18, StratumServer::api_update_local_stats). The user comes last
// and p2pool strips commas out of it, so splitting on the first four commas
// is exact.
func ParseWorker(s string) (Worker, bool) {
	f := strings.SplitN(s, ",", 5)
	if len(f) != 5 {
		return Worker{}, false
	}
	secs, err1 := strconv.ParseUint(f[1], 10, 64)
	rate, err2 := strconv.ParseUint(f[3], 10, 64)
	if err1 != nil || err2 != nil {
		return Worker{}, false
	}
	w := Worker{Addr: f[0], Connected: time.Duration(secs) * time.Second, Hashrate: rate, Name: f[4]}
	if w.Name == notLoggedIn {
		w.Name = ""
	}
	return w, true
}

// ParseWorkers reads every entry, skipping any it cannot.
func ParseWorkers(entries []string) []Worker {
	out := make([]Worker, 0, len(entries))
	for _, e := range entries {
		if w, ok := ParseWorker(e); ok {
			out = append(out, w)
		}
	}
	return out
}

// maxWorkerName is one less than p2pool's CUSTOM_USER_SIZE (32): it keeps
// that many characters of a login and silently drops the rest.
const maxWorkerName = 31

// WorkerName turns a device name into a login p2pool will show intact.
// p2pool reads the user field up to the first '.' or '+' (what follows sets a
// fixed difficulty) and drops commas, quotes and backslashes; anything like
// that becomes a hyphen here instead, so "Dad's laptop.local" is labelled
// "Dad-s-laptop" rather than "Dads laptop" or an empty name.
func WorkerName(s string) string {
	var b strings.Builder
	for _, c := range strings.TrimSpace(s) {
		if b.Len() == maxWorkerName {
			break
		}
		switch {
		case c == '.':
			// A hostname's domain is not part of the device's name.
			return strings.Trim(b.String(), "-")
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteRune(c)
		default:
			b.WriteRune('-')
		}
	}
	return strings.Trim(b.String(), "-")
}

// devicePool is one entry of xmrig's "pools" array, as the plan for a device
// running plain xmrig without kind-miner writes it.
type devicePool struct {
	URL            string `json:"url"`
	User           string `json:"user"`
	TLS            bool   `json:"tls"`
	TLSFingerprint string `json:"tls-fingerprint"`
	Keepalive      bool   `json:"keepalive"`
}

// DeviceConfig is the "pools" block for a device that runs bare xmrig — a
// server in the garage with no desktop to run kind-miner on. Its user field
// is the name the device shows up under in kind-minerd status.
func DeviceConfig(p Pairing, name string) []byte {
	data, _ := json.MarshalIndent(map[string][]devicePool{"pools": {{
		URL:            p.StratumAddr(),
		User:           WorkerName(name),
		TLS:            true,
		TLSFingerprint: p.FingerprintHex(),
		Keepalive:      true,
	}}}, "", "  ")
	return data
}
