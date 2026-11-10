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

- **Mining settings: Ghost, Polite, Balanced, or Full.** These set how much of your computer mining may use, from Ghost (the least) to Full (the most). Even Full backs off when your computer needs the power for something else.
- **Stays out of your way.** Every 2 seconds it checks how busy your computer is, and gives mining only what's left over.
- **Shows you what it's doing.** A live chart shows how much of your computer mining is using next to everything else, and marks each time mining stepped back.
- **Leaves the fast parts for you.** While you're using the computer, mining keeps to part of the processor (the low-power cores, on chips that have them) and leaves the rest for you. Once you step away, it spreads out.
- **Waits for you to step away.** Mining stays gentle until you haven't touched the keyboard or mouse for a few minutes. A "Mine now, kindly" switch skips the wait.
- **Shows what you're earning.** Your mining speed (hashrate) now, over 30 minutes and over 24 hours, each with an estimate of the Monero (XMR) it would earn per month, plus your recent payouts.
- **Starts with your computer.** It can start by itself whenever you log in, on Linux, macOS and Windows. You can switch that off.
- **Pauses when it should.** Mining eases off when your computer gets hot, pauses while a laptop is on battery, and pauses while a game is running with [GameMode](https://github.com/FeralInteractive/gamemode) (a Linux tool that game launchers can switch on).
- **Gives memory back.** When your computer runs short of memory, mining stops completely and frees up what it was using. It starts again after ten calm minutes.
- **Pause it whenever you like.** From its small icon in the system tray (the row of icons by the clock), pause mining for an hour, until 6:00 tomorrow morning, or until you start it again. It stays paused even if you restart.
- **Private by default.** Out of the box, mining connects through Tor, a network that hides your internet address (IP) from the computers it talks to. If you switch to a different Monero server (node) whose address doesn't end in `.onion`, it connects directly and they can see your IP.
- **Everything in one download.** On Linux, the AppImage, Flatpak and .deb packages come with the mining programs they need, so there is nothing more to set up. All you bring is a Monero wallet address.
- **Window or command line.** Double-click it for a normal window with setup, a dashboard and settings. Started from a terminal instead, it shows a tray icon and prints what it's doing as text.

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

Requires Go 1.25+ and, for the GUI, the GL/X11/Wayland development headers. Steps and package lists: [Building from source](docs/building.md).

---

## Philosophy

Privacy is not a feature. It is the precondition for everything else.

Monero gives every user the same privacy by default. P2Pool removes the pool operator, who would otherwise control which transactions get confirmed. Tor is here because an IP address is metadata, and metadata has a way of becoming evidence.

You don't need something to hide to want privacy. Run the miner, collect some XMR, help keep the network decentralized, and go about your day.

---

## License

MIT. Do what you want with it.
