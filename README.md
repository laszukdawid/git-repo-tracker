# git-repo-tracker

A fast, cross-platform **system-tray app that watches your local git repositories**.
It discovers repos under directories you configure, periodically checks how
repositories and branches stand against their configured upstreams, and surfaces
the results in the menu bar plus a searchable popover — inspired by
[RepoZ](https://github.com/awaescher/RepoZ).

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
- **A status mark that says which state** — one glyph per repo, answering where
  it stands against its remote: **↓ pull**, **↑ push**, **↓↑ diverged**,
  **⚠ mid-merge/rebase or conflicts**, **✓ settled**. Uncommitted changes are a
  separate dot beside the name, so "twelve commits behind" and "I have local
  edits" are shown as the two different facts they are.
- **Inline detail** — click a repo to expand its path and the latest local/origin
  commit times; hover to reveal **pull** or **keep fresh**, plus **open**.
- **Update all** — one button fast-forwards every repo that's behind.
- **Open in your editor** — the row carries a chip with your editor's own icon.
  It finds what is installed (JetBrains, VS Code, Cursor, Zed, Xcode, …), remembers
  your choice, and re-checks each time that the editor is still there.
- **Per-branch sync and worktrees** — expand a repo to list its branches. One
  two-way sync control fetches first and then does whatever that branch needs:
  fast-forward when it is behind, **push** when it is ahead, merge its upstream
  in when it has diverged. Nothing is ever forced, and nothing is pushed unless
  you press the control.
  A branch you are not standing on fast-forwards **without a checkout** — your
  working tree and `HEAD` stay where they are. A branch checked out in a **linked
  worktree** is updated there, exactly as pulling in that worktree by hand would,
  and is refused if the update would overwrite uncommitted changes. A diverged
  branch that is checked out nowhere says so: a merge needs a working tree.
  The editor control opens the checkout that already holds a branch. If there is
  none, it creates one under
  `<repo-parent>/.worktrees/<repo-name>/<branch-name>` and opens that worktree in
  the selected editor. Remote-only rows create a tracking branch as part of the
  same operation.
  Branches with something to pull are listed first, then the rest of your local
  branches (newest first), then the remote-only ones; a repo with hundreds of
  branches pages them in rather than listing them all, and the section header
  reports how many are behind.
- **Keep fresh** — opt a synced repo into safe automatic fast-forward pulls when
  a later refresh finds remote commits.
- **In-app settings** — manage roots, intervals, the open action, and
  launch-at-login without hand-editing YAML.

## Install

### Homebrew (macOS)

```sh
brew install --cask laszukdawid/tap/git-repo-tracker
```

This drops **git-repo-tracker.app** into `/Applications`. It's a menu-bar
utility — no Dock icon and no main window — so after installing, **launch it once**:

- open it from **Launchpad** or **Spotlight** (⌘-Space → type "git-repo-tracker"), or
- run `open -a git-repo-tracker` in a terminal.

Its icon then appears in the menu bar (top-right). **Left-click** it for the
search popover, **right-click** for the menu. You don't need to keep a terminal
open — it keeps running in the background on its own.

To have it start automatically at login, turn on **Launch at login** in
**Settings** (open the popover, click **☰**). Then you never have to launch it by
hand again.

The cask strips the Gatekeeper quarantine flag on install, so the unsigned app
opens without an "unidentified developer" prompt (no Apple notarization needed).

### From source

Requires **Go 1.26+** and **Git 2.34+** on your `PATH`. Git 2.34 is the minimum
because branch updates pass `--no-auto-maintenance`; background fetches also use
`--no-write-fetch-head`, which was added in Git 2.29. On Debian/Ubuntu install
the Fyne build dependencies first with `task linux-deps`.

```sh
task run     # or: go run ./cmd/git-repo-tracker
task build   # produces ./git-repo-tracker
```

Headless/status CLI:

```sh
go run ./cmd/git-repo-tracker-cli status --json
go run ./cmd/git-repo-tracker-cli status --refresh --json
```

## Usage

The app lives in the menu bar / system tray — it has no main window.

<p align="center">
  <img src="docs/media/screenshot.png" alt="The search popover" width="360">
  &nbsp;&nbsp;
  <img src="docs/media/linux-native-tray.svg" alt="Ubuntu native tray menu" width="360">
</p>

### What the mark next to a repo means

| Mark | State | What to do |
|------|-------|------------|
| **↓** | behind its upstream | pull |
| **↑** | ahead of its upstream | push |
| **↓↑** | diverged — commits on both sides | use branch sync to merge when it is checked out; otherwise check it out and merge or rebase |
| **⚠** | mid-merge, mid-rebase, or unresolved conflicts | finish it or abort it; nothing else applies until then |
| **!** | the last fetch or status failed | read the message in the row's detail |
| **✓** | level with its upstream, nothing local | nothing |
| **•** *(beside the name)* | uncommitted changes | independent of the mark above — a repo can be behind **and** dirty |

Hovering any row spells all of it out in words, including how long ago it was
fetched and pulled.

| Action | Result |
|--------|--------|
| **Left-click** the tray icon | Open/close the search popover |
| **Right-click** the tray icon | Native menu. Linux shows outdated repos plus **Open App**. |
| Type in the search box | Live-filter by name, path, current branch **or any branch** — every word must match (`www sw67`). A repo found by a branch you cannot see says which one: `v2 · matched origin/v3-rewrite` |
| `↑` / `↓` | Move the keyboard highlight across repos (section headers are skipped); typing pre-selects the first match |
| `⏎` | Open the highlighted repo with the configured click action and close the popover |
| `⇥` | Expand / collapse the highlighted repo's inline detail |
| **Click a repo row, or its ▸ chevron** | Expand inline detail (path + latest commit times + **Branches**) |
| **Branches (N)** in the detail | List every local branch plus the remote-only ones; pull any of them, or create a local branch from a remote one |
| **↻ beside Branches (N)** | Fetch just this repo, on demand — this is what makes each branch's ahead/behind current, and it works even on a root with `autoFetch: false` |
| **Branch controls** | Always shown, no hover needed. **↓↑ sync** fetches and then pulls, pushes, or merges according to the branch's current state. The editor control opens its existing worktree or creates one beside the repo first. **+ create local** remains available when the branch exists only on the remote; **⧉ copy** copies the name. |
| **Show N more** under the list | Reveal another page of branches |
| **Hover a row** | Reveal **⬇ pull** when behind or **↻ keep fresh** when synced, plus **open in editor** and **📂 open folder** |
| **Editor chip** | Opens the repo in your editor; its corner chevron changes which editor |
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

- **[Documentation site](https://laszukdawid.github.io/git-repo-tracker/)** — published with GitHub Pages.
- **[Configuration reference](docs/CONFIGURATION.md)** — every option, env vars, cache.
- **[Architecture](docs/ARCHITECTURE.md)** — layering, concurrency, caching, the Fyne/cgo internals.
- **[GNOME extension development](docs/GNOME_EXTENSION.md)** — optional Ubuntu/Fedora integration.
- **[Release checklist](docs/RELEASE.md)** — first-release verification and packaging notes.
- **[Security](docs/SECURITY.md)** — threat model, the git hardening applied to every background call, residual risks, how to report.
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
- **Linux native tray** — AppIndicator menus are intentionally simple: they show
  outdated repos and **Open App**. Rich search, settings and inline detail live in
  the app window or optional GNOME Shell integration.
- **Click-away dismiss** — works on macOS (app-deactivation observer); on
  Linux/Windows use `Esc` or click the tray icon again.

## License

[MIT](LICENSE) © Dawid Laszuk
