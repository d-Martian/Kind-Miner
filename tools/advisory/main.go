// Command advisory checks the advisory manifest before it is signed, and
// verifies a signed one against the key kind-miner embeds.
//
//	go run ./tools/advisory check  advisory/manifest.json [previous serial]
//	go run ./tools/advisory verify advisory/manifest.json
//
// check refuses what every client would refuse — a missing serial, a
// lifetime past advisory.MaxLifetime — and a serial no higher than the one
// already published, which clients would treat as a replay. verify is the
// clients' own check, run before anything is published.
package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/kind-miner/kind-miner/internal/advisory"
)

func main() {
	if len(os.Args) < 3 {
		fmt.Fprintln(os.Stderr, "usage: advisory check FILE [previous-serial] | verify FILE")
		os.Exit(2)
	}
	data, err := os.ReadFile(os.Args[2])
	if err != nil {
		fail(err)
	}
	switch os.Args[1] {
	case "check":
		m, err := advisory.Parse(data)
		if err != nil {
			fail(err)
		}
		if len(os.Args) > 3 {
			prev, err := strconv.ParseInt(os.Args[3], 10, 64)
			if err != nil {
				fail(fmt.Errorf("previous serial %q: %w", os.Args[3], err))
			}
			if m.Serial <= prev {
				fail(fmt.Errorf("serial %d is not above the published %d; clients would refuse it as a replay", m.Serial, prev))
			}
		}
		if !time.Now().Before(m.Expires) {
			fail(fmt.Errorf("it has already expired (%s)", m.Expires.Format(time.RFC3339)))
		}
		fmt.Printf("serial %d, good until %s\n", m.Serial, m.Expires.Format("2 Jan 2006"))
	case "verify":
		pk, ok := advisory.Key()
		if !ok {
			fail(fmt.Errorf("internal/advisory/minisign.pub is empty: run scripts/make-advisory-key.sh"))
		}
		sig, err := os.ReadFile(os.Args[2] + ".minisig")
		if err != nil {
			fail(err)
		}
		m, err := advisory.Verified(pk, data, string(sig), 0)
		if err != nil {
			fail(err)
		}
		fmt.Printf("serial %d verifies against the embedded key\n", m.Serial)
	default:
		fail(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}

func fail(err error) {
	fmt.Fprintf(os.Stderr, "advisory: %v\n", err)
	os.Exit(1)
}
