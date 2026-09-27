package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/kind-miner/kind-miner/internal/core"
	"github.com/kind-miner/kind-miner/internal/stats"
)

// The running daemon writes what it is doing to a small file in its runtime
// directory, and `status` reads it. No socket, no API, nothing to secure: the
// file says only what the owner of the box could already see, and a stale one
// is how `status` knows the daemon is gone.

// daemonStatus is the status file.
type daemonStatus struct {
	Version   string    `json:"version"`
	UpdatedAt time.Time `json:"updated_at"`
	State     string    `json:"state"`
	Reason    string    `json:"reason,omitempty"`
	// Hashrates in H/s; the long ones are absent until the ledger can vouch
	// for them.
	Hashrate    float64  `json:"hashrate"`
	Hashrate30m *float64 `json:"hashrate_30m,omitempty"`
	Hashrate24h *float64 `json:"hashrate_24h,omitempty"`
	Threads     int      `json:"threads"`
	Node        string   `json:"node,omitempty"`
	ViaTor      bool     `json:"via_tor"`
	UptimeSec   int64    `json:"uptime_seconds"`
	SharesFound uint64   `json:"shares_found"`
	Payouts     int      `json:"payouts_seen"`
}

// statusPath is the status file: systemd's RuntimeDirectory= when running as
// the service, the user's runtime directory otherwise.
func statusPath() string {
	if d := os.Getenv("RUNTIME_DIRECTORY"); d != "" {
		return filepath.Join(d, "status.json")
	}
	if d := os.Getenv("XDG_RUNTIME_DIR"); d != "" {
		return filepath.Join(d, "kind-miner", "status.json")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("kind-miner-%d", os.Getuid()), "status.json")
}

func snapshot(sup *core.Supervisor, now time.Time) daemonStatus {
	st := daemonStatus{Version: version, UpdatedAt: now}
	if up, ok := sup.Uptime(); ok {
		st.UptimeSec = int64(up / time.Second)
	}
	st.Node, st.ViaTor = sup.Node()
	st.Payouts = len(sup.Payouts().Recent(1 << 10))
	if s := sup.Scheduler(); s != nil {
		state, reason := s.CurrentState()
		st.State, st.Reason = state.String(), reason
		active, _ := s.ActiveThreads()
		st.Threads = active
		st.Hashrate, _ = stats.Mean(s.History(), time.Minute, stats.Hashrate)
		if r, ok := s.Ledger().Rate(30*time.Minute, now); ok {
			st.Hashrate30m = &r
		}
		if r, ok := s.Ledger().Rate(24*time.Hour, now); ok {
			st.Hashrate24h = &r
		}
	}
	if p := sup.P2Pool(); p != nil {
		if ps, ok := p.Stats(); ok {
			st.SharesFound = ps.SharesFound
		}
	}
	return st
}

func writeStatus(path string, st daemonStatus) error {
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

func writeStatusEvery(sup *core.Supervisor, path string, every time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		if err := writeStatus(path, snapshot(sup, time.Now())); err != nil {
			log.Printf("status: %v", err)
		}
		select {
		case <-stop:
			os.Remove(path) // a stopped daemon leaves no status behind
			return
		case <-t.C:
		}
	}
}

// readStatus returns the daemon's status, and whether it is running: the
// file exists and is fresh.
func readStatus(path string, now time.Time) (daemonStatus, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return daemonStatus{}, false, nil
	}
	if err != nil {
		return daemonStatus{}, false, err
	}
	var st daemonStatus
	if err := json.Unmarshal(data, &st); err != nil {
		return daemonStatus{}, false, err
	}
	return st, now.Sub(st.UpdatedAt) <= staleAfter, nil
}

// runStatus prints the status and returns the exit code: 0 running, 3 not
// (the LSB code for "not running", which scripts can test for).
func runStatus(asJSON bool, w io.Writer) int {
	st, running, err := readStatus(statusPath(), time.Now())
	if err != nil {
		fmt.Fprintf(w, "cannot read status: %v\n", err)
		return 1
	}
	if !running {
		fmt.Fprintln(w, "kind-minerd is not running")
		return 3
	}
	if asJSON {
		data, _ := json.MarshalIndent(st, "", "  ")
		fmt.Fprintln(w, string(data))
		return 0
	}
	fmt.Fprint(w, formatStatus(st))
	return 0
}

// formatStatus is the human summary.
func formatStatus(st daemonStatus) string {
	line := st.State
	if st.Reason != "" {
		line += " — " + st.Reason
	}
	out := fmt.Sprintf("kind-minerd %s: %s\n", st.Version, line)
	rate := func(p *float64) string {
		if p == nil {
			return "—"
		}
		return fmt.Sprintf("%.0f H/s", *p)
	}
	out += fmt.Sprintf("  hashrate  %.0f H/s now · %s 30 min · %s 24 h on %d threads\n",
		st.Hashrate, rate(st.Hashrate30m), rate(st.Hashrate24h), st.Threads)
	if st.Node != "" {
		via := "direct"
		if st.ViaTor {
			via = "over Tor"
		}
		out += fmt.Sprintf("  node      %s (%s)\n", st.Node, via)
	}
	out += fmt.Sprintf("  shares    %d found · %d payouts seen\n", st.SharesFound, st.Payouts)
	out += fmt.Sprintf("  uptime    %s\n", time.Duration(st.UptimeSec)*time.Second)
	return out
}
