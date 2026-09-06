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
- **Waits for you to step away.** Until your keyboard and mouse have been quiet for a while (5 minutes by default), mining is held to the Ghost ceiling whatever preset you picked. Reading a page or watching a video barely touches the CPU, so idle detection — not CPU load alone — is what decides.
- **Mine now, kindly.** A toggle skips the wait when you want to mine on purpose. It skips *only* the wait: the CPU, battery, and temperature backoff all stay in force, so browsing and video stay smooth.
- **Says what it has earned.** Hashrate, estimated XMR per day, how often you land a p2pool share, and how much of the payout window you occupy — in the window and in the tray.
- **Starts with your machine.** Optional login-item registration on Linux, macOS, and Windows.
- **Backs off for heat.** A configurable temperature ceiling, plus an optional pause while the CPU is clocking itself down — which catches the clogged-fan case a fixed °C limit misses.
- **Battery awareness.** Pauses automatically when unplugged. Resumes on AC.
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

On first run with no config file, kind-miner asks four questions — where your rewards go, how to reach the Monero network, which xmrig and p2pool to run, and how kind to be — then writes `~/.config/kind-miner/config.yaml` and starts mining. Launched from a terminal it runs a short text wizard instead, and a headless launch with no display writes a config template for you to edit.

---

## Keeping your machine awake

Kind-miner only mines when the machine is on. Most operating systems will suspend the machine automatically after a period of inactivity — when that happens, mining stops until you wake it up.

To mine overnight or continuously, turn off automatic sleep:

**Linux — GNOME**  
Settings → Power → Automatic Suspend → set to **Off**

**Linux — KDE Plasma**  
System Settings → Power Management → Energy Saving → uncheck **Suspend session**

**Linux — command line**
```sh
sudo systemctl mask sleep.target suspend.target hibernate.target hybrid-sleep.target
```

**macOS**  
System Settings → Battery (or Energy Saver) → set **Turn display off after** to **Never**, and enable **Prevent automatic sleeping when the display is off**

**Windows**  
Control Panel → Power Options → Change plan settings → set **Put the computer to sleep** to **Never**

---

## Configuration

Config lives at `~/.config/kind-miner/config.yaml` (Linux/macOS) or `%APPDATA%\kind-miner\config.yaml` (Windows).

```yaml
# Your Monero wallet address. Required.
wallet: "4..."

# Connection mode: p2pool-remote | p2pool-local | pool
mode: p2pool-remote

# p2pool-remote: which monerod node p2pool connects to.
# Leave blank to use the kind-miner Nodo (Tor, ZMQ confirmed).
remote_node: ""

# P2Pool sidechain: "main" above ~50 kH/s, "mini" for most desktops,
# "nano" below ~1 kH/s.
p2pool_chain: mini

# Set to true to let kind-miner manage the p2pool subprocess.
manage_p2pool: true

# Traditional pool URL — only used when mode: pool.
pool_url: ""

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

# Dashboard chart — display only.
chart:
  shade_headroom: true
  mark_backoff: true
  draw_temperature: false
  fill_miner: true
  window_seconds: 30

log_level: info
```

Everything here is editable from the settings window, which groups it the same
way: **Payout**, **Connection**, **Binaries**, **Kindness**, **Graph**,
**Advanced**. Kindness and graph settings take effect while you watch; wallet,
mode, node and sidechain need a restart, and the window says so when you save.

kind-miner only rewrites the keys it owns, so anything you add by hand is kept.

---

## Connection modes

### `p2pool-remote` (default)

```
XMRig → p2pool (local) ──Tor──→ monerod Nodo (.onion) → Monero network
```

P2Pool runs on your machine. It connects to the kind-miner Nodo — a dedicated Monero full node running as a Tor hidden service, with ZMQ enabled for p2pool. Your IP is not exposed to the node. Payouts arrive directly in your wallet from the P2Pool sharechain. No fee.

**Requires:** Tor running at `127.0.0.1:9050`.

### `p2pool-local`

```
XMRig → p2pool (local) → monerod (local) → Monero network
```

You run your own `monerod` locally. Maximum trustlessness — you validate every block yourself. Requires ~180 GB of disk space and 1–3 days for initial sync.

Set `manage_monerod: true` to have kind-miner start and stop `monerod` for you.

### `pool`

```
XMRig → pool (Stratum) → Monero network
```

XMRig connects directly to a Stratum pool. Simpler, but the pool operator knows your wallet address, takes a fee (typically 0.6–1%), and controls payouts. Suitable for machines where your hashrate is too low for P2Pool shares to arrive in reasonable time.

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

The tray icon reflects state in real time:

| Icon | State | Meaning |
|---|---|---|
| 🟠 colour | Mining | Running at its full thread count |
| 🟠 dim | Stepping aside | Running at fewer threads than allowed |
| ⚫ grey | Paused | Suspended — load, battery, heat, or you |

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
