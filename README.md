# git-repo-tracker

A cross-platform system-tray app that watches the status of your local git
repositories. It discovers repos under directories you configure, periodically
checks how far each branch is behind its `origin`, and surfaces the results in
the menu bar plus a fast, searchable browser window — inspired by
[RepoZ](https://github.com/awaescher/RepoZ).

```
●  12 repos · 3 behind
   ────────────────────
   chat-api      ↓5  +120/-3
   blog          ↓3
   ai-platform   ↓2
   ────────────────────
   Browse Repos…
   Refresh Now
   …
```

## Features

- **Discovery** — recursively finds every git repo under your configured roots.
  The walk is pruned (it stops at each repo and skips `node_modules`, `vendor`,
  …), so even large trees scan in a few milliseconds.
- **Behind tracking** — a background `git fetch` (working tree never touched)
  keeps remote-tracking refs current, then each repo reports how many **commits**
  and **lines** it is behind by.
- **Search popover** — **left-click** the tray icon for a borderless popover with
  a search field on top and a live-filtered, virtualized list (sort by name /
  most-behind, filter all / updatable / dirty) that stays snappy with hundreds of
  repos. Press `Esc` to dismiss.
- **At-a-glance menu** — **right-click** the tray icon for a native menu listing
  the repos that are behind (most-behind first) plus utility actions.
- **Instant startup** — last-known state is cached to disk and painted before any
  git runs, so the UI is never blank or laggy.
- **Click to expand** — clicking a repo expands an inline accordion panel showing
  its path and the date/time of the latest local and origin commits.
- **Hover actions** — hovering a row reveals top-right icons: **pull**
  (fast-forward the repo to its origin, with an in-progress spinner) and **open**
  (run the configured click action — open the folder by default). The open
  action's behaviour is configurable (file manager / terminal / editor / custom).
- **In-app settings** — add/remove scanned directories (with depth + per-root
  auto-fetch), tune the refresh cadences, pick the click action, and toggle
  launch-at-login, without hand-editing YAML.
- **Glassy dark UI** — the popover uses a custom dark theme over a gradient
  backdrop and, on macOS, positions itself just under the menu bar.

## Status notation

`↑n` ahead · `↓n` behind · `≡` even with upstream · `+n` staged · `~n` modified ·
`-n` deleted · `?n` untracked · `Δ+a/-d` lines behind.

## Install

### Homebrew (macOS)

```sh
brew install --cask laszukdawid/tap/git-repo-tracker
```

The cask strips the Gatekeeper quarantine flag, so the unsigned binary runs
without an "unidentified developer" prompt.

### From source

Requires Go 1.26+ and `git` on your `PATH`. On Debian/Ubuntu, install the Fyne
build dependencies first with `task linux-deps`.

```sh
task run        # or: go run .
task build      # produces ./git-repo-tracker
task check      # fmt + vet + test
task bundle     # macOS .app bundle (after `task install-fyne`)
task icon       # regenerate icon.png from internal/assets/icon.svg
```

Releases are cut by [GoReleaser](.goreleaser.yaml) on a version tag: macOS
arm64/amd64 (CGO) plus a Homebrew cask, and a Linux amd64 archive
(see `.github/workflows`).

## Configuration

Config lives at (override with `GIT_REPO_TRACKER_CONFIG`):

- macOS: `~/Library/Application Support/git-repo-tracker/config.yaml`
- Linux: `~/.config/git-repo-tracker/config.yaml`

On first launch a starter file is written (seeded with `~/projects` if it
exists). See [`config.example.yaml`](config.example.yaml) for all options. The
tray's **Open Config File…** / **Reload Config** items let you edit it live.

The discovered repos and their last-known status are kept separately in a
disposable cache (`~/Library/Caches/git-repo-tracker/state.json` on macOS).

## Architecture

Four layers, communicating via a single debounced `onChange` callback rather than
channels between layers:

| Layer | Package | Responsibility |
|-------|---------|----------------|
| GUI | `internal/ui` | tray menu, search popover, settings window, click actions, theme |
| Daemon | `internal/monitor` | repo registry, local + remote refresh loops, on-disk cache |
| API | `internal/git`, `internal/scan` | git status/fetch wrappers; filesystem discovery |
| Persistence | `internal/config` | YAML config (mutex-guarded, atomic writes) |
| Platform | `internal/loginitem` | launch-at-login (macOS LaunchAgent / Linux autostart) |

The monitor runs two schedulers: a cheap **local** pass (branch, ahead/behind,
dirty state) on a short interval, and a **remote** pass (`git fetch` through a
bounded worker pool, then recompute behind/line stats) on a longer one.

## Notes & limitations

- **Translucency**: stock Fyne can't make a window truly see-through, so the
  "glass" look is a dark gradient rather than OS vibrancy. An experimental
  `NSVisualEffectView` blur is available behind `GRT_VIBRANCY=1` (macOS only).
- **Positioning**: the macOS popover anchors to the top-right under the menu bar
  (not pixel-aligned to the icon — Fyne/systray don't expose the icon's position).
  On Linux/Wayland, window placement is left to the compositor.
- **Click-away dismiss**: the popover auto-hides when you click outside it on
  macOS (via an app-deactivation observer). On Linux/Windows that hook isn't
  wired up yet — use `Esc` or click the tray icon again to dismiss.
