# Update miner runtime dependencies

## Summary

- XMRig:
- P2Pool:

## Upstream Review

- Release notes:
- Security-sensitive changes:
- Compatibility-sensitive changes:

## Reproducibility

- Asset filenames and SHA256 hashes are pinned.
- Upstream checksums/signatures reviewed:
- Local reproducibility checks:

## Validation

```sh
go test ./...
make verify-repro
bash scripts/download-xmrig.sh
bash scripts/download-p2pool.sh
```

## Follow-Up

- Runtime autoinstall behavior:
- Manual checks:

