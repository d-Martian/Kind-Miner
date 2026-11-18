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
- **To remove the desktop app:**
  1. Quit kind-miner from its tray icon. A copy that is already running keeps running after the package is removed. To check it has stopped, run `pgrep -l 'kind-miner|xmrig|p2pool'`: it prints nothing once everything has quit.
  2. `sudo apt purge kind-miner` removes the program, the timer and the repository.
  3. `rm -r ~/.config/kind-miner ~/.local/share/kind-miner` deletes your settings and mining data, which the purge leaves in your home folder. Skip this step to keep them: a later reinstall then starts mining again with your old wallet address, without showing the setup screen.
- **To remove the headless service:** `sudo apt purge kind-minerd` removes everything: the program, both pairs of
  engines, the timer, the config, and the state — p2pool's data, the hub's
  certificate and tokens, the onion keys.

## Removing the AppImage

The AppImage is one file: the file you downloaded is the program itself. To get rid of kind-miner, delete that file and the few things it added:

1. Quit kind-miner from its tray icon. To check it has stopped, run `pgrep -l 'kind-miner|xmrig|p2pool'`: it prints nothing once everything has quit.
2. Delete the AppImage file and the `.sha256` file beside it, from the folder you downloaded them into.
3. `rm ~/.local/share/applications/kind-miner.desktop ~/.local/share/icons/hicolor/512x512/apps/kind-miner.png` deletes the menu entry and the icon it added the first time it ran.
4. `rm -r ~/.config/kind-miner ~/.local/share/kind-miner` deletes your settings and mining data. These folders only exist once you have completed the setup screen.

## Removing the Flatpak

1. Quit kind-miner from its tray icon. To check it has stopped, run `pgrep -l 'kind-miner|xmrig|p2pool'`: it prints nothing once everything has quit.
2. `flatpak uninstall --user --delete-data io.github.d_Martian.KindMiner` removes the program and its private data. Answer `y` when it asks whether to delete the data.
3. `rm -r ~/.config/kind-miner` deletes your settings. The Flatpak creates this folder the first time it starts, and uninstalling leaves it behind.
4. Delete the `.flatpak` file and the `.sha256` file beside it, from the folder you downloaded them into.
