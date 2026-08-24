# Architecture

`git-repo-tracker` is a Go repo-tracking backend with platform frontends. The primary desktop frontend is a [Fyne](https://fyne.io) v2 tray app; a headless CLI uses the same backend; and the optional GNOME Shell extension renders a separate GNOME-native panel frontend on top of the CLI boundary. State flows one way: **discover → refresh → cache → notify the frontend**.

For the user-facing split between macOS, native Linux, and GNOME, see [Platform Integrations](PLATFORM_INTEGRATIONS.md).

## Layers

| Layer | Package(s) | Responsibility |
|-------|-----------|----------------|
| Frontends | `cmd/git-repo-tracker`, `cmd/git-repo-tracker-cli`, `internal/ui` | Fyne desktop app and headless CLI entrypoints |
| Optional integrations | `integrations/gnome-shell` | GNOME Shell panel frontend; shells out to the CLI |
| Backend | `internal/backend` | Shared service boundary over config + monitor for GUI, CLI, and future platform frontends |
| GUI | `internal/ui` | Tray menu, search popover, settings window, theme, tooltips, click actions; macOS-native bits (cgo) |
| Daemon | `internal/monitor` | Repo registry, refresh schedulers, branch service, activity tracking, bounded worker pool, debounced notifications, on-disk cache |
| API | `internal/git`, `internal/scan` | Repo and branch git operations; filesystem discovery |
| Persistence | `internal/config` | YAML config, including roots, palette, and IDE choice — mutex-guarded, atomic writes, rollback on failure |
| Desktop helpers | `internal/ide`, `internal/loginitem` | Editor discovery/launch and launch-at-login integration |

`cmd/git-repo-tracker` wires version/flags → loads backend config → constructs `ui.App` → runs. On macOS that app adds a small Cocoa bridge for menu-bar behavior. On Linux it uses the native tray/AppIndicator path for a compact menu plus a full Fyne window. `cmd/git-repo-tracker-cli` uses `internal/backend` without importing Fyne. The GNOME Shell extension shells out to that CLI and renders GNOME-native panel menu widgets, which is how it can support richer panel UI without AppIndicator limits.

The monitor exposes two callback seams: debounced `onChange` notifications for
registry changes and faster `onActivity` notifications for progress. The UI
marshals both onto Fyne's main thread with `fyne.Do`; it then pulls immutable
snapshots from the backend. Capacity-one signal channels implement the two
debouncers inside `internal/monitor`, but channels do not cross package layers.

```
cmd/git-repo-tracker ─▶ ui.App ─▶ backend.Service ─▶ monitor.Manager ─▶ git / scan
                         ▲              │                    │
                         └── onChange / onActivity ◀─────────┘   (debounced; via fyne.Do)

cmd/git-repo-tracker-cli ─▶ backend.Service ─▶ monitor.Manager ─▶ git / scan
```

## Data flow & threading

1. **Startup is cache-first.** `monitor.New` loads `state.json` into the in-memory registry, and `Start` fires `onChange` immediately — so the tray/popover paint the last-known state *before* any git process runs.
2. **Two schedulers** run as background goroutines (`monitor.go`):
   - **localLoop** (default every 30 s) — cheap `git status --porcelain=v2 --branch` for every repo: branch, upstream, ahead/behind, dirty counts.
   - **remoteLoop** (default every 30 min, or on demand) — re-discovers repos, then `git fetch` each fetch-eligible repo and recomputes behind + line stats. Both re-read their interval from config each cycle, so settings changes apply without a restart.
3. **Bounded concurrency.** A refresh feeds repo paths through a `jobs` channel to a fixed pool of workers (`refreshWorkers = 8`), so scanning dozens of repos never forks hundreds of git processes at once. On cancellation the workers drain the channel and exit.
4. **Debounced notifications.** Per-repo state updates are coalesced into at most
   one `onChange` per ~200 ms. Activity changes use a separate ~100 ms path so
   the UI can repaint only its status line with the active operation, progress,
   repository name, and short-lived completion messages.
5. **Locking.** The registry map is guarded by a `sync.RWMutex`, and the UI reads
   immutable `Snapshot()` copies. A mutex keyed by repository path serializes
   scheduled fetches, explicit fetch/pull, and branch fast-forward, push, merge,
   or tracking operations for that repository while allowing different
   repositories to proceed concurrently. All UI view-state is main-thread-only.

## The git layer (`internal/git`)

We shell out to the user's `git` binary rather than use a pure-Go library, so we inherit their SSH keys / credential helpers and match C-git's speed.

- **`GetStatus`** runs one `git status --porcelain=v2 --branch` — branch, upstream, ahead/behind (`branch.ab`), and per-file staged/modified/deleted/untracked counts, all from a single ~0 ms call.
- **`DiffStat`** uses a *three-dot* diff (`HEAD...<ref>`) — the merge-base→upstream change, i.e. exactly the lines you'd pull in.
- **`Fetch`** updates remote-tracking refs only (never the working tree). **`Pull`** is `--ff-only` — it never merges or leaves a conflicted state. Repositories opted into **Keep fresh** are fetched during remote passes regardless of their root's `autoFetch` setting, then pulled when their refreshed status is behind.
- **Branch operations** list local plus remote-only branches, preserve each local
  branch's full configured upstream ref, and resolve the target again before
  acting. A non-checked-out branch fast-forwards by updating its ref; a checked-
  out branch advances in its own checkout or linked worktree. Diverged branches
  merge only when a worktree exists, and pushes are never forced. Opening a
  branch in the selected editor reuses that checkout or atomically asks Git to
  create one at `<repo-parent>/.worktrees/<repo-name>/<branch-name>`; remote-only
  rows create their local tracking branch in the same worktree operation. The
  scanner prunes nested `.worktrees` directories so app-managed checkouts do not
  enter the repository registry a second time.
- **Worktree inspection** parses `git worktree list --porcelain -z`, excludes the
  primary checkout, then reads linked-checkout status through the same bounded
  worker count used by repository refresh. It is read-only, runs under the
  repository operation lock, and includes detached, locked, and prunable entries.
- **`exec.go`** resolves `git` once and augments `PATH` (`/opt/homebrew/bin`, …) plus sets `GIT_TERMINAL_PROMPT=0`, so background fetches fail fast instead of hanging on a credential prompt, even when launched from Finder/launchd.
- **Command tracing** also lives at that single execution boundary. Every
  completed invocation contributes an immutable record (executable, redacted
  argv, repository path, duration, exit code, and redacted stderr) to a
  process-local ring buffer capped at 500 entries. Stdout and environment values
  are never retained. A lightweight callback lets the UI repaint the open Git
  Console without coupling `internal/git` to Fyne.
- **`ConfigPath`** asks Git for `--git-path config` instead of guessing
  `<repo>/.git/config`, so the repository Info action resolves both ordinary
  checkouts and the shared configuration used by linked worktrees correctly.

## Discovery (`internal/scan`)

`Discover` walks each root concurrently with `filepath.WalkDir`, pruning aggressively: it skips ignored directory names *first*, then on finding a `.git` entry records the repo and stops descending (so submodules / nested clones aren't double-counted), and honors a per-root depth limit.

## Caching (`internal/monitor/cache.go`)

The discovered repos + last-known status are persisted as JSON in the OS cache dir (`~/Library/Caches/git-repo-tracker/state.json`). It's disposable — corrupt or version-mismatched cache is ignored and rebuilt. Writes use a unique `os.CreateTemp` + atomic rename, so the two refresh loops can both persist without clobbering a shared temp file. `BranchList` and `WorktreeList` values are intentionally excluded: they are loaded on demand, become stale when refs or linked checkouts move, and stay in UI memory only.

## Config (`internal/config`)

YAML, hand-editable, mutex-guarded. It stores scan/poll behavior plus appearance
(`theme` + `palette`) and the chosen editor (`ide` + `extraIDEs`). Mutations go
through `update()`, which snapshots the persistable fields, applies the change,
writes atomically, and **rolls back the in-memory state if the write fails** — so
memory never diverges from disk. Editor discovery and launch stay in
`internal/ide`; config stores only stable identifiers. See
[CONFIGURATION.md](CONFIGURATION.md).

## UI specifics (`internal/ui`)

<p align="center">
  <img src="media/screenshot.png" alt="The search popover" width="340">
  &nbsp;&nbsp;
  <img src="media/linux-native-tray.svg" alt="Ubuntu native tray menu" width="360">
</p>

Fyne is great for cross-platform widgets but lacks a few things a menu-bar app wants; these are the non-obvious bits:

- **Tray popover.** A native tray menu can't host a text field, so the popover is a borderless **splash window**. Left-click toggles it (`systray.SetOnTapped`), right-click shows the menu; the search field is the window's content.
- **Linux native tray.** AppIndicator menus are intentionally kept limited: a snapshot of outdated repos plus one `Open App` item that opens the real Fyne UI. Search, settings and config editing live in the app window rather than pretending the native menu can host them.
- **UI subpackages.** `internal/ui` owns Fyne app state and orchestration. Leaf UI concerns that do not need App state live in subpackages: `internal/ui/actions` for open-folder/editor/terminal commands and `internal/ui/trayicon` for tray icon resource rendering.
- **macOS native helpers** (`native_darwin.go`, cgo). Fyne exposes no window positioning, transparency, or focus-lost callback, so a small Cocoa shim: positions the popover under the cursor at the top of the screen, rounds the window corners (transparent corners via a content-layer mask + shadow), and observes `NSApplicationDidResignActive` to dismiss the popover on click-away. The `//export`ed Go callback lives in `native_darwin_export.go` (a cgo file using `//export` may only have declarations in its preamble). `native_other.go` provides no-op stubs for non-macOS builds.
- **Grouped list rows** (`browser.go`, `grouprow.go`, `repo_row.go`) use one
  virtualized `widget.List` grouped by scan root. Repository rows have a fixed
  two-line summary (name, then branch/status) so ordinary rows stay aligned;
  only the expanded repository grows through `list.SetItemHeight` for inline
  detail. Action icons are deliberately *not* Hoverable — the row hit-tests the
  pointer so moving onto an icon cannot make the row's hover actions disappear.
- **Worktrees, branches, and search** (`branches.go`, `branchindex.go`) keep
  expanded `WorktreeList` and `BranchList` state lazy and UI-owned. Linked
  worktrees render under their canonical repository and are re-read on reopen
  or explicit refresh. A separate bounded background index reads
  branch names for all repositories when the popover opens, allowing search to
  find branches that were never expanded; ref movement invalidates both views.
- **Status line** (`statusline.go`) reduces monitor activity and completion
  events to one footer line. Its faster `onActivity` callback avoids rebuilding
  the grouped list for progress-only changes.
- **Git Console and repository Info** (`app.go`, `browser.go`, `ide.go`,
  `repo_row.go`) stay on the existing UI → monitor → git boundary. Config-path
  resolution runs off the main thread, then the chosen editor opens the file;
  console refreshes marshal the trace callback through `fyne.Do`. The console
  window owns only its query and display state, while the git layer owns the
  bounded memory-only records.
- **Marquee** (`marquee.go`) scrolls overflowing path/commit-message lines on hover by sliding a substring window — since it always renders a *fitting* substring, no clipping is needed.
- **Tooltips** (`tooltip.go`) are a non-intercepting overlay layered on top of the window content (Fyne 2.7 has no built-in tooltips, and `widget.PopUp` would capture clicks).
- **Design system** (`tokens.go`, `palettes.go`, `theme.go`) separates geometry
  tokens from semantic colour palettes. Slate, Ink, and Signal each derive light
  and dark variants; `theme: system` follows the OS while forced modes use the
  same palette semantics. Stock Fyne widgets and custom canvas controls consume
  the same tokens and colours.

## Build & release

- **`Taskfile.yml`** — `build`, `run`, `test`, `check` (fmt+vet+test), `bundle` (macOS `.app`), `icon` (regenerate `icon.png` from the SVG), `linux-deps`, `release-snapshot`.
- **`.goreleaser.yaml`** — macOS arm64 + amd64 (CGO; amd64 cross-built via `clang -arch x86_64`), lipo'd into one universal binary. The `.app` bundle and Homebrew cask are packaged by the release workflow (`build/macos/`), not GoReleaser (both are Pro-only there).
- **`.github/workflows`** — `test-and-tag` (test on macOS + Linux, then bump & push a SemVer tag from conventional commits) → `release` (GoReleaser universal binary + `git-repo-tracker.app` cask + a Linux amd64 archive).
