package engine

import (
	"regexp"
	"strconv"
)

// p2pool announces a payout only in its log. It is the most fragile interface
// kind-miner has: a line of prose, not an API, and nothing stops a p2pool
// release rewording it. So the format is pinned twice over — the expression
// below, and golden files under testdata/p2pool/<version>/ that the tests
// require to exist for the p2pool version deps.json pins. Bumping p2pool
// without checking this line fails the build.
//
// The wording, as of p2pool v4.18 (p2pool.cpp and side_chain.cpp, the same in
// both, unchanged since at least v3.10):
//
//	Your wallet <address> got a payout of <W>.<FFFFFFFFFFFF> XMR in block <height>
//
// Not "You received a payout of … XMR in block …", the form other tools match:
// no p2pool release kind-miner could ship prints that.
var payoutLine = regexp.MustCompile(`got a payout of (\d+)\.(\d{12}) XMR in block (\d+)`)

// ansi matches colour escapes. kind-miner runs p2pool with --no-color, but a
// payout missed because that flag was ever dropped would be missed silently.
var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// PayoutFromLog reads a payout from one line of p2pool's output: the block
// height and the amount in atomic units. The amount is parsed as integers —
// p2pool prints exactly 12 fractional digits, one per piconero — so nothing is
// lost to floating point on the way into the ledger.
func PayoutFromLog(line string) (height, atomic uint64, ok bool) {
	m := payoutLine.FindStringSubmatch(ansi.ReplaceAllString(line, ""))
	if m == nil {
		return 0, 0, false
	}
	whole, err1 := strconv.ParseUint(m[1], 10, 64)
	frac, err2 := strconv.ParseUint(m[2], 10, 64)
	height, err3 := strconv.ParseUint(m[3], 10, 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0, 0, false
	}
	return height, whole*atomicPerXMR + frac, true
}

// atomicPerXMR is one XMR in piconero.
const atomicPerXMR = 1_000_000_000_000
