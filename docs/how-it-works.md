# How kind-miner works

The detail behind the short feature list in the [README](../README.md).

## Features in detail

- **Kindness, not thresholds.** One control decides how much of the machine mining may take and how fast it lets go — Ghost, Polite, Balanced, or Full. Change it from the tray without stopping anything.
- **Disappears under load.** Every 2 seconds the scheduler measures CPU usage excluding the miner's own threads, and gives the miner whatever is left under the kindness ceiling. If you start compiling, gaming, or rendering, XMRig gets out of the way within one tick.
- **Shows you it is doing so.** The dashboard plots the miner's CPU against everything else's, with the reserved headroom shaded and each backoff marked. You can watch the miner step aside.
- **Uses the right cores.** While you are at the machine, mining runs one thread on each efficiency core (half the cores on a CPU with one kind); once you step away it spreads to the performance cores, fastest first. It never uses a second hyperthread of a core, or a core outside the L3 cache (the low-power E-cores on Intel Meteor Lake). Moving between the two takes xmrig a few milliseconds and keeps its RandomX dataset.
- **Waits for you to step away.** Until your keyboard and mouse have been quiet for a while (5 minutes by default), mining is held to the Ghost ceiling whatever preset you picked. Reading a page or watching a video barely touches the CPU, so idle detection — not CPU load alone — is what decides.
- **Mine now, kindly.** A toggle skips the wait when you want to mine on purpose. It skips *only* the wait: the CPU, battery, and temperature backoff all stay in force, so browsing and video stay smooth.
- **Says what it has earned.** Hashrate now, over 30 minutes and over 24 hours, each as an estimate of XMR per month; when your next p2pool share is due; and how much of the payout window you occupy — in the window and in the tray. The 30-minute and 24-hour figures come from kind-miner's own record, which counts paused time as zero and survives a restart, so they describe what the machine actually earns while it gets out of your way. The tray's **Recent payouts** lists the last five payouts p2pool announced while kind-miner was running, with how long ago and, for the first two hours, when the coinbase output becomes spendable. Your wallet remains the full record.
- **Starts with your machine.** Optional login-item registration on Linux, macOS, and Windows.
- **Backs off for heat.** A configurable temperature ceiling, plus an optional pause while the CPU is clocking itself down — which catches the clogged-fan case a fixed °C limit misses.
- **Battery awareness.** Pauses automatically when unplugged. Resumes on AC.
- **Steps aside for games.** When a game turns on [GameMode](https://github.com/FeralInteractive/gamemode) (or you launch one with `gamemoderun`), mining pauses until it ends.
- **Gives memory back.** When the machine starts thrashing — sustained memory stalls, or free memory below a small margin — the miner is stopped outright, not just paused, so its ~2 GB returns at once. It starts again only after ten calm minutes with room to spare.
- **Works without a tray.** Stock GNOME has no system tray. There, closing the window keeps kind-miner mining in the background (inside a Flatpak it asks the desktop for permission first), and opening kind-miner again from your apps brings the running window back, with a Quit button in it. On every desktop, launching it a second time shows the one already running instead of starting a second miner.
- **Snooze from the tray.** Pause for an hour, until tomorrow morning (06:00), or until you resume — and a snooze ends by itself. A pause survives a restart, so a reboot never brings the miner back early.
- **Tor for onion nodes.** The default node is a Tor hidden service, and with it P2Pool's traffic goes over Tor too, so neither the node nor other P2Pool nodes see your IP. A node on your own network, or any other address that isn't `.onion`, is reached directly: faster, but that node and P2Pool's peers then see your IP.
- **Zero-setup dependencies.** The AppImage, the Flatpak and the .deb ship their engines — XMRig built from source with kind-miner's patches, the pinned P2Pool, and (on the desktop) Tor — so nothing is downloaded when you first run it: no request to GitHub before Tor is up, nothing for antivirus to block. A bare binary with nothing beside it downloads the same pinned, SHA256-verified builds instead. You need: a Monero wallet address.
- **Graphical or terminal.** Double-click for a full window — four-step setup, live dashboard, and a tabbed settings panel — that tucks into the system tray while it mines. Launched from a terminal it behaves as it always has: a tray icon and logs on stdout.
- **Single native binary.** No installer, no runtime, no Python, no Electron. `./kind-miner` and you're done.

## How kindness is applied

The miner only ever asks for what is left underneath the ceiling, so other work
always has the rest of the machine reserved:

```
target  = ceiling − CPU used by everything else
allowed = allowed + (target − allowed) × (falling ? Fall : Rise)
duty    = allowed ÷ (miner's full-tilt CPU share)
```

The asymmetry between `Fall` and `Rise` is the whole idea. A symmetric
controller oscillates against bursty desktop load and you feel every swing;
falling fast and rising slowly turns the same load into a miner that simply
is not there while you work.

The allowance is applied by **duty-cycling** XMRig — suspending and resuming it
(SIGSTOP/SIGCONT) so it runs for `duty` of each half-second — never by changing
its thread count. That matters more than it sounds: RandomX spends ~3 seconds
allocating its dataset on every launch, so a throttler that restarted XMRig to
re-thread it would spend all its time re-initialising and never actually hash.
Pausing keeps the dataset warm, gives smooth fractional control the thread count
cannot, and hands idle cores straight back to your apps the instant it suspends.

## How the scheduler works

Your kindness preset sets the ceiling; the duty is the fraction of each half-second the miner actually runs, recalculated every tick.

```
every 2 seconds:
  other_cpu = total_cpu% - xmrig_cpu%     ← other processes only

  if manually paused                      → allowance 0
  if on battery and pause_on_battery      → allowance 0
  if any core temp >= temp limit          → allowance 0
  if thermally throttled and asked to     → allowance 0
  if screen unlocked and asked to         → allowance 0
  else:
    ceiling = kindness ceiling            ← Ghost's, until you have gone idle
    target  = max(0, ceiling - other_cpu)
    allowed += (target - allowed) x (target < allowed ? Fall : Rise)

  duty = allowed ÷ (miner's full-tilt CPU share), capped at 1
  duty 0  → XMRig stays suspended
  duty 1  → XMRig runs without pauses
  between → XMRig runs for that fraction of each half-second
```

XMRig also runs at OS idle priority (`nice 19` on Linux/macOS,
`IDLE_PRIORITY_CLASS` on Windows). The scheduler is a second, independent layer
on top of that. The combination means the miner loses scheduler fights to
anything — including background browser tabs — and then explicitly hands back
CPU if load climbs further.

On Linux it goes further, since nice 19 still takes a real share of a busy core:

- **CPU:** XMRig runs in its own idle-weighted systemd user scope (`cpu.idle=1`), so any app in your session preempts it outright. Inside Flatpak, or without a systemd user session, its threads run under `SCHED_IDLE` instead.
- **Memory:** its OOM score is 1000, so if memory runs out the kernel kills the miner first, not what you are working in.
- **Disk:** its I/O is in the idle class, served only when nothing else wants the disk.

None of this needs root, and none of it changes the hashrate on an idle machine.

The tray icon reflects state in real time:

| Icon | State | Meaning |
|---|---|---|
| 🟠 colour | Mining | Running at least 90% of the time |
| 🟠 dim | Stepping aside | Running less than 90% of the time |
| ⚫ grey | Paused | Suspended — load, battery, heat, or you |
| ⚫ grey, amber badge | Held | p2pool is mining alone on a sidechain of its own, so shares would never pay. The menu says why; mining resumes by itself once the check passes |

p2pool on an island looks like healthy mining: it takes every share and never
pays, because nobody else is on its chain. Once a minute kind-miner checks that
p2pool has peers, that the sidechain is long and still moving, and that the pool
is much bigger than this machine. If any check fails for three minutes, mining
is held until they pass again.

## The resource chart

The dashboard draws the last 30 seconds (or 60, or 2 minutes) of two series:
every other process's CPU, and the miner's. The band above the kindness ceiling
is shaded — that is the part of the machine mining will not touch — and each
moment the miner handed CPU back is ticked along the bottom.

This exists because the failure mode that costs a user the most is deciding some
unrelated slowdown is the miner's fault and uninstalling it. Watching the orange
line dive the instant the grey one climbs is a better answer to that than any
status line.

Fyne 2.5.3 has no chart widget, so the plot is rasterised with
`golang.org/x/image/vector` and handed to a `canvas.Raster`
(`internal/gui/chart.go`).

**A note on the power figures.** Since CVE-2020-8694 the Linux kernel ships the
RAPL energy counters as root-only, because they are precise enough to infer what
other processes are computing. kind-miner does not ask for privileges to read
them, so on most machines the power tiles show `—`. Where the counters *are*
readable, the miner's share is attributed proportionally to CPU — an
approximation, exact only at the extremes. A modelled wattage presented next to
a measured hashrate would be indistinguishable from a real one, so none is
shown.
