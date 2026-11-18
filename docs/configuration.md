# Configuration

Config lives at `~/.config/kind-miner/config.yaml` on Linux and macOS — or `$XDG_CONFIG_HOME/kind-miner/config.yaml` if you set that variable — and at `%APPDATA%\kind-miner\config.yaml` on Windows. A config already in `~/.config/kind-miner` keeps being used until there is one in the `XDG_CONFIG_HOME` folder, and the Flatpak always uses `~/.config/kind-miner`, so it shares one config with the other builds.

```yaml
# Your wallet's primary address. Required. It starts with 4 and is 95
# characters long; subaddresses (8…) and integrated addresses are refused,
# because P2Pool pays in the coinbase, which only a primary address receives.
wallet: "4..."

# Connection mode: p2pool-remote | p2pool-local | hub
mode: p2pool-remote

# p2pool-remote: which monerod node p2pool connects to.
# Leave blank to choose automatically: a synced local monerod with ZMQ, else the default node over Tor.
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
# ghost | polite | balanced | full — see "Kindness" in the README.
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

# Be a hub (headless boxes): serve p2pool on the LAN over TLS.
hub:
  serve: false
  stratum_port: 18087
  api_port: 18088
  onion: false   # also serve both as a Tor onion, for devices away from home

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

## Connection modes

### `p2pool-remote` (default)

```
XMRig → p2pool (local) ──Tor──→ default node (.onion) → Monero network
```

P2Pool runs on your machine. By default it follows kind-miner's [default node](#the-default-node) — a Monero full node running as a Tor hidden service, with ZMQ enabled for p2pool — so the node never sees your IP. Payouts arrive directly in your wallet from the P2Pool sharechain. No fee.

**A node on this machine comes first.** With no `remote_node` set, kind-miner looks for a monerod already running here before anything else. If it is synced and publishes ZMQ (`monerod --zmq-pub tcp://127.0.0.1:18083`), p2pool uses it directly and Tor is never started. If it is still syncing, has no ZMQ, or wants an RPC login, the log says which and what to change, and mining uses the default node in the meantime. kind-miner keeps checking once a minute and moves p2pool onto the local node, off Tor, as soon as it is ready — or if you start one later — without a restart.

**Tor only for onions.** A `remote_node` on your LAN or the open internet is reached directly; only a `.onion` address goes through Tor, which kind-miner starts for you when it is needed.

### `p2pool-local`

```
XMRig → p2pool (local) → monerod (local) → Monero network
```

You run your own `monerod` locally. Maximum trustlessness — you validate every block yourself. Requires ~180 GB of disk space and 1–3 days for initial sync.

Set `manage_monerod: true` to have kind-miner start and stop `monerod` for you.

### `hub`

```
XMRig ──TLS, LAN──→ p2pool on the hub → the hub's monerod
```

Only XMRig runs here; the hub (see [One hub for several computers](headless.md#one-hub-for-several-computers)) does the rest, and pays its owner's wallet. No Tor, because nothing leaves the LAN from this machine.

### No `pool` mode

Earlier versions could send XMRig straight to a Stratum pool. That told the pool operator your wallet address, paid them a fee, and connected outside Tor, so it has been removed. A config that still says `mode: pool` is moved to `p2pool-remote` when it loads, with one line in the log. If your machine is too slow for P2Pool shares to arrive often — the reason pool mode used to be suggested — set `p2pool_chain: nano`.

## The default node

With no `remote_node` set and no node on this machine, kind-miner follows one Monero full node, run for this project as a Tor hidden service:

```
44yfclfdry66bbpyolux6xmfkcapage7khfk5sax7xmmylwwmjaqukad.onion
RPC  18089   ZMQ  18083
```

It runs `monerod` with ZMQ enabled — a requirement for p2pool that most public nodes skip. It is the only node in kind-miner's default list because we don't advertise nodes we can't verify. If it is temporarily unreachable, kind-miner retries automatically before giving up.

ZMQ cannot travel through a SOCKS proxy, so p2pool on its own would reach an onion node's RPC but never hear about new blocks. kind-miner therefore runs a small relay on `127.0.0.2` that carries both RPC and ZMQ to the node over Tor, with each node on its own Tor circuit. It also checks that the node really speaks ZMQ, not just that the port opens, before mining against it.

You can substitute any monerod node that has ZMQ enabled by setting `remote_node` in your config. If you run your own node on a home server, this is the cleanest option.
