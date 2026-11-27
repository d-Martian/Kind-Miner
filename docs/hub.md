# The household hub

A hub is one machine that runs P2Pool for every computer in the house. The
others run only the miner and send their work to it. It usually runs on a
[Monero Nodo](https://moneronodo.com/) or a home server that is on all the
time and already follows the chain.

```
desktop  (kind-miner, mode hub) ──┐
laptop   (kind-miner, mode hub) ──┤   TLS, port 18087
garage   (plain xmrig)          ──┼──────────────────→  hub: p2pool ──→ monerod
your own xmrig-proxy            ──┘
                                      HTTPS, port 18088: the household's statistics
```

Why run one:

- **One P2Pool, one sync.** Each paired machine skips P2Pool and its RandomX
  cache. Only the hub follows the sidechain and talks to a node.
- **One set of numbers.** Every paired desktop shows the household's shares,
  payouts and devices, read from the hub.
- **Nothing leaves the house from the paired machines.** They talk only to the
  hub over the LAN, so they need no Tor and no node.

**One hub pays one wallet.** Everyone who pairs mines for the hub owner's
address. P2Pool pays the wallet it was started with and ignores any address a
miner logs in with, so a paired device has no way to mine for a different
wallet.

---

## Set up the hub

Install `kind-minerd` on the hub machine first: see
[Debian, Ubuntu and Mint: apt](../README.md#debian-ubuntu-and-mint-apt). Once
installed, the service is running and waiting for a wallet. Set it up one of
two ways.

### From a desktop, no SSH

1. On a desktop running kind-miner, enter your wallet's primary address on the
   **Payout** tab of Settings.
2. Open **Settings → Connection → Find a hub on this network…**. kind-miner
   searches the LAN and lists the hubs that answer. If nothing answers, which
   happens when your network blocks broadcasts or the hub is on another
   subnet, type the hub's address instead, e.g. `192.168.1.20`.
3. Next to a hub that says *not set up yet*, choose **Set up…**. Check that the
   address in the dialog is yours and confirm.
4. Press **Save**, then restart kind-miner when the window asks. The desktop
   is now in mode `hub` with the pairing code filled in. The hub starts mining
   itself within a few minutes.

What this does:

- The hub writes its own config, `/var/lib/kind-miner/config.yaml`, with the
  hub turned on.
- **This desktop becomes the hub's owner.** It keeps an owner token, the
  `hub_owner_token` line in its own config. It is the only machine that can
  change the hub's wallet later. The token is never part of the pairing code,
  so a housemate who pairs a laptop cannot redirect the household's earnings.
- **The first desktop to ask wins.** A Nodo has no screen to show a
  confirmation code on, so the first desktop to set it up is trusted. After
  that the hub refuses to be set up again. Every other desktop gets *already
  set up* and pairs with the code instead (below).

### Over SSH

On the hub:

```sh
sudo kind-minerd init --address 4…
```

This writes root's config at `/etc/kind-miner/config.yaml` with the hub on,
restarts the service, and prints the pairing code. You can print it again at
any time:

```sh
sudo kind-minerd pair
```

A hub set up this way has no owner desktop: you change it over SSH, in that
file. If both configs exist, the one in `/etc/kind-miner` wins over the one a
desktop wrote.

Add `--no-hub` to `init` to have the box mine on its own instead of serving the
house.

### By hand

A kind-minerd that already mines on its own, for example one set up with
`--no-hub` or [run as a service by hand](headless.md#as-a-service-by-hand),
becomes a hub when you add this to its config and restart it:

```yaml
hub:
  serve: true
```

It needs `manage_p2pool: true` (the default), and it can't be in mode `hub`
itself. A box can't mine to a hub and be one at the same time. Then run
`sudo kind-minerd pair` for the code.

---

## Pair a device

Every device trusts the hub by its certificate, which the hub makes once. The
pairing code carries the certificate's fingerprint, and a device refuses any
other certificate. That is what stops another machine on your network, say a
phone on the guest Wi-Fi claiming to be `moneronodo.lan`, from answering as
the hub and taking the household's hashrate for its own wallet.

### A desktop or laptop running kind-miner

1. Get the code. It's either the one `sudo kind-minerd pair` prints on the hub,
   or the one in the owner desktop's **Settings → Connection**.
2. On the new machine, open **Settings → Connection**, choose mode `hub` and
   paste the code. **Worker name** is what the machine is called in the hub's
   device list. If you leave it blank, the hostname is used.
3. Save and restart kind-miner.

That machine now runs xmrig alone. Its dashboard and tray show the hub's
shares, payouts and devices. Kindness, idle detection and every pause
condition still apply on the machine itself: being paired changes where the
work goes, not how politely it is done.

### A headless box running kind-minerd

`init` only sets a box up to run P2Pool, so write the config yourself:

```sh
sudo install -d -m 700 /etc/kind-miner
sudo tee /etc/kind-miner/config.yaml >/dev/null <<'EOF'
mode: hub
hub_code: "km1-…"
worker_name: "garage-server"
EOF
sudo chmod 600 /etc/kind-miner/config.yaml
sudo systemctl restart kind-minerd
kind-minerd status
```

In mode `hub` no wallet is needed, because the hub's own wallet is paid.

### A machine running plain xmrig

For a box where you'd rather not install kind-miner at all:

```sh
sudo kind-minerd pair --name garage-server
```

After the code, this prints a `pools` block to put in that xmrig's
`config.json`:

```json
{
  "pools": [
    {
      "url": "192.168.1.20:18087",
      "user": "garage-server",
      "tls": true,
      "tls-fingerprint": "…64 hex characters…",
      "keepalive": true
    }
  ]
}
```

`user` is the name the box shows up under in `kind-minerd status`. `pair`
cleans the name you give it: anything other than letters, digits, `-` and `_`
becomes a hyphen, and it is cut at the first `.` and at 31 characters. That's
because P2Pool treats some characters in the login specially, and anything
after a `+` sets a fixed difficulty.

This box gets none of kind-miner's restraint: plain xmrig mines flat out
whenever it runs.

### A mining proxy you already run

Yes, a hub can be the upstream for a mining proxy such as xmrig-proxy. To
P2Pool the proxy is just another miner. Ask the hub for a pools block named
after the proxy:

```sh
sudo kind-minerd pair --name proxy
```

Put that entry in the proxy's `config.json` in place of the pool it mines to
now. xmrig-proxy's pool entries take the same `url`, `user`, `tls`,
`tls-fingerprint` and `keepalive` fields as xmrig's.

```json
"pools": [
  {
    "url": "192.168.1.20:18087",
    "user": "proxy",
    "tls": true,
    "tls-fingerprint": "…64 hex characters…",
    "keepalive": true
  }
]
```

Things to know:

- **Every rig behind the proxy now pays the hub's wallet.** The `user` is only
  a label. A wallet address in the proxy's or the rigs' configs is ignored.
- **The hub sees the proxy, not the rigs.** `kind-minerd status` lists each
  connection the proxy opens to the hub under the `user` above, from the
  proxy's address. How many connections that is depends on the proxy's own
  `mode`. Per-rig figures stay in the proxy's own statistics.
- **Keep `tls` and `tls-fingerprint`.** The port also accepts plain stratum,
  which is how the hub's own miner reaches P2Pool over loopback. Without the
  pin, though, anything on your network that answers at the hub's address
  gets your rigs' hashrate.
- **Use the hub, not a desktop's kind-miner.** The hub is the machine
  kind-miner sets up to serve others, with TLS, a pinned certificate and a
  device list.
- **Away from home:** with an onion hub (below), `pair` prints a second entry
  that reaches the hub through Tor, using the `socks5` field. Keep it only if
  the proxy's machine runs Tor on `127.0.0.1:9050`.

The other direction isn't supported. kind-miner doesn't mine *to* an arbitrary
pool or proxy. It mines to the P2Pool it runs itself, or to a hub by its
pairing code. (Pool mode was removed: see
[No `pool` mode](configuration.md#no-pool-mode).) If you already run P2Pool
yourself, set `manage_p2pool: false` and kind-miner's xmrig mines to
`127.0.0.1:3333`.

---

## The pairing code

A code looks like `km1-…`: one line, safe to paste into a shell or YAML. It
holds:

- the hub's address and its two ports,
- the SHA-256 fingerprint of the hub's certificate,
- the token for the hub's statistics,
- for `km2-` codes only, the hub's onion address (see below).

Treat it like your Wi-Fi password, not like a wallet key. Someone who has it
can see your device list and mine to the hub, which only ever pays you. It
cannot change the hub's wallet: only the owner token can do that, and the code
never carries it.

**The address is part of the code.** `pair` puts in the machine's LAN address
(a desktop that set the hub up puts in the address it reached the hub at). If
that address can change, either reserve it in your router or give a name
instead:

```sh
sudo kind-minerd pair --host nodo.lan
```

If the hub's address changes after you paired, pair each device again with a
fresh code.

The certificate is made once, the first time the hub starts, and is valid for
a hundred years. Devices pin it by fingerprint and never check dates, so it
never expires out from under you, and you never need to pair again for it.

---

## Away from home

With `onion: true` the hub also publishes both ports as a Tor onion service:

```yaml
hub:
  serve: true
  onion: true
```

It does this from a Tor of its own, with its own configuration and keys under
`/var/lib/kind-miner`. That way a Nodo update that rewrites `/etc/tor/torrc`
can't take the onion away.

Restart, wait a minute, then pair again: the new code starts `km2-`.
(`kind-minerd pair` says so if the onion isn't up yet, and gives a LAN-only
code in the meantime.)

- **A kind-miner laptop** paired with a `km2-` code mines over the LAN at
  home. When the LAN address stops answering, it moves to the onion through
  its own Tor, with the same certificate pin, and moves back by itself once
  it is home.
- **Plain xmrig or a proxy** gets a second pool entry with
  `"socks5": "127.0.0.1:9050"`. It needs a Tor running on that machine.

The onion does not help while the hub itself is down, since it runs on the
same box.

---

## Watching it

On the hub:

```
$ kind-minerd status
  …
  hub       serving stratum on port 18087 (TLS) · 3 connected
            desktop                    4210 H/s  192.168.1.31:51544     up 2h10m0s
            garage-server              2900 H/s  192.168.1.40:40112     up 26h3m0s
            proxy                     18500 H/s  192.168.1.50:38870     up 3h41m0s
```

A hashrate shows as `—` for the first minute or so, until P2Pool has seen
enough shares to estimate it. `kind-minerd status --json` gives the same
information for scripts.

On a paired desktop the dashboard and tray show the household: the hub's
shares, its payouts and every device. On a paired kind-minerd box,
`kind-minerd status` says `mining to the hub at …`, and adds `(reached over
Tor)` while it is away from home.

---

## Changing it later

**The wallet.**

- *A hub set up from a desktop:* change the address on the owner desktop's
  **Payout** tab and save. The hub picks it up and restarts P2Pool. Shares
  already in the payout window still pay the old address.
- *A hub set up over SSH:* edit `wallet:` in `/etc/kind-miner/config.yaml`
  and run `sudo systemctl restart kind-minerd`.

**Lost the owner desktop?** Ownership is three lines in that desktop's config:
`mode: hub`, `hub_code` and `hub_owner_token`. Copy them into a new desktop's
config to carry it over. Without them, take the hub over from SSH by writing
root's config:

```sh
sudo kind-minerd init --address 4… --config /etc/kind-miner/config.yaml
```

Root's config then wins over the one the desktop wrote. From then on the hub
is changed over SSH. Its certificate is kept, so paired devices carry on.

**The ports.** Set `hub.stratum_port` and `hub.api_port`, restart, and pair
every device again: the ports are in the code. The search that **Find a hub**
uses always listens on UDP 18088.

**The sidechain.** Every paired device mines on the hub's sidechain,
`p2pool_chain` in the hub's config. nano is the default.

---

## Network and security

| Port | Protocol | What | Who needs it |
|---|---|---|---|
| 18087 | TCP | P2Pool stratum, TLS (plain also accepted) | every paired device |
| 18088 | TCP | the household's statistics, HTTPS, needs the token; also where a new hub waits to be set up | paired kind-miner and kind-minerd machines |
| 18088 | UDP | answers **Find a hub** | only for searching; a code works without it |

The hub listens on these ports on every interface. Allow them from your LAN
only. On a machine with a public address, block them from the internet: anyone
who can reach port 18087 can mine to the hub. That pays you, but it is not
your network's business, and plain connections are accepted.

What each party can do:

- **Anyone who can reach the ports** can mine to the hub's wallet, and ask the
  hub its name. While a new hub is still waiting to be set up, they can also
  set it up, so set it up soon after installing it.
- **Anyone with the pairing code** can also read the household's statistics,
  including the device list.
- **The owner desktop** can also change the hub's wallet, and only on a hub
  that was set up from a desktop.
- **Root on the hub** can do everything, through `/etc/kind-miner/config.yaml`.

The hub's state lives in `/var/lib/kind-miner/hub`: its certificate and key,
the statistics token, the owner token and the onion keys. Keep it, and the
whole house stays paired. `sudo apt purge kind-minerd` removes it along with
everything else.

---

## When something is wrong

`kind-minerd doctor` on the hub gives one sentence on what is wrong, if
anything. Common cases:

| You see | What it means |
|---|---|
| `this machine is not serving the hub yet` | `hub.serve` is off in the config `pair` read. Turn it on and restart. |
| `the hub has not started yet` | The service hasn't made its certificate yet. `sudo systemctl restart kind-minerd`, then pair again. |
| `the service's config can be read only by root` | Run the command with `sudo`. |
| **Find a hub** finds nothing | Broadcasts don't reach the hub. This is common with Wi-Fi client isolation, mesh networks and separate subnets. Type the hub's address in the same dialog. |
| `already set up — paste its pairing code…` | Another desktop set this hub up first. Ask its owner for the code, or run `sudo kind-minerd pair` on the hub. |
| A device connects but never shows in `status` | Check its fingerprint matches the one `pair` printed. A device with the wrong pin refuses the hub, rather than the hub refusing it. |
| A desktop says the hub's statistics are unavailable | Port 18088 is blocked between them, or the code is from a different hub. Mining still works: it only needs 18087. |
