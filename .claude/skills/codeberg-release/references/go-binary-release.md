# Go Binary Releases

Use this reference when releasing Go binaries.

## Version Source

Prefer the project's existing version mechanism. Common patterns:

- `-ldflags "-X main.version=$VERSION"`
- package variable such as `internal/version.Version`
- generated file from `go generate`
- `git describe --tags --dirty`

For release builds, avoid embedding dirty working-tree state unless the project intentionally marks non-release builds that way.

## Reproducible Build Defaults

Good baseline flags:

```sh
go build -trimpath -buildvcs=false -ldflags "-s -w -buildid= -X main.version=$VERSION"
```

Use the project's own flags if they differ. Keep flags in sync across:

- `Makefile`,
- scripts,
- CI,
- release docs.

Set `SOURCE_DATE_EPOCH` from the release commit time when packaging or generators use timestamps:

```sh
SOURCE_DATE_EPOCH="$(git log -1 --format=%ct)"
export SOURCE_DATE_EPOCH
```

## Reproducibility Check

Minimum local check:

1. Build a binary in directory A.
2. Build the same binary in directory B with the same environment.
3. Compare SHA256 hashes.

This proves deterministic output on the current machine. It does not prove cross-host reproducibility when cgo, native SDKs, or system headers are involved.

If the project has a reproducibility target such as `make verify-repro`, run that instead of inventing a parallel proof.

## Cross Platform Targets

Pure Go projects can often cross-compile with:

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ...
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build ...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build ...
```

Projects using cgo, GUI toolkits, OpenGL, system trays, platform SDKs, or native libraries may require native runners per OS/architecture. Do not pretend a cross-compiled cgo release is supported unless it has actually built and run.

## Packaging

Use versioned names:

```text
app-v1.2.3-linux-amd64.tar.gz
app-v1.2.3-darwin-arm64.tar.gz
app-v1.2.3-windows-amd64.tar.gz
```

Include only expected files:

- binary,
- license,
- README or config example if useful,
- completion files or man pages if the project ships them.

Avoid:

- build logs,
- caches,
- private config,
- vendored source unless intentionally shipped.

## When To Use The Helper

Use `scripts/prepare_go_release.sh` for simple Go projects. For projects with custom packaging, cgo, AppImage, Flatpak, notarization, Windows signing, or platform-specific assets, read the existing release scripts and adapt the workflow instead.

