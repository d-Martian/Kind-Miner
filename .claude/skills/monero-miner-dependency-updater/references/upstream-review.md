# Upstream Review

Use this reference when reviewing XMRig and P2Pool upstream releases before changing this project.

## Release Discovery

Use GitHub release metadata for current public upstreams:

- XMRig: `xmrig/xmrig`
- P2Pool: `SChernykh/p2pool`

Prefer the GitHub REST release API over scraping HTML:

- latest release: `/repos/{owner}/{repo}/releases/latest`
- compare range: `/repos/{owner}/{repo}/compare/BASE...HEAD`
- release assets: `/repos/{owner}/{repo}/releases/{id}/assets`

If the API is rate-limited, set `GITHUB_TOKEN` for read-only API access.

## Security Review

Look for changes affecting:

- wallet address handling,
- mining donation/devfee behavior,
- default pool or node endpoints,
- proxy/Tor/I2P behavior,
- TLS/certificate handling,
- executable extraction and archive layout,
- command-line flags used by this project,
- network listeners and bind addresses,
- RPC/API exposure,
- dependency bumps in upstream projects,
- bundled cryptography libraries,
- logs that may reveal wallet addresses, peers, local network details, or Tor routes.

Block the update if there are unexplained changes to donation behavior, wallet handling, download provenance, or default network endpoints.

## XMRig Compatibility Focus

Check:

- XMRig binary asset names for supported platforms.
- Whether the project still extracts `xmrig` or `xmrig.exe` from the archive path.
- Stratum URL behavior for local P2Pool and traditional pool mode.
- Thread configuration flags and API behavior used by `internal/engine/xmrig.go`.
- CPU feature detection changes that affect RandomX or low-power systems.
- Default config changes that might conflict with the app's generated arguments.
- Upstream SHA256SUMS and signatures.

Be suspicious of changes that silently alter donation rounds, reset nonces, pool failover, proxy handling, or background API exposure.

## P2Pool Compatibility Focus

Check:

- P2Pool binary asset names for supported platforms.
- Required Monero daemon version and ZMQ/RPC parameters.
- Stratum port behavior and local listener defaults.
- `--mini` or sidechain selection behavior.
- Tor/I2P-related peer ID, DNS, or proxy behavior.
- Peer connection limits and DoS hardening that might affect local operation.
- Wallet address command-line handling.
- Upstream `sha256sums.txt` and `sha256sums.txt.asc`.

P2Pool releases can include security hardening for spam/DoS resistance and fake chain defense. Treat security releases as high priority, but still require asset/hash verification and local compatibility tests.

## Review Output

The PR body or report should include:

- current version and latest version,
- release links,
- notable security-sensitive changes,
- compatibility-sensitive changes,
- asset/hash table,
- tests run,
- reproducibility checks run,
- open questions or reasons the update was blocked.

