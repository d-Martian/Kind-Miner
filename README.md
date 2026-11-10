<img src="assets/icons/app.png" alt="kind-miner" width="120" align="right" />

# kind-miner

**Mine Monero with the CPU capacity available that you aren't using.**  
kind-miner steps aside the moment you need your computer, and pays straight
into your own wallet — no account, no pool operator, no fee.

**[Install](#install)** · **[First run](#first-run)** · [Headless boxes](#headless-kind-minerd) · [Configuration](#configuration) · [Releases](https://github.com/d-Martian/Kind-Miner/releases)

![The kind-miner dashboard: mining at the Polite preset, with a chart of a burst of desktop work where the miner drops out of the way at once and then creeps back](assets/screenshots/dashboard.png)

*The dashboard. The grey line is everything else on the machine, the orange
line is the miner: when real work starts, the miner gets out of the way at
once, and only creeps back after it ends. (Demo data.)*

---

## The premise

Most of the time a computer is on, most of its CPU is doing nothing. kind-miner gives that spare capacity to Monero and hands it back the instant anything else wants it. It is not free: a busy CPU draws more power, so whether mining pays depends on what your electricity costs. What it never costs you is a smooth desktop.

Monero's RandomX algorithm is designed to favour ordinary CPUs over ASICs, so a single computer adds real hashrate. Mining runs through [P2Pool](https://p2pool.io), which pays every participant directly from the coinbase output: no pool wallet, no payout threshold, no operator who knows your address. By default it works over Tor, so neither the node nor other P2Pool nodes see your IP. Point it at your own node and it connects directly, which is faster.

---

## Features

- **Kindness, not thresholds.** One control (Ghost, Polite, Balanced or Full) sets how much of the machine mining may take and how fast it lets go.
- **Disappears under load.** Every 2 seconds it measures everything else's CPU use and gives the miner only what is left under the kindness ceiling.
- **Shows you it is doing so.** The dashboard plots the miner's CPU against everything else's, with each backoff marked.
- **Uses the right cores.** Efficiency cores while you are at the machine, performance cores once you step away, and never a second hyperthread of a core.
- **Waits for you to step away.** Until keyboard and mouse have been quiet for a while (5 minutes by default), mining is held to the Ghost ceiling.
- **Mine now, kindly.** A toggle skips that wait. The CPU, battery and temperature backoff all stay in force.
- **Says what it has earned.** Hashrate now, over 30 minutes and over 24 hours, each as an estimate of XMR per month, plus recent payouts in the tray.
- **Starts with your machine.** Optional login-item registration on Linux, macOS and Windows.
- **Backs off for heat.** A configurable temperature ceiling, plus an optional pause while the CPU is clocking itself down.
- **Battery awareness.** Pauses when unplugged. Resumes on AC.
- **Steps aside for games.** Mining pauses while a game has [GameMode](https://github.com/FeralInteractive/gamemode) on.
- **Gives memory back.** When the machine starts thrashing, the miner is stopped outright so its ~2 GB returns at once. It restarts after ten calm minutes.
- **Works without a tray.** On a desktop with no system tray, such as stock GNOME, closing the window keeps it mining, and opening kind-miner again brings the window back.
- **Snooze from the tray.** Pause for an hour, until tomorrow morning (06:00), or until you resume. A pause survives a restart.
- **Tor for onion nodes.** The default node is a Tor hidden service, so neither it nor other P2Pool nodes see your IP. Any address that isn't `.onion` is reached directly.
- **Zero-setup dependencies.** The AppImage, the Flatpak and the .deb ship XMRig, P2Pool and (on the desktop) Tor. You need only a Monero wallet address.
- **Graphical or terminal.** Double-click for a full window with setup, dashboard and settings. Launched from a terminal, it is a tray icon and logs on stdout.
- **Single native binary.** No runtime, no Python, no Electron.

More detail on each of these: [How kind-miner works](docs/how-it-works.md).

---

## Install

**You need one thing: a Monero wallet address.** It must be your wallet's
*primary* address — it starts with `4` and is 95 characters long. Many wallets
show a subaddress (starting with `8`) first; P2Pool can't pay one, so
kind-miner refuses it. [Feather](https://featherwallet.org) is recommended — it
also runs over Tor.

Then pick the install that fits the machine:

| Your machine | Install | Updates |
|---|---|---|
| A Debian, Ubuntu or Mint desktop (x86-64) | [apt](#debian-ubuntu-and-mint-apt): `kind-miner` | automatic, weekly |
| A box with no screen — a [Monero Nodo](https://moneronodo.com/), a home server — on Debian or Ubuntu (x86-64 or arm64) | [apt](#debian-ubuntu-and-mint-apt): `kind-minerd` | automatic, weekly |
| Any other Linux desktop (x86-64) | [AppImage or Flatpak](#other-linux-desktops-appimage-or-flatpak) | download each new release |
| macOS or Windows | [an archive](#macos-windows-or-a-bare-linux-binary) | download each new release |

Linux is the primary target; macOS and Windows work but are less tested.

Every file is on the [releases page](https://github.com/d-Martian/Kind-Miner/releases),
newest at the top, each with a `.sha256` beside it to check the download
against. The commands below name the current release, `0.2.0-rc10`. If the page
shows a newer one, change `VERSION` to match (without the leading `v`).

### Debian, Ubuntu and Mint: apt

There are two packages; install the one that fits the machine:

- **`kind-miner`** — the desktop app, with a window and a tray icon. amd64.
- **`kind-minerd`** — the service for a machine with no screen. amd64 and arm64.

They cannot be installed side by side: installing one removes the other, so a
machine never runs two miners.

You download the first .deb by hand, once. It adds kind-miner's own apt
repository, and from then on apt keeps it up to date.

**The desktop app:**

```sh
sudo apt update
VERSION=0.2.0-rc10
DEB=kind-miner_${VERSION}_amd64.deb
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$DEB
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$DEB.sha256
sha256sum -c $DEB.sha256        # go on only if this prints "OK"
sudo apt install ./$DEB tor
```

Then open **kind-miner** from your applications menu and carry on at
[First run](#first-run).

**The headless service:**

```sh
sudo apt update
VERSION=0.2.0-rc10
DEB=kind-minerd_${VERSION}_$(dpkg --print-architecture).deb
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$DEB
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$DEB.sha256
sha256sum -c $DEB.sha256        # go on only if this prints "OK"
sudo apt install ./$DEB
```

apt installs `tor` along with it.

The service starts at once and waits to be set up: see
[Headless: kind-minerd](#headless-kind-minerd).

Upgrading, removing and troubleshooting the apt packages: [Install notes](docs/install.md).

### Other Linux desktops: AppImage or Flatpak

Both carry XMRig, P2Pool and Tor, so nothing is downloaded when you first run
them. Neither updates itself: when a new release is out, download it the same
way.

**AppImage** — one file, nothing to install:

```sh
VERSION=0.2.0-rc10
APP=kind-miner-v${VERSION}-linux-x86_64.AppImage
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$APP
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$APP.sha256
sha256sum -c $APP.sha256
chmod +x $APP
./$APP --gui
```

The first time it runs it adds itself to your applications menu, so after that
you can start it from there. Keep the file where it is: the menu entry points
at it.

**Flatpak** — needs `flatpak` installed; the runtime comes from Flathub:

```sh
VERSION=0.2.0-rc10
PAK=kind-miner-v${VERSION}-linux-amd64.flatpak
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$PAK
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$PAK.sha256
sha256sum -c $PAK.sha256
flatpak install --user $PAK
```

Then open **kind-miner** from your applications menu, or run
`flatpak run io.github.d_Martian.KindMiner`. For a later release, install
its bundle the same way (add `--reinstall` if flatpak says it is already
installed).

### macOS, Windows, or a bare Linux binary

These archives hold the program alone, with this README and
`config.example.yaml`. On first run it downloads the pinned XMRig and P2Pool
and checks their SHA256 before using them.

**Tor is not included**, so install it first: kind-miner starts it for you, but
it needs the program to be there. (It also uses a Tor already listening on
`127.0.0.1:9050`.)

| Platform | Archive | Tor |
|---|---|---|
| macOS, Apple silicon | `kind-miner-v<VERSION>-darwin-arm64.tar.gz` | `brew install tor` |
| macOS, Intel | `kind-miner-v<VERSION>-darwin-amd64.tar.gz` | `brew install tor` |
| Windows | `kind-miner-v<VERSION>-windows-amd64.zip` | `tor.exe` from the [Tor Expert Bundle](https://www.torproject.org/download/tor/), on your `PATH` or in a `bin` folder beside `kind-miner.exe` |
| Linux (x86-64) | `kind-miner-v<VERSION>-linux-amd64.tar.gz` | `sudo apt install tor`, `sudo dnf install tor` or `sudo pacman -S tor` |

On macOS, for example:

```sh
VERSION=0.2.0-rc10
NAME=kind-miner-v${VERSION}-darwin-arm64     # -darwin-amd64 on an Intel Mac
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$NAME.tar.gz
curl -fLO https://github.com/d-Martian/Kind-Miner/releases/download/v$VERSION/$NAME.tar.gz.sha256
shasum -a 256 -c $NAME.tar.gz.sha256
tar xzf $NAME.tar.gz
$NAME/kind-miner --gui
```

On Windows, unzip the archive and double-click `kind-miner.exe`.

---

## First run

### In the window

1. Open kind-miner — from the applications menu, or by double-clicking it.
2. Paste your wallet's primary address.
3. Under it, a ticked box says it **starts with your computer and mines gently
   in the background**. Untick it if you'd rather start it yourself.
4. Press **Start mining**.

<img src="assets/screenshots/onboarding.png" alt="The first-run screen: one field for your Monero payout address, a ticked box to start with your computer, and a Start mining button" width="360" />

That is the whole setup. **More options** lets you use your own Monero node
instead of ours over Tor; everything else starts from a default — the
automatic node, the nano sidechain, the bundled engines, the Polite preset,
and pausing on battery — and all of it can be changed later in Settings.

Once mining has started, the window tucks into the system tray and a
notification says where it went. On a desktop with no tray, such as stock
GNOME, closing the window keeps it mining; open kind-miner again from your
apps to bring the window back. Launching it a second time always shows the
one already running rather than starting a second miner.

### From a terminal

Launched from a terminal, kind-miner shows a tray icon and logs to stdout, and
on first run asks for your address in the terminal instead of the window:

```sh
kind-miner                 # tray icon, logs on stdout; first run is a short text wizard
kind-miner --gui           # the full window, even from a terminal
kind-miner --no-tray       # no GUI at all, for servers and SSH (--headless is an alias)
kind-miner --config PATH   # use a config file somewhere else
kind-miner --version
```

(For the AppImage or an unpacked archive, run it by its path, as above.)
Started with no config and neither a terminal nor a display — as a service,
say — it writes a config template for you to edit instead: see
[Configuration](#configuration). For a machine that never has a screen,
[kind-minerd](#headless-kind-minerd) is the better fit: it is the same miner
built without the GUI, and the apt package runs it as a service.

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
| **Full** | 100% | Every core, straight away — it doesn't wait for you to step away. Still yields to your apps and gives the CPU back slowly. For machines you are not sitting at. |

The formula behind the presets, and how the limit is applied: [How kind-miner works](docs/how-it-works.md#how-kindness-is-applied).

---

## Headless: kind-minerd

For a box with no screen — a Nodo, a home server — there is `kind-minerd`, the same miner without the window. Install it on Debian or Ubuntu [with apt](#debian-ubuntu-and-mint-apt); the package runs it as a service and keeps it current.

Setup, commands, updates, running it as a service by hand, and one hub for several computers: [Headless: kind-minerd](docs/headless.md).

## Sleep

kind-miner mines while your machine is awake and stops when it sleeps. It never keeps a machine awake, and we don't suggest turning sleep off to mine more: a kind miner uses the time you leave it, not time it takes.

---

## Tuning

Two system settings can make a large difference:

- **Huge pages.** Mining works through about 2 GB of memory at random, and the computer's default small memory pages slow that down a lot. Setting aside larger "huge pages" for the miner is the single biggest thing you can do for your hashrate.
- **Wi-Fi power save.** If you mine over Wi-Fi with power save on, the connection on that machine can stall for seconds at a time.

kind-miner warns at startup when it sees either problem. Both fixes are in [Tuning](docs/tuning.md).

---

## Configuration

Almost everything can be changed in the settings window. The same settings live in a config file: `~/.config/kind-miner/config.yaml` on Linux and macOS, `%APPDATA%\kind-miner\config.yaml` on Windows.

Every option, the connection modes and the default node: [Configuration](docs/configuration.md).

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
| **Monero** | Amounts, sender and receiver are hidden by default (RingCT and stealth addresses). Coins are fungible. |
| **P2Pool** | There is no pool operator. Payouts come directly from the Monero protocol, and no operator knows your wallet address or hashrate. |
| **Tor** | With a `.onion` node (the default), the node and other P2Pool nodes do not see your IP. A node at any other address is reached directly, and it and P2Pool's peers do see your IP. |
| **kind-miner** | No telemetry, analytics, account or registration. Beyond the node and P2Pool's peers it fetches only the signed advisory: once a day, over Tor, and not at all when Tor isn't running. A bare binary also downloads its pinned engines once, on first run. The macOS, Windows and plain Linux archives use upstream XMRig, which keeps its 1% minimum donation and connects to XMRig's pool directly; the .deb, AppImage and Flatpak ship XMRig built without it. |

With the default `.onion` node, the only public trace is P2Pool's sharechain, which shows that *someone* with *some* hashrate submitted a share. Your wallet address appears in the coinbase of blocks you help find, exactly as in solo mining.

---

## Building from source

```sh
git clone https://github.com/d-Martian/Kind-Miner
cd Kind-Miner
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

## Philosophy

Privacy is not a feature. It is the precondition for everything else.

Monero gives every user the same privacy by default. P2Pool removes the pool operator, who would otherwise control which transactions get confirmed. Tor is here because an IP address is metadata, and metadata has a way of becoming evidence.

You don't need something to hide to want privacy. Run the miner, collect some XMR, help keep the network decentralized, and go about your day.

---

## License

MIT. Do what you want with it.
