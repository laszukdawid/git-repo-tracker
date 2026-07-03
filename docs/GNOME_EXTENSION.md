# GNOME Extension

The GNOME Shell extension is an optional Linux frontend, not the default Linux
tray integration. The default Linux app is the Fyne desktop app with a compact
native tray/AppIndicator menu and an **Open App** entry for the full searchable UI.
The extension exists for users and contributors who want a GNOME-native panel
menu instead.

For the broader split between macOS, native Linux, and GNOME, see
[Platform Integrations](PLATFORM_INTEGRATIONS.md).

The optional GNOME frontend is a Shell extension in `integrations/gnome-shell/`.
It renders the rich panel menu with GNOME Shell widgets and gets repo state from
the headless Go CLI:

```text
GNOME Shell extension -> git-repo-tracker-cli status --json -> internal/backend -> monitor/git/scan
```

This is intentionally separate from the Fyne app. AppIndicator/native tray menus
are reliable for a compact status menu, but they are a poor fit for searchable,
expandable, live-updating UI. GNOME Shell extensions can render that kind of panel
experience with Shell-native widgets.

The extension boundary is the CLI, not an internal Go package import. That keeps
the JavaScript extension small and lets the Go backend remain the single source of
truth for config, discovery, refresh behavior, and cache state.

## Prerequisites

On Ubuntu/Debian:

```sh
task gnome:deps
```

That installs GNOME extension development tools such as `gnome-extensions` and
`gjs`. Native Fyne app dependencies are installed separately with
`task linux-deps`.

You also need `node` for the syntax-only JavaScript check used by `task gnome:check`.

## First Install

Build the CLI bridge and clean-install the extension into GNOME's user extension
directory:

```sh
task gnome:install
```

This does three important things:

```text
1. Builds ~/.local/bin/git-repo-tracker-cli
2. Packs integrations/gnome-shell/ into /tmp/git-repo-tracker@laszukdawid.github.com.shell-extension.zip
3. Installs that bundle with gnome-extensions install --force
```

Then enable it:

```sh
task gnome:enable
```

If GNOME does not see the extension yet, log out/in. On X11 you can often restart
GNOME Shell with `Alt+F2`, type `r`, press Enter. On Wayland, log out/in is the
reliable reload path.

GNOME Shell 46 still exposes a `ReloadExtension` DBus method, but it returns
`ReloadExtension is deprecated and does not work` on current Ubuntu sessions, so
the development tasks do not rely on it. A new local extension may not appear in
`gnome-extensions list` until the shell process restarts.

## Development Loop

After changing `integrations/gnome-shell/extension.js`, `stylesheet.css`,
metadata, or the CLI/backend code:

```sh
task gnome:restart
```

This disables the extension, rebuilds the CLI bridge, reinstalls a fresh packed
extension bundle, and enables it again if the running Shell already knows about
the UUID. If Shell reports that the extension does not exist, log out/in and run
`task gnome:enable`.

For JS syntax only:

```sh
task gnome:check
```

For backend/CLI checks:

```sh
task check
go run ./cmd/git-repo-tracker-cli status --json
go run ./cmd/git-repo-tracker-cli status --refresh --json
```

Use `--fetch` only when you intentionally want network fetches:

```sh
go run ./cmd/git-repo-tracker-cli status --refresh --fetch --json
```

## Logs

Tail GNOME Shell logs filtered for this extension:

```sh
task gnome:logs
```

If that filter misses something, use the broader journal directly:

```sh
journalctl --user -f -o cat /usr/bin/gnome-shell
```

Common messages to look for:

```text
git-repo-tracker
git-repo-tracker@laszukdawid.github.com
Gjs-WARNING
JS ERROR
```

## CLI Path

By default the extension looks for:

```text
~/.local/bin/git-repo-tracker-cli
```

If you want to use a different binary, set `GIT_REPO_TRACKER_CLI` before GNOME
Shell starts:

```sh
export GIT_REPO_TRACKER_CLI=/absolute/path/to/git-repo-tracker-cli
```

For a persistent user-session variable, put it in a shell/session environment file
that your display manager reads, then log out/in.

## Config And Cache

The CLI honors the same environment variables as the desktop app:

```text
GIT_REPO_TRACKER_CONFIG=/path/to/config.yaml
GIT_REPO_TRACKER_CACHE=/path/to/state.json
```

If these are not set, it uses the default Linux locations:

```text
~/.config/git-repo-tracker/config.yaml
~/.cache/git-repo-tracker/state.json
```

## Uninstall Development Copy

```sh
task gnome:clean
```

This disables the extension if possible and removes the installed development copy
from `~/.local/share/gnome-shell/extensions/`.

## Troubleshooting

If the extension does not appear:

```sh
gnome-extensions list | grep git-repo-tracker
gnome-extensions info git-repo-tracker@laszukdawid.github.com
```

If enabling fails, check `metadata.json` shell-version compatibility and GNOME
Shell logs.

If the panel menu shows an error, verify the CLI works from the same user account:

```sh
~/.local/bin/git-repo-tracker-cli status --json
```

If repo data is stale, force a backend refresh:

```sh
~/.local/bin/git-repo-tracker-cli status --refresh --json
```

If the extension keeps old code, run `task gnome:restart`; if that still does not
update it, log out/in to force GNOME Shell to reload extension modules.
