# Install notes

Extra detail for the install steps in the [README](../README.md#install).

## Notes on the apt packages

- **Keep the `./`.** `apt install ./file.deb` installs that file; without the
  `./`, apt looks for a package of that name in its repositories and fails.
- **`-f` matters too.** It makes curl fail on an error rather than save
  GitHub's error page under the package's name, which apt then rejects as
  "Invalid archive signature". If `sha256sum` says `FAILED`, delete both files
  and download them again.
- **Kept current, over Tor.** The package adds
  `tor+https://d-martian.github.io/Kind-Miner/apt`, signed with a key it
  carries, and pins that repository so it can supply `kind-miner` and
  `kind-minerd` and nothing else — whoever controlled it could not replace a
  system package. A timer upgrades it weekly on Thursday: the desktop app in
  the afternoon, or at the next boot if the machine was off; kind-minerd
  between 03:00 and 06:00, away from Nodo's own Monday 04:00 update. It
  refreshes only its own repository. A running desktop app keeps its version
  until it is next opened.
- **To upgrade now** rather than wait for Thursday:
  `sudo apt update && sudo apt install kind-miner` (or `kind-minerd`).
- **Why `tor`.** The repository is reached only over Tor, so upgrades need it.
  The desktop app uses it too while it runs, and starts its own Tor only when
  there is none.
- **Coming from the AppImage?** Your data carries over, since both keep it in
  `~/.local/share/kind-miner`. Delete the menu entry the AppImage made,
  `~/.local/share/applications/kind-miner.desktop`, or it shadows the
  package's.
- **To remove it:** `sudo apt purge kind-miner` removes the program, the timer
  and the repository; each user's data stays in their home.
  `sudo apt purge kind-minerd` removes everything: the program, both pairs of
  engines, the timer, the config, and the state — p2pool's data, the hub's
  certificate and tokens, the onion keys.
- **To remove your own data too:** the desktop app keeps your settings in `~/.config/kind-miner` and its mining data in `~/.local/share/kind-miner`. Until you delete both, a reinstall starts mining again with your old wallet address, without showing the setup screen. `rm -r ~/.config/kind-miner ~/.local/share/kind-miner` deletes them.
