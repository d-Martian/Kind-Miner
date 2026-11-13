# Headless: kind-minerd

For a box with no screen — a Nodo, a home server — there is `kind-minerd`, the
same miner without the window. It is a static binary with no graphical
dependencies (built with `CGO_ENABLED=0`), for x86-64 and arm64.

**Install it** on Debian or Ubuntu [with apt](../README.md#debian-ubuntu-and-mint-apt):
the package runs it as a service and keeps it current. Anywhere else, build it
with `make build-minerd` (pure Go, so none of the GUI's build dependencies) and
[run it as a service by hand](#as-a-service-by-hand).

### Set it up

Installed, the service is already running, waiting for a wallet. Give it one
either way:

- **From a desktop.** On a machine running kind-miner, open **Settings →
  Connection → Find a hub**. It finds the box on your network and sets it up
  as a hub, paying the address on the Payout tab, and switches
  the desktop to mine through it. See
  [One hub for several computers](#one-hub-for-several-computers).
- **Over SSH.** On the box, run `sudo kind-minerd init --address 4…`. It
  writes the service's config with the hub on, restarts the service, and
  prints the pairing code for your other machines. Add `--no-hub` to have it
  mine on its own instead.

Then `kind-minerd status` says what it is doing.

### Commands

```sh
kind-minerd init --address 4…   # write a config for your wallet
kind-minerd run                 # mine until stopped (what a service runs);
                                # with no config, wait to be set up from a desktop
kind-minerd status              # what it is doing   (--json for scripts)
kind-minerd doctor              # one sentence on what is wrong, if anything
kind-minerd pair                # the code that pairs a device with this hub
```

Only one kind-miner runs per session: the daemon and the desktop app will not
mine side by side.

### Updates, and a way back

The apt package upgrades itself weekly from its own repository over Tor (see
[apt](../README.md#debian-ubuntu-and-mint-apt)). Beyond that:

- **The engines come in the package** — xmrig built from source with
  kind-miner's patches, and the pinned p2pool — so nothing is downloaded at
  run time, and a Nodo gets an xmrig at all (upstream has no arm64 build).
- **A way back.** An upgrade keeps the engines it replaced. New engines have
  15 minutes to show they work — p2pool synced, not on an island, xmrig
  hashing whenever it is allowed to — and if they do not, kind-minerd goes
  back to the previous pair, if that one has worked here before, and says so
  in `kind-minerd status` and `doctor` until a release brings a new pair.

### As a service, by hand

`packaging/systemd/` has a unit for the daemon and a slice to run it in:

```sh
sudo install -Dm755 kind-minerd /opt/kind-miner/kind-minerd
sudo install -Dm644 packaging/systemd/kind-miner.slice packaging/systemd/kind-minerd.service -t /etc/systemd/system/
sudo install -d -m 700 /etc/kind-miner
sudo kind-minerd init --address 4…   # optional: sets up the service and prints the pairing code
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

Skip `init` and the service waits to be set up from a desktop instead: see
[One hub for several computers](#one-hub-for-several-computers).

### The advisory

Once a day every install fetches a small signed file over Tor, from
`https://d-martian.github.io/Kind-Miner/advisory/`: the oldest engine
versions still safe to run, any upcoming fork heights, and the default node
list. It carries facts, never code, so the most its signing key could do is
stop mining, and it does that only on positive knowledge: an engine known to
be older than the floor, or a fork height passed without the version it
needs. You see "Update needed" with the reason. An engine you built yourself
has no known version and is never stopped. Failing to fetch it never stops
anything; after three days `kind-minerd status` says it has not been checked.
Each advisory is good for at most 60 days, and its serial only goes up, so an
old one cannot be replayed.

### Source for the bundled engines

The packages bundle xmrig and p2pool, which are GPLv3. Every release publishes
their complete source beside the binaries:

- `kind-miner-<version>-xmrig-<version>-source.tar.gz` — the pinned xmrig,
  libuv, hwloc and OpenSSL archives, kind-miner's two patches, and the recipe.
  Unpacked, `KM_XMRIG_SOURCES=upstream scripts/build-xmrig.sh out` rebuilds
  the shipped binary offline, bit for bit.
- `p2pool_source-v<version>.tar.xz` — p2pool's own source release, unchanged.

### One hub for several computers

A Nodo already follows the chain, so it can run P2Pool once for every computer
that mines to it, and those computers then run only the miner.

**From the desktop.** Start `kind-minerd` on the Nodo with no config. On a
desktop, open **Settings → Connection → Find a hub**: it finds the Nodo on the
LAN and offers to set it up to pay the address on the Payout tab. The Nodo
writes its own config with the hub on and starts mining; the desktop switches
to mode `hub` with the pairing code filled in. Changing the address on that
desktop later changes the hub's too. Only the desktop that set the hub up can
do that — its owner token is not in the pairing code — and a hub, once set up,
cannot be set up again. Until then, whichever desktop asks first sets it up:
there is no screen on a Nodo to show a code on, so the first connection is
trusted, and from then on only that certificate is.

**Over SSH.** One command on the Nodo:

```sh
sudo kind-minerd init --address 4…
```

It writes the service's config with the hub on, restarts the service, and
prints the pairing code. (`--no-hub` sets it up to mine on its own instead.)
Run without `sudo`, it says so rather than writing a config the service never
reads. Later, `sudo kind-minerd pair` prints the code again, and
`sudo kind-minerd pair --name garage-server` a config for a box running plain
xmrig.

The pairing code starts `km1-`. On a desktop, open **Settings →
Connection**, choose mode `hub` and paste it — this is also how other
computers pair with a hub set up from a desktop, using the code in that
desktop's settings: that machine now runs xmrig
alone, and its dashboard and tray show the hub's shares, payouts and
computers. For a box running plain xmrig, `--name` prints the `pools` block for
its `config.json`; its `user` is the name it shows under in `kind-minerd
status`.

Stratum is served over TLS on port 18087 and the hub's statistics on
18088 (TCP; UDP 18088 answers the desktop's search), with a self-signed
certificate made once on the hub. Devices trust it by
the fingerprint in the code, not by any authority: a machine on your network
answering at the hub's address is refused, by xmrig and by the desktop alike.
One hub pays one wallet: everyone who pairs is mining for the hub's owner.

**Away from home.** With `onion: true` under `hub:` the hub also publishes
both ports as a Tor onion service, from a Tor of its own — its own
configuration and keys under `/var/lib/kind-miner`, so a Nodo update that
rewrites `/etc/tor/torrc` cannot take it away. Pair again after turning it
on: the new code starts `km2-`. A laptop paired with it mines over the LAN at
home; when the LAN address stops answering it moves to the onion through its
own Tor, with the same certificate pin, and moves back by itself once it is
home again. The onion does not help while the hub itself is down — it is on
the same box.
