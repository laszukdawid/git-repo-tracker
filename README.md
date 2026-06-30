# git-repo-tracker

A fast, cross-platform **system-tray app that watches your local git repositories**.
It discovers repos under directories you configure, periodically checks how far
each branch is behind its `origin`, and surfaces the results in the menu bar plus
a searchable popover — inspired by [RepoZ](https://github.com/awaescher/RepoZ).

<p align="center">
  <img src="docs/media/demo.gif" alt="git-repo-tracker in action" width="440">
</p>

## Features

- **Discovery** — recursively finds every git repo under your roots. The walk is
  pruned (it stops at each repo and skips `node_modules`, `vendor`, …), so even
  large trees scan in a few milliseconds.
- **Behind tracking** — a background `git fetch` (working tree never touched)
  keeps remote-tracking refs current, then each repo reports how many **commits**
  and **lines** it is behind.
- **Instant startup** — last-known state is cached to disk and painted before any
  git runs, so the UI is never blank or laggy.
- **Search popover** — left-click the tray icon for a borderless popover with a
  search field and a virtualized list (sort by name / most-behind; filter all /
  updatable / dirty).
- **Inline detail** — click a repo to expand its path and the latest local/origin
  commit times; hover to reveal **pull** (fast-forward) and **open** icons.
- **Update all** — one button fast-forwards every repo that's behind.
- **In-app settings** — manage roots, intervals, the open action, and
  launch-at-login without hand-editing YAML.

## Install

### Homebrew (macOS)

```sh
brew install --cask laszukdawid/tap/git-repo-tracker
```

The cask strips the Gatekeeper quarantine flag, so the unsigned binary runs
without an "unidentified developer" prompt.

### From source

Requires **Go 1.26+** and `git` on your `PATH`. On Debian/Ubuntu install the Fyne
build dependencies first with `task linux-deps`.

```sh
task run     # or: go run .
task build   # produces ./git-repo-tracker
```

## Usage

The app lives in the menu bar / system tray — it has no main window.

<p align="center">
  <img src="docs/media/screenshot.png" alt="The search popover" width="360">
</p>

| Action | Result |
|--------|--------|
| **Left-click** the tray icon | Open/close the search popover |
| **Right-click** the tray icon | Native menu: behind repos, Browse, Refresh, Settings, Quit |
| Type in the search box | Live-filter by name or branch |
| **Click a repo row** | Expand inline detail (path + latest commit times) |
| **Hover a row** | Reveal **⬇ pull** (fast-forward) and **📂 open** icons |
| **⬇ in the header** | Update all — pull every repo that's behind |
| **☰ in the header** | Sort / filter / Settings |
| `Esc` | Dismiss the popover |

### Status notation

`↑n` ahead · `↓n` behind · `≡` even with upstream · `+n` staged · `~n` modified ·
`-n` deleted · `?n` untracked · `Δ+a/-d` lines behind · `⚠` last fetch/status error.

## Configuration

Config is YAML, hand-editable, and editable in-app via **Settings**. It lives at
`~/Library/Application Support/git-repo-tracker/config.yaml` (macOS) or
`~/.config/git-repo-tracker/config.yaml` (Linux); on first launch a starter file
is written (seeded with `~/projects` if it exists).

See **[docs/CONFIGURATION.md](docs/CONFIGURATION.md)** for the full reference
(every field, environment variables, click actions, cache location).

## Documentation

- **[Configuration reference](docs/CONFIGURATION.md)** — every option, env vars, cache.
- **[Architecture](docs/ARCHITECTURE.md)** — layering, concurrency, caching, the Fyne/cgo internals.
- **[Contributing](CONTRIBUTING.md)** — dev setup, build/test/release workflow, code layout.

Run **`task docs`** to preview the site locally with
[MkDocs Material](https://squidfunk.github.io/mkdocs-material/) at
<http://localhost:8000> (uses [`uvx`](https://docs.astral.sh/uv/), no install needed).

## Notes & limitations

- **Translucency** — stock Fyne can't make a window truly see-through, so the
  "glass" look is a dark gradient rather than OS vibrancy. An experimental
  `NSVisualEffectView` blur is available behind `GRT_VIBRANCY=1` (macOS only).
- **Popover position** — anchors under the cursor at the top of the screen on
  macOS (Fyne/systray don't expose the tray icon's exact position). On
  Linux/Wayland, placement is left to the compositor.
- **Click-away dismiss** — works on macOS (app-deactivation observer); on
  Linux/Windows use `Esc` or click the tray icon again.

## License

[MIT](LICENSE) © Dawid Laszuk
