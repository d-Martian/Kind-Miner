package engine

import (
	"bufio"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the payout golden files from the parser's output")

type goldenPayout struct {
	Height uint64 `json:"height"`
	Atomic uint64 `json:"atomic"`
}

// TestPayoutGoldenFiles parses every version's recorded p2pool output and
// compares it with what that version's golden file says it contains.
func TestPayoutGoldenFiles(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join("testdata", "p2pool", "*"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range dirs {
		if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
			continue
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			f, err := os.Open(filepath.Join(dir, "stdout.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			got := []goldenPayout{}
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if h, a, ok := PayoutFromLog(sc.Text()); ok {
					got = append(got, goldenPayout{h, a})
				}
			}
			golden := filepath.Join(dir, "payouts.golden.json")
			if *update {
				data, _ := json.MarshalIndent(got, "", "  ")
				if err := os.WriteFile(golden, append(data, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			data, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			var want []goldenPayout
			if err := json.Unmarshal(data, &want); err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("parsed %d payouts, golden has %d:\n got  %v\n want %v", len(got), len(want), got, want)
			}
			for i := range want {
				if got[i] != want[i] {
					t.Errorf("payout %d = %+v, want %+v", i, got[i], want[i])
				}
			}
		})
	}
}

// TestPinnedP2PoolHasGoldenFiles is the tripwire: moving the p2pool pin
// without re-checking the payout line fails here.
func TestPinnedP2PoolHasGoldenFiles(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "autoinstall", "deps.json"))
	if err != nil {
		t.Fatal(err)
	}
	var deps struct {
		P2Pool struct {
			Version string `json:"version"`
		} `json:"p2pool"`
	}
	if err := json.Unmarshal(data, &deps); err != nil || deps.P2Pool.Version == "" {
		t.Fatalf("cannot read the p2pool pin: %v", err)
	}
	dir := filepath.Join("testdata", "p2pool", deps.P2Pool.Version)
	if _, err := os.Stat(filepath.Join(dir, "payouts.golden.json")); err != nil {
		t.Errorf("p2pool %s is pinned but %s has no payout golden files: check the new version's "+
			"payout log line and add them (see testdata/p2pool/README.md)", deps.P2Pool.Version, dir)
	}
}

func TestPayoutFromLogRejects(t *testing.T) {
	for _, line := range []string{
		"",
		"Your wallet 4AB didn't get a payout in block 3412389 because you had no shares in PPLNS window",
		"got a payout of 0.00081 XMR in block 3412341", // too few digits: not p2pool's formatter
		"got a payout of 0.000812345678 XMR in block ",
	} {
		if h, a, ok := PayoutFromLog(line); ok {
			t.Errorf("%q parsed as block %d, %d atomic", line, h, a)
		}
	}
}
