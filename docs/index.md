# git-repo-tracker

A fast, cross-platform **system-tray app that watches your local git repositories**.
It discovers repos under directories you configure, periodically checks how far
each branch is behind its `origin`, and surfaces the results in the menu bar plus
a searchable popover — inspired by [RepoZ](https://github.com/awaescher/RepoZ).

<p align="center">
  <img src="media/demo.gif" alt="git-repo-tracker in action" width="440">
</p>

## How It Fits Together

`git-repo-tracker` has a shared Go backend and platform-specific frontends. The
backend discovers repositories, refreshes git state, caches snapshots, and applies
config. The frontend decides how that state appears on your desktop.

On macOS, the packaged app is a menu-bar utility with a Fyne UI and small Cocoa
helpers for native popover behavior. On Linux, the primary app uses the desktop's
tray/AppIndicator support for a compact native menu and opens the full Fyne UI for
search, settings, and repo actions. GNOME users can optionally run a separate
Shell extension that renders a GNOME-native panel menu powered by the same
headless CLI/backend.

See [Platform Integrations](PLATFORM_INTEGRATIONS.md) for the full split.

## Features

- **Discovery** — recursively finds every git repo under your roots, pruned so
  even large trees scan in milliseconds.
- **Behind tracking** — a background `git fetch` (working tree never touched)
  reports how many **commits** and **lines** each repo is behind.
- **Instant startup** — last-known state is cached and painted before any git runs.
- **Search popover** — left-click the tray icon for a searchable, virtualized list.
- **Inline detail** — click a repo to expand its path and latest commit times;
  hover to reveal **pull** (fast-forward) and **open** icons.
- **Update all** — one button fast-forwards every repo that's behind.
- **In-app settings** — roots, intervals, open action, and launch-at-login.

<p align="center">
  <img src="media/screenshot.png" alt="The search popover" width="320">
  &nbsp;&nbsp;
  <img src="media/linux-native-tray.svg" alt="Ubuntu native tray menu" width="340">
</p>

On Linux, the native AppIndicator menu stays intentionally modest: it lists repos
that are behind and provides **Open App** for the full searchable UI. The optional
GNOME extension is a separate Shell-native frontend for users who want a richer
GNOME panel menu.

## Install

### Homebrew (macOS)

```sh
brew install --cask laszukdawid/tap/git-repo-tracker
```

This installs **git-repo-tracker.app** into `/Applications`. It's a menu-bar app
(no Dock icon, no main window), so **launch it once** from Launchpad/Spotlight or
with `open -a git-repo-tracker`; its icon then appears in the menu bar and it
keeps running in the background. Turn on **Launch at login** in the in-app
**Settings** (☰ in the popover header) so it starts with you automatically. The
cask strips the Gatekeeper quarantine flag, so the unsigned app opens without an
"unidentified developer" prompt.

### From source

Requires **Go 1.26+** and `git`. On Debian/Ubuntu, `task linux-deps` first.

```sh
task run     # or: go run ./cmd/git-repo-tracker
task build   # produces ./git-repo-tracker
```

## Status notation

`↑n` ahead · `↓n` behind · `≡` even with upstream · `+n` staged · `~n` modified ·
`-n` deleted · `?n` untracked · `Δ+a/-d` lines behind · `⚠` last fetch/status error.

## Where next

- **[Configuration](CONFIGURATION.md)** — every option, environment variables, the cache.
- **[Platform Integrations](PLATFORM_INTEGRATIONS.md)** — macOS, native Linux, and GNOME frontend split.
- **[Architecture](ARCHITECTURE.md)** — layering, concurrency, caching, and the Fyne/cgo internals.
- Contributing & agent notes live at the repo root
  ([CONTRIBUTING.md](https://github.com/laszukdawid/git-repo-tracker/blob/main/CONTRIBUTING.md),
  [AGENTS.md](https://github.com/laszukdawid/git-repo-tracker/blob/main/AGENTS.md)).
