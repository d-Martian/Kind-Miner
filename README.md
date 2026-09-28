<img src="assets/icons/app.png" alt="kind-miner" width="120" align="right" />

# kind-miner

**Idle Monero mining for always-on machines.**  
No account. No pool operator. No fee. Your coins go directly to your wallet.

---

## The premise

Your desktop is already on. The CPU is already drawing power. Those cycles are going nowhere — a modern machine at idle burns 60–120 W regardless of whether it's doing useful work or not.

Monero is one of the last coins where CPU mining is meaningful. RandomX was designed specifically to resist ASICs and favour commodity hardware. A single desktop at home running kind-miner puts real hashrate onto a network that needs it — and does so in a way that is private by default, trustless from end to end, and completely invisible when you actually need your machine.

[P2Pool](https://p2pool.io) changed the economics. Before P2Pool, solo mining was a lottery with months between wins; pools required accounts and took fees. P2Pool is a sidechain on top of Monero: your miner works on the P2Pool sharechain, and when any P2Pool miner finds a Monero block, every participant gets a proportional payout **directly from the coinbase output**. There is no pool wallet. No payout threshold. No operator who knows your address. Your coins arrive in your wallet exactly like solo-mined coins — they just arrive more often.

Kind-miner connects your machine to this infrastructure over Tor. The node doesn't see your IP. The sidechain pays you directly. Nobody in the pipeline knows who you are.

---

## Features

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
- **Tor-native.** Routes through a Tor hidden service by default. Your IP is not visible to the node operator.
- **Zero-setup dependencies.** XMRig and P2Pool are downloaded automatically on first run (SHA256-verified). You need: a Monero wallet address, and Tor running.
- **Graphical or terminal.** Double-click for a full window — four-step setup, live dashboard, and a tabbed settings panel — that tucks into the system tray while it mines. Launched from a terminal it behaves as it always has: a tray icon and logs on stdout.
- **Single native binary.** No installer, no runtime, no Python, no Electron. `./kind-miner` and you're done.

---

## Kindness

Kindness is the one thing kind-miner asks you to understand. Each preset is a
**ceiling** — the share of the whole machine that mining and everything else may
occupy together — plus how fast the miner gives ground and how slowly it takes
it back.

| Preset | Ceiling | What it feels like |
|---|---|---|
| **Ghost** | 52% | Only genuinely idle cycles. You will never notice it; payouts are slow. |
| **Polite** | 78% | Steps aside the moment you touch the machine, and comes back slowly. The default, and the one most people keep. |
| **Balanced** | 90% | Shares the machine evenly. Heavy work still wins, but you may feel a short lag. |
| **Full** | 100% | Fills the rest of the machine and gives it back slowly. Still yields to your apps — it just aims to use what's spare. For machines you are not sitting at. |

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

Upgrading from a version before kindness existed? Your `throttle_sensitivity`
is migrated on first load — `high` → Ghost, `medium` → Polite, `low` →
Balanced — and the dead key is dropped the next time the config is written.

---

## Prerequisites

| Requirement | Notes |
|---|---|
| **Tor** | Must be running at `127.0.0.1:9050`. Kind-miner will attempt to start it automatically if not running. |
| **A Monero wallet address** | Any wallet that gives you a standard address (starts with `4`). [Feather](https://featherwallet.org) is recommended — it also runs over Tor. |
| Linux, macOS, or Windows | Linux is the primary target; macOS and Windows work but are less tested. |

**To install and start Tor:**
```sh
# Debian / Ubuntu / Mint
sudo apt install tor
sudo systemctl enable --now tor

# Fedora / RHEL
sudo dnf install tor
sudo systemctl enable --now tor

# Arch
sudo pacman -S tor
sudo systemctl enable --now tor

# macOS
brew install tor && brew services start tor
```

To confirm Tor is running: `systemctl is-active tor` should print `active`.

---

## Quick start

```sh
# Download the latest release for your platform from:
# https://github.com/kind-miner/kind-miner/releases

# Make executable (Linux/macOS)
chmod +x kind-miner

# Double-click the app, or run it from a terminal:
./kind-miner
#   • From a desktop (double-click / .desktop): opens the full GUI window.
#   • From a terminal: shows a system-tray icon and logs to stdout.

# Force the graphical window, even from a terminal:
./kind-miner --gui

# Headless — no GUI at all, logs to stdout (servers, SSH):
./kind-miner --no-tray        # --headless is an alias
```

On first run with no config file, kind-miner asks one thing: where your rewards go. Under the address field, a pre-ticked box says it will start with your computer and mine gently in the background — untick it and it won't. "More options" lets you use your own Monero node instead of ours over Tor. Everything else starts from a default: the automatic node, the nano sidechain, the bundled binaries, the Polite preset, and pausing on battery; all of it is in Settings. Once mining has started, the window tucks into the system tray and a notification says where it went.

Launched from a terminal, kind-miner runs a short text wizard instead, and a headless launch with no display writes a config template for you to edit.

---

## Headless: kind-minerd

For a box with no screen — a Nodo, a home server — there is `kind-minerd`, the
same miner without the window. It is a static binary with no graphical
dependencies (built with `CGO_ENABLED=0`), for x86-64 and arm64.

```sh
kind-minerd init --address 4…   # write a config for your wallet
kind-minerd run                 # mine until stopped (what a service runs)
kind-minerd status              # what it is doing   (--json for scripts)
kind-minerd doctor              # one sentence on what is wrong, if anything
kind-minerd pair                # the code that pairs a device with this hub
```

Only one kind-miner runs per session: the daemon and the desktop app will not
mine side by side.

### As a service

`packaging/systemd/` has a unit for the daemon and a slice to run it in:

```sh
sudo install -Dm755 kind-minerd /opt/kind-miner/kind-minerd
sudo install -Dm644 packaging/systemd/kind-miner.slice packaging/systemd/kind-minerd.service -t /etc/systemd/system/
sudo /opt/kind-miner/kind-minerd init --address 4…   # writes /etc/kind-miner/config.yaml
sudo systemctl daemon-reload
sudo systemctl enable --now kind-minerd
kind-minerd status
```

The slice sits beside `system.slice` with idle CPU weight, so on a Nodo the
node itself — monerod, the light-wallet server, Tor, Nodo's weekly update —
always wins any core it wants. The service runs as a throwaway system user with
idle I/O, can write only `/var/lib/kind-miner`, reads its config as a private
copy (the file in `/etc` stays root-only), and is first to go if memory runs
out.

### One hub for the house

A Nodo already follows the chain; it can run p2pool once for every machine in
the house, and the desktops then run only the miner. Turn it on in the hub's
config and restart:

```yaml
hub:
  serve: true
```

```sh
sudo systemctl restart kind-minerd
sudo kind-minerd pair --name garage-server
```

`pair` prints a code starting `km1-`. On a desktop, open **Settings →
Connection**, choose mode `hub` and paste it: that machine now runs xmrig
alone, and its dashboard and tray show the household's shares, payouts and
devices. For a box running plain xmrig, `--name` prints the `pools` block for
its `config.json`; its `user` is the name it shows under in `kind-minerd
status`.

Stratum is served over TLS on port 18087 and the household statistics on
18088, with a self-signed certificate made once on the hub. Devices trust it by
the fingerprint in the code, not by any authority: a machine on your network
answering at the hub's address is refused, by xmrig and by the desktop alike.
One hub pays one wallet: everyone who pairs is mining for the hub's owner.

## Sleep

kind-miner mines while your machine is awake and stops when it sleeps. It never keeps a machine awake, and we don't suggest turning sleep off to mine more: a kind miner uses the time you leave it, not time it takes.

---

## Getting the full hashrate

RandomX keeps a ~2 GB dataset and a 2 MiB scratchpad per mining thread, and walks them at random. On the default 4 KiB memory pages that walk misses the TLB constantly, and the miner gives up a large fraction of its hashrate for it. Reserving **huge pages** is the single biggest thing you can do for your rate.

Size the pool for **both** processes. kind-miner runs xmrig *and* p2pool, and p2pool verifies shares with RandomX too. It runs in light mode, so it keeps two 256 MiB caches rather than a second 2 GB dataset, but it starts first. A pool sized for the miner alone loses those pages to p2pool, and the miner silently falls back to 4 KiB pages — the exact thing you were trying to avoid. kind-miner warns at startup when this happens and prints the number to use.

Roughly: 2,080 MiB for the miner's dataset, 2 MiB per mining thread, and about 520 MiB for p2pool. For a 12-thread miner that is around 2,620 MiB — 1,310 pages of 2 MiB, so 1,400 leaves a little slack. Do not over-reserve: pages in the huge page pool are held out of normal use whether or not anything is using them, so the difference comes straight off the RAM available to everything else.

**Linux**
```sh
# Reserve the pages on every boot
echo 'vm.nr_hugepages = 1400' | sudo tee /etc/sysctl.d/99-kind-miner-hugepages.conf
sudo sysctl --system

# Check what you actually got
grep HugePages_Total /proc/meminfo
```

Setting this on a machine that has been up for a while often falls short of what you asked for: memory is fragmented and the kernel cannot find enough contiguous blocks. The drop-in above is applied at boot, when memory is clean — so if the number comes back low, reboot rather than raising it further.

---

## Mining over Wi-Fi

If you mine over Wi-Fi, check whether the radio runs with 802.11 power save on:

```sh
iw dev <interface> get power_save
```

With power save on the radio sleeps between beacons and has to be woken to be serviced. A busy CPU delays that path, the card misses its service windows, and traffic queues for **seconds at a time**. The effect is bursty rather than constant, so the median stays healthy while anything needing many round trips — loading a page, bringing up a VPN — fails outright. It reads exactly like a broken internet connection, which is the trap: the machine looks fine, the router looks fine, and only the one machine that is mining is affected.

Measured on one laptop: hashing drove round trips to the local router — one hop, no internet involved — from 4 ms to over 14 seconds, with a fifth of packets taking more than a second. With power save off and nothing else changed, latency under the same load was indistinguishable from idle.

```sh
sudo nmcli con modify "<connection>" wifi.powersave 2
sudo iw dev <interface> set power_save off
```

kind-miner warns at startup when it sees this. Keeping the radio awake costs a little idle battery; the setting is per-connection, so you can disable it only on the networks you mine over.

---

## Configuration

Config lives at `~/.config/kind-miner/config.yaml` (Linux/macOS) or `%APPDATA%\kind-miner\config.yaml` (Windows).

```yaml
# Your Monero wallet address. Required.
wallet: "4..."

# Connection mode: p2pool-remote | p2pool-local | hub
mode: p2pool-remote

# p2pool-remote: which monerod node p2pool connects to.
# Leave blank to choose automatically: a synced local monerod with ZMQ, else the kind-miner Nodo over Tor.
remote_node: ""

# P2Pool sidechain: "nano" (the default; its 18-hour window keeps even a
# laptop paid), "mini" for a busier chain, "main" above ~50 kH/s.
p2pool_chain: nano

# Set to true to let kind-miner manage the p2pool subprocess.
manage_p2pool: true

# Maximum XMRig threads. 0 = auto — min(logical cores, L3 / 2 MiB), which is
# the most RandomX can use before threads start evicting each other (recommended).
max_threads: 0

# How much of the machine mining may take, and how fast it lets go.
# ghost | polite | balanced | full — see "Kindness" above.
kindness: polite

# Pause when running on battery.
pause_on_battery: true

# Pause while the CPU is clocking itself down for heat.
pause_on_thermal_throttle: true

# Mine only while the screen is locked.
mine_only_when_locked: false

# Pause if any CPU core exceeds this temperature (Celsius).
temp_limit_celsius: 95

# Mine as a guest on a Nodo (or any big.LITTLE box that serves a Monero node):
# low-power cores first, big cores only while monerod and monero-lws are not
# waiting for CPU, nothing while the node syncs, and a 70 °C ceiling. Opt-in,
# config file only. Needs a restart.
mine_on_nodo: false

# hub mode: the code `kind-minerd pair` printed, and this machine's name there.
hub_code: ""
worker_name: ""

# Be the household hub (headless boxes): serve p2pool on the LAN over TLS.
hub:
  serve: false
  stratum_port: 18087
  api_port: 18088

# Dashboard chart — display only.
chart:
  shade_headroom: true
  mark_backoff: true
  draw_temperature: false
  fill_miner: true
  window_seconds: 30

log_level: info
```

Everything here except `mine_on_nodo` and `hub` (headless-box settings) is editable
from the settings window, which groups it the same
way: **Payout**, **Connection**, **Binaries**, **Kindness**, **Graph**,
**Advanced**. Kindness and graph settings take effect while you watch; wallet,
mode, node, hub code and sidechain need a restart, and the window says so when you save.

kind-miner only rewrites the keys it owns, so anything you add by hand is kept.

---

## Connection modes

### `p2pool-remote` (default)

```
XMRig → p2pool (local) ──Tor──→ monerod Nodo (.onion) → Monero network
```

P2Pool runs on your machine. It connects to the kind-miner Nodo — a dedicated Monero full node running as a Tor hidden service, with ZMQ enabled for p2pool. Your IP is not exposed to the node. Payouts arrive directly in your wallet from the P2Pool sharechain. No fee.

**A node on this machine comes first.** With no `remote_node` set, kind-miner looks for a monerod already running here before anything else. If it is synced and publishes ZMQ (`monerod --zmq-pub tcp://127.0.0.1:18083`), p2pool uses it directly and Tor is never started. If it is still syncing, has no ZMQ, or wants an RPC login, the log says which and what to change, and mining uses the Nodo in the meantime. kind-miner keeps checking once a minute and moves p2pool onto the local node, off Tor, as soon as it is ready — or if you start one later — without a restart.

**Tor only for onions.** A `remote_node` on your LAN or the open internet is reached directly; only a `.onion` address goes through Tor, which kind-miner starts for you when it is needed.

### `p2pool-local`

```
XMRig → p2pool (local) → monerod (local) → Monero network
```

You run your own `monerod` locally. Maximum trustlessness — you validate every block yourself. Requires ~180 GB of disk space and 1–3 days for initial sync.

Set `manage_monerod: true` to have kind-miner start and stop `monerod` for you.

### `hub`

```
XMRig ──TLS, LAN──→ p2pool on the household hub → the hub's monerod
```

Only XMRig runs here; the hub (see [One hub for the house](#one-hub-for-the-house)) does the rest, and pays its owner's wallet. No Tor, because nothing leaves the LAN from this machine.

### No `pool` mode

Earlier versions could send XMRig straight to a Stratum pool. That told the pool operator your wallet address, paid them a fee, and connected outside Tor, so it has been removed. A config that still says `mode: pool` is moved to `p2pool-remote` when it loads, with one line in the log. If your machine is too slow for P2Pool shares to arrive often — the reason pool mode used to be suggested — set `p2pool_chain: nano`.

---

## How the scheduler works

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

  threads = floor(allowed x cores), capped at max_threads
  0 threads → suspend (SIGSTOP, instant)
  fewer     → applied immediately
  more      → only after several ticks agree (it restarts XMRig)
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
| 🟠 colour | Mining | Running at its full thread count |
| 🟠 dim | Stepping aside | Running at fewer threads than allowed |
| ⚫ grey | Paused | Suspended — load, battery, heat, or you |
| ⚫ grey, amber badge | Held | p2pool is mining alone on a sidechain of its own, so shares would never pay. The menu says why; mining resumes by itself once the check passes |

p2pool on an island looks like healthy mining: it takes every share and never
pays, because nobody else is on its chain. Once a minute kind-miner checks that
p2pool has peers, that the sidechain is long and still moving, and that the pool
is much bigger than this machine. If any check fails for three minutes, mining
is held until they pass again.

---

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

---

## Privacy model

| Layer | What it protects |
|---|---|
| **Monero** | Transaction amounts, sender, receiver hidden by default via RingCT + stealth addresses. No transaction graph. Coins are fungible. |
| **P2Pool** | The pool operator does not exist. Payouts come from the Monero protocol directly. No operator knows your wallet address or hashrate contribution. |
| **Tor** | The node operator does not see your IP. Your mining activity is not linkable to your network location. |
| **kind-miner** | No telemetry, no analytics, no update pings, no account, no registration. The binary talks to one place: the node, over Tor. |

If you use `p2pool-remote` (the default), the only party who sees anything about you is P2Pool's public sharechain — which shows that *someone* with *some* hashrate submitted a share. Your wallet address appears in the coinbase output of blocks you help find, exactly as it would if you solo-mined.

---

## Building from source

```sh
git clone https://github.com/kind-miner/kind-miner
cd kind-miner
go build -o kind-miner ./cmd/kind-miner
```

Requires Go 1.25+. The GUI uses [Fyne](https://fyne.io), which builds with cgo and
OpenGL, so the build host needs the GL/X11/Wayland development headers. On Fedora:

```sh
sudo dnf install gcc libXxf86vm-devel libX11-devel libXcursor-devel \
                 libXrandr-devel libXinerama-devel libXi-devel \
                 mesa-libGL-devel libxkbcommon-devel wayland-devel
```

(On Debian/Ubuntu the equivalents are `libgl1-mesa-dev xorg-dev libxxf86vm-dev
libxkbcommon-dev`.) The tray icons are embedded statically — no assets need to be
shipped alongside the binary.

```sh
# Run tests
go test ./...
```

---

## The Nodo

The default remote node is a dedicated Monero full node (the "Nodo") running as a Tor hidden service:

```
44yfclfdry66bbpyolux6xmfkcapage7khfk5sax7xmmylwwmjaqukad.onion
RPC  18089   ZMQ  18083
```

It runs `monerod` with ZMQ enabled — a requirement for p2pool that most public nodes skip. It is the only node in kind-miner's default list because we don't advertise nodes we can't verify. If it is temporarily unreachable, kind-miner retries automatically before giving up.

ZMQ cannot travel through a SOCKS proxy, so p2pool on its own would reach an onion node's RPC but never hear about new blocks. kind-miner therefore runs a small relay on `127.0.0.2` that carries both RPC and ZMQ to the node over Tor, with each node on its own Tor circuit. It also checks that the node really speaks ZMQ, not just that the port opens, before mining against it.

You can substitute any monerod node that has ZMQ enabled by setting `remote_node` in your config. If you run your own node on a home server, this is the cleanest option.

---

---

## Philosophy

Privacy is not a feature. It is the precondition for everything else.

Monero exists because surveillance of financial transactions is a form of control. A currency that leaks who paid whom, and how much, is a tool of that control regardless of who runs the nodes. Monero's default privacy means every user gets the same protection — there is no opt-in anonymity set of suspicious users, just one set of everyone.

P2Pool exists because mining centralization is an attack surface. A pool operator who controls the block template controls which transactions get confirmed. P2Pool removes the operator. Kind-miner defaults to P2Pool because that choice costs you nothing and contributes to a network that is harder to censor.

Tor is here because an IP address is metadata, and metadata has a way of becoming evidence.

None of this requires you to be doing anything interesting. The point is that the infrastructure exists and works, and that using it is free. Run the miner, collect some XMR, help keep the network decentralized, and go about your day.

---

## License

MIT. Do what you want with it.
