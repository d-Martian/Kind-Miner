# p2pool log golden files

kind-miner reads payouts from p2pool's log, which is prose, not an API. Each
directory here is named for a p2pool version and holds:

- `stdout.log` — lines as that version prints them to stdout: payouts from both
  places p2pool logs them (`P2Pool` and `SideChain`, the same block twice), the
  "didn't get a payout" line, a colour-coded line, and noise that must not match.
- `payouts.golden.json` — what the parser must read from it, in order.

`TestPayoutGoldenFiles` fails when the p2pool version pinned in
`internal/autoinstall/deps.json` has no directory here, so a p2pool bump cannot
land without someone checking this format again. For a bump: read the new
tag's `src/p2pool.cpp` and `src/side_chain.cpp` for the payout line, copy the
previous directory to the new version, adjust `stdout.log` to what the new
version prints, and run

    go test ./internal/engine -run TestPayoutGoldenFiles -update

then review the diff of `payouts.golden.json` by hand.

The 4.18 lines were written from the source (p2pool.cpp:627,
side_chain.cpp:689, the `XMRAmount` formatter in log.h), not captured from a
live payout.
