# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

kind-miner is a single-binary Monero miner (Go + Fyne GUI) that mines on P2Pool
over Tor using only the CPU the user isn't using. The product promise is
*restraint*: the miner must be unnoticeable, and must be visibly seen to get out
of the way. Most of the non-obvious design below exists to serve that promise.

## Commands

```sh
go build ./...                              # compile everything
make build                                  # the real binary, with release flags
go test ./...                               # full suite; no display needed
go test ./internal/scheduler -run TestDecide # one test
go test ./internal/gui -run TestScreensRender -v

make reproduce        # canonical reproducible build (pinned stock toolchain)
make verify-repro     # build twice locally, compare SHA256 — fast determinism check
make vendor           # after any dependency change; vendor/ is committed
```

The GUI uses cgo (Fyne/GLFW/OpenGL), so the build host needs GL/X11/Wayland dev
headers — see README "Building from source" for the per-distro package list.
Go 1.25+.

Regenerating the AppStream/README screenshots (a capture utility, skipped in
normal runs — it renders through Fyne's software test driver, so no display or
supervisor is involved). Run from the repo root; the path must be absolute:

```sh
KM_SHOT_DIR="$PWD/assets/screenshots" go test -mod=mod ./internal/gui -run TestCaptureScreenshots
```

There is no linter configured and no lint step in CI.

## Architecture

Layering is strict and worth preserving: **presentation never talks to
subprocesses.**

```
cmd/kind-miner  → decides UI mode (GUI / tray / headless), owns signals
  internal/core → Supervisor: owns the whole mining stack for one config
    internal/autoinstall → downloads + SHA256-verifies xmrig, p2pool, tor
    internal/engine      → XMRig, P2Pool, Monerod, Tor subprocesses
    internal/scheduler   → the 2s control loop that decides how much CPU mining gets
      internal/monitor   → CPU / battery / temp / idle / session-lock / power samplers
      internal/kindness  → the presets (ceiling + fall/rise rates)
      internal/stats     → rolling history ring, feeds the chart
  internal/gui  → Fyne window, dashboard, settings, tray (reads via Supervisor)
```

`core.Supervisor` is the seam that makes three front-ends possible: it reports
startup progress through a callback and **returns errors instead of exiting**,
so the GUI can show a progress screen and an error dialog where the terminal
build prints and dies. Anything that calls `os.Exit` or `log.Fatal` below
`cmd/` breaks this.

### The control loop (internal/scheduler)

Every 2s: sample the machine, compute a *continuous* CPU allowance, hand it to
the miner as a duty fraction.

```
target  = kindness ceiling − CPU used by everything except the miner
allowed = allowed + (target − allowed) × (falling ? Fall : Rise)
duty    = allowed ÷ (miner's full-tilt share of the machine)
```

Three things here are load-bearing and easy to undo by accident:

- **Fall ≫ Rise.** Symmetric smoothing oscillates against bursty desktop load
  and the user feels every swing. Fast down, slow up is the entire trick.
- **Hard vs soft zero.** A hard stop (manual pause, battery, temperature limit)
  zeroes the allowance instantly. A *soft* zero — a transient spike leaving no
  headroom — only pulls it down at the fall rate. Without that split, one
  background task every few seconds resets the allowance before it can ever
  accumulate and the miner idles forever on a 95%-free machine. This was a real
  observed failure, not a hypothetical.
- **The idle gate.** Until the keyboard/mouse have been quiet for
  `idle_full_after_seconds`, the ceiling is clamped to Ghost's regardless of
  preset. CPU load alone can't tell "reading a page" from "away".

### Throttling is duty-cycling, never re-threading

`XMRig.SetDuty` suspends/resumes the process (SIGSTOP/SIGCONT) on a 500ms
period. RandomX spends ~3s allocating its dataset on every launch, so anything
that restarts xmrig to throttle it re-initialises forever and never hashes.
`SetThreads` exists but restarts the process and is reserved for a config
change; the scheduler's `miner` interface deliberately **omits it** so this
can't regress.

### Testability pattern

Policy is extracted into pure functions — `decide`, `approach`, `thermalFactor`,
`dutyFor`, `stateForDuty`, `decideMode` — and dependencies are narrow interfaces
(`miner`, `idleSource`, `idleBackend`, `lockBackend`). Tests are table-driven
with prose case names describing the *behaviour*, and never spawn a subprocess,
touch a clock, or need a desktop. Keep new policy in that shape.

### Fyne 2.5.3 constraints (internal/gui)

The pinned version shapes a lot of this code; don't "clean it up" without
bumping Fyne:

- No `fyne.Do`, so background goroutines mutate widgets and call `Refresh`
  directly. A bump to 2.6 should route these through `fyne.Do`.
- No chart widget and no path API — the dashboard plot is rasterised with
  `x/image/vector` into a `canvas.Raster` (`chart.go`, `chartdraw.go`).
- No card/tile/pill widgets — composed by hand in `widgets.go`.
- A single tray menu item cannot be refreshed; changing one label means
  re-applying the whole menu, which closes it if open. `app.go` guards this with
  `lastMenuKey` (re-apply only when the rendered text actually changed).
- `setScreen` re-asserts window size on every screen swap, because Fyne shrinks
  the window to content minimum on `SetContent` and never grows it back.

User-facing prose lives in `copy.go`; display formatting in `format.go`. The
house rule in `format.go`: never show more precision than the number deserves,
and show `—` rather than a zero for "unknown" (a modelled figure next to a
measured one is indistinguishable from a real measurement).

### Platform code

Per-OS files with build tags, in one of two shapes: a four-way split
(`*_linux.go` / `*_darwin.go` / `*_windows.go` / `*_other.go`, the last guarded
by `//go:build !linux && !darwin && !windows`) where each OS differs, or a
`*_unix.go` / `*_windows.go` pair where they don't. Either way every capability
has a fallback for platforms it isn't implemented on, and monitors must degrade
rather than fail: a sampler that cannot answer returns the value that changes
nothing, and where a fact is genuinely unknowable the API returns a `known`
bool (`Session.Locked`, `Power.Watts`) so callers can't read "not locked" or
"0 W" as a fact.

## Invariants that span files

- **Build flags in three places** must stay in sync: `Makefile`
  (`GO_BUILD_FLAGS`), `scripts/reproduce.sh`, and
  `.github/workflows/release.yml`. Archive flags have one home:
  `scripts/package.sh`. See REPRODUCIBLE.md.
- **Dependency pins in three places**: `internal/autoinstall/deps.json` (source
  of truth, embedded and verified at runtime), `scripts/download-xmrig.sh`,
  `scripts/download-p2pool.sh`. Runtime never follows `/releases/latest` —
  moving a version requires editing `deps.json`. XMRig has no linux-arm64
  prebuilt, so its absence is intentional.
- **`kindness.Level` strings are the on-disk config format.** Renaming one needs
  an entry in `aliases` (see `greedy` → `full`) so old configs migrate.
  `internal/kindness` is also the source of truth for UI ordering and labels —
  add a preset there, not in the GUI.
- **Config migration is one-way and silent**: `Load` applies it (e.g.
  `throttle_sensitivity` → `kindness`) and `Save` drops the dead key via
  `omitempty`. `Load` also has to distinguish "key absent" from "key set to the
  default", which is why `Kindness` is blanked before unmarshalling.
- Changing config keys touches `config.example.yaml` and the README config
  block; the settings window groups the same keys and must say when a change
  needs a restart (wallet, mode, node, sidechain) versus taking effect live
  (kindness, chart, pause conditions — via `Scheduler.UpdateRuntime`).
- `p2poolapi.go` parses p2pool's JSON stat files; field names are tied to the
  pinned p2pool version and need re-checking when that pin moves.

## Conventions

Comments in this codebase explain *why*, often at paragraph length, and
frequently record the failure that motivated the design. That density is
deliberate — match it when touching policy code, and skip it for plumbing.

Commits are sentence-case and imperative ("Wait for the user to go idle before
mining at full speed"), no conventional-commits prefixes. Work lands on
`feat/*` branches via PR to `main`.

## Project skills

`.claude/skills/` carries four workflows specific to this repo:
`monero-miner-dependency-updater` (XMRig/P2Pool version bumps — it knows the
three pin sites), `reproducible-builds-review`, `codeberg-cicd`, and
`codeberg-release`. Prefer them over ad-hoc edits for those tasks. Note the
project migrated hosting from Codeberg to GitHub, so the Codeberg skills
describe a platform this repo no longer primarily uses.
