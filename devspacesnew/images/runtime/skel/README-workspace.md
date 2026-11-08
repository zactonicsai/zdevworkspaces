# Your workspace

* `/workspace` → `~/workspace`, where repos are cloned. Everything under `/home/dev`
  (code, shell history, `~/.cargo`, `~/go`, `~/.npm`, `~/.cache`, venvs …) lives on your
  persistent volume and survives stop/start. Anything outside it (apt packages
  you install by hand, `/opt`, `/usr/local`) is re-created from the image on every start —
  put those in your image's `--tools` list or a dotfiles install script instead.
* `sudo` works without a password. You are root inside a user namespace, so
  host-level resources are still off limits.
* Logs from boot (tool installs, git clone): `ws-logs` or `/var/log/workspace/`.
* Multicast workspaces have a second interface (`$WORKSPACE_MCAST_IFACE`, usually `net1`);
  run `mcast-check` to verify sending and receiving.
