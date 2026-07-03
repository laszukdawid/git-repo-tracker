# Platform Integrations

`git-repo-tracker` has one shared repo-tracking backend and several platform frontends. The backend discovers repositories, refreshes git state, persists the cache, and exposes snapshots. Platform integrations decide how that state is shown on a specific desktop.

This split is deliberate: macOS menu bar apps, Linux AppIndicator trays, and GNOME Shell extensions have different capabilities and restrictions.

## At A Glance

| Integration | Status | What it owns | Best for |
|-------------|--------|--------------|----------|
| macOS Fyne app + Cocoa helpers | Primary app | Menu bar item, rich popover, settings, launch-at-login | Normal macOS use |
| Linux native Fyne/AppIndicator app | Primary app | Tray icon, compact native menu, full Fyne app window | Desktop environments with tray/AppIndicator support |
| GNOME Shell extension | Optional frontend | GNOME-native panel menu powered by the CLI | Users who want a richer GNOME panel experience |

All three use the same config and cache model. The GNOME extension reads data through `git-repo-tracker-cli`; the Fyne apps call the backend directly.

## Shared Backend

The common path looks like this:

```text
config -> backend.Service -> monitor.Manager -> scan/git -> cache -> snapshots
```

The backend is responsible for work that should not live in UI code:

- discovering repositories under configured roots
- running `git status`, `git fetch`, `git pull --ff-only`, and diff stats
- limiting concurrency so refreshes do not spawn too many git processes
- writing the JSON cache and YAML config atomically
- notifying frontends when snapshots change

The UI layers should only render state and request actions. They should not walk the filesystem or shell out to git inline from a click handler.

## macOS Integration

On macOS the shipped app is a Fyne tray application packaged as `git-repo-tracker.app`. It behaves like a menu bar utility: no Dock icon, no main window, and a menu bar item that opens the repo popover.

Fyne provides most widgets and app lifecycle plumbing. A small Cocoa bridge fills in menu-bar-specific behavior that Fyne does not expose directly:

- positioning the borderless popover near the menu bar
- making the popover feel native with rounded transparent corners and shadow
- dismissing the popover when the app resigns focus
- integrating launch-at-login through a LaunchAgent

The Cocoa code is intentionally narrow and lives behind platform files in `internal/ui/native_darwin.go` and `internal/ui/native_darwin_export.go`. Non-macOS builds use no-op stubs from `internal/ui/native_other.go`, so the rest of the UI can call the same functions without platform checks everywhere.

## Native Linux Integration

On Linux the primary app is still the Fyne app, but the tray integration goes through the desktop's tray/AppIndicator support. That native tray menu is kept small on purpose: it shows a snapshot of repos that are behind and an **Open App** entry for the full UI.

The richer experience lives in the Fyne window rather than the native tray menu:

- searchable repo list
- expandable repo rows
- settings
- sort and filter controls
- update actions

This is a capability boundary, not just a design preference. AppIndicator-style menus are not a good host for text input, custom row layout, live expansion, or a complex settings surface. Keeping the native menu compact makes the Linux tray reliable across desktop environments while preserving the full app UI where Fyne can render it consistently.

Launch-at-login on Linux uses XDG autostart through `internal/loginitem`.

## GNOME Shell Extension

GNOME is a special case. Modern GNOME often hides or de-emphasizes traditional tray/AppIndicator UI, while GNOME Shell extensions can render native panel menus with custom layout and live updates.

The optional extension in `integrations/gnome-shell/` is therefore separate from the Fyne Linux tray integration. It does not import Go code directly. Instead, it shells out to the headless CLI:

```text
GNOME Shell extension -> git-repo-tracker-cli status --json -> backend.Service
```

That boundary keeps the extension small and GNOME-native while preserving one source of truth for repo discovery, refresh behavior, config, and cache.

Use the GNOME extension when you specifically want a GNOME panel frontend. Use the native Linux app when you want the cross-desktop Fyne app with a compact tray menu.

See [GNOME Extension](GNOME_EXTENSION.md) for development and install details.

## Choosing A Frontend

Use the macOS app on macOS. It is the primary packaged experience and includes the native menu-bar polish needed for click-away behavior and popover placement.

Use the native Linux app on Linux desktops where a tray/AppIndicator area is available. The tray menu is intentionally compact, and **Open App** brings up the full searchable UI.

Use the GNOME extension only if you want a Shell-native panel menu. It is an optional frontend layered over the CLI, not a replacement for the shared backend.

## Development Notes

When adding a platform feature, keep the boundary in mind:

- backend behavior belongs in `internal/backend`, `internal/monitor`, `internal/git`, `internal/scan`, or `internal/config`
- shared Fyne UI behavior belongs in `internal/ui`
- macOS-only window/menu-bar behavior belongs in the `native_darwin` files with matching non-macOS stubs
- Linux autostart behavior belongs in `internal/loginitem`
- GNOME Shell UI behavior belongs in `integrations/gnome-shell/` and should use `git-repo-tracker-cli` as its data boundary

This keeps platform code small and prevents the same repo-monitoring rules from being reimplemented in multiple frontends.
