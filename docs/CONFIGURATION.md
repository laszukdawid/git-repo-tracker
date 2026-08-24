# Configuration

Configuration is a YAML file. It's hand-editable and also editable in-app via **Settings**. On Linux, use the tray's **Open App** item first, then the Settings button in the app window. Changes made in Settings are written atomically; hand-edits take effect via **Reload Config** where that action is exposed, or by restarting the app.

## File location

Resolved in this order:

1. `GIT_REPO_TRACKER_CONFIG` environment variable, if set (an explicit path).
2. The OS config directory, otherwise:
   - **macOS:** `~/Library/Application Support/git-repo-tracker/config.yaml`
   - **Linux:** `~/.config/git-repo-tracker/config.yaml`

On first launch, if no file exists, a starter config is written — seeded with a `~/projects` root if that directory exists.

## macOS folder access

macOS protects `Documents` and other user folders. Add a scan root with the folder button in Settings rather than typing `~/` and saving it: the native folder picker grants access to the chosen folder and avoids scanning every protected folder beneath the home directory.

If a previous version repeatedly prompts for Documents access, remove a broad `~/` root unless it is intentional, then reset the old permission record and relaunch:

```sh
tccutil reset SystemPolicyDocumentsFolder com.github.laszukdawid.git-repo-tracker
```

The next access request explains that the app scans only directories explicitly configured as repository roots.

## Options

```yaml
# Directories to scan for git repositories (recursively).
roots:
  - path: ~/projects   # ~ expands to your home directory
    depth: 5           # max sub-directory depth to descend (0 = unlimited)
    autoFetch: true    # run `git fetch` in the background for repos under this root

# Directory names pruned during discovery (never descended into, even if they
# themselves contain a .git).
ignore:
  - node_modules
  - vendor
  - .Trash
  - .cache
  - Library

# Repositories automatically fast-forwarded when a refresh finds them behind.
keepFresh:
  - ~/projects/example

# How often to run `git fetch` for each repo (minutes).
fetchIntervalMinutes: 30

# How often to refresh cheap local status — branch, ahead/behind, dirty (seconds).
localRefreshSeconds: 30

# What the per-row "open" icon does.
#   open-folder : reveal the directory in the OS file manager (default)
#   terminal    : open a terminal in the directory
#   editor      : open the directory in VS Code (`code`)
#   custom      : run customCommand below
clickAction: open-folder

# Used when clickAction is "custom". {path} is replaced with the repo path.
# Quotes are honored, e.g.  open -a "Visual Studio Code" {path}
# If {path} is omitted, the directory is appended as the final argument.
customCommand: ""

# Start the app automatically at login (macOS LaunchAgent / Linux XDG autostart).
launchAtLogin: false
```

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| `roots[].path` | string | — | `~` is expanded; relative paths are resolved to absolute |
| `roots[].depth` | int | `5` | Max depth below the root; `0` = unlimited |
| `roots[].autoFetch` | bool | `false` | Per-root: whether the remote loop fetches these repos |
| `ignore` | []string | `[node_modules, vendor, .Trash, .cache, Library]` | Pruned directory names |
| `keepFresh` | []string | `[]` | Repo paths opted into automatic `git pull --ff-only`; these repos are fetched even when their root has `autoFetch: false` |
| `fetchIntervalMinutes` | int | `30` | Remote (fetch) refresh cadence; ≤0 → default |
| `localRefreshSeconds` | int | `30` | Local status cadence; ≤0 → default |
| `clickAction` | string | `open-folder` | `open-folder` \| `terminal` \| `editor` \| `custom` |
| `customCommand` | string | `""` | Used only when `clickAction: custom` |
| `launchAtLogin` | bool | `false` | Reflects the actual login-item state |
| `theme` | string | `system` | `system` \| `light` \| `dark` |
| `palette` | string | `slate` | `slate` \| `ink` \| `signal` — the colour family; with `theme` above, six themes in all |

### Opening a repository in an editor

Hovering a row reveals an editor chip carrying the chosen editor's own icon.
Tapping it opens that repository; tapping the small chevron in its corner opens
the picker to choose a different one. Before it is set, the chip opens the picker.

The picker lists the editors found on this machine — `/Applications` and
`~/Applications` (including JetBrains Toolbox installs) on macOS, plus known
launchers on `PATH` — and a **Browse…** entry for anything it missed. The choice
is global, not per repository.

The chosen editor is re-checked every time the chip is drawn and every time the
picker is opened. One that has been deleted or moved is shown faded and named
followed by *(not found)*, and the chip opens the picker instead of failing on
click.

| Key | Type | Meaning |
|---|---|---|
| `ide` | string | The chosen editor: a `.app` path, or `cmd:<name>` for a launcher on `PATH` |
| `extraIDEs` | []string | Editors added through **Browse…** that discovery would not find on its own |

`clickAction: editor` now opens this editor too, rather than assuming VS Code's
`code` command is installed.

### Branches

Expanding a repository lists its branches under a collapsible **Branches (N)**
section. The listing is read only when you open it, so no extra git runs during
the background poll.

Branches are ordered the way they are looked for: the branch you are standing
on, then local branches with something to pull, the remaining local branches
(newest first), and finally branches that exist only on the remote. The branch
viewport is height-bounded and scrolls internally. At most 40 branches are
rendered at first; **Show more** adds the next page without letting a large
repository stretch the whole popover.

One two-way-arrow control syncs each local branch after fetching the repository
and re-reading its current state: it fast-forwards a behind branch, pushes an
ahead branch, or merges the upstream into a diverged branch. An existing local
branch always uses its configured upstream, which can be on a remote other than
`origin`. The icon stays the same because the command is always "sync"; its
tooltip names the exact operation the current state requires.

The editor control beside every branch opens the checkout that already holds it.
When the branch is checked out nowhere, the app creates a linked worktree at
`<repo-parent>/.worktrees/<repo-name>/<branch-name>`, checks the branch out
there, and opens that directory in the selected editor. Slash-separated branch
names keep their hierarchy, so `feature/login` ends in `feature/login`. A
remote-only row creates its local tracking branch and worktree together. An
existing destination is never overwritten; the refusal is shown under the
branch row. Discovery skips nested `.worktrees` directories automatically so
these linked checkouts do not appear as duplicate repositories; configuring a
`.worktrees` directory itself as a root still scans it explicitly.

A branch checked out nowhere can be fast-forwarded by updating its ref without a
checkout, so the working tree and `HEAD` do not move. A branch checked out in the
main checkout or any linked worktree is advanced there and is refused when
uncommitted changes would be overwritten. A diverged branch checked out nowhere
is blocked because a merge needs a working tree. Remote-only branches discovered
under `origin` are shown dimmed with a control that creates a local tracking
branch without checking it out.

A refused pull explains itself in red directly under the branch, and stays there
until dismissed.

The search box matches every indexed branch name, not only the repository's
name, path, or current branch. Typing a ticket number therefore finds the
repository holding that branch without opening its detail first. Branch names
are indexed in the background when the popover opens and re-read for a
repository whenever a fetch or pull moves its refs.

Listing, fast-forwarding, and remote-only branch creation are also available
headlessly:

```sh
git-repo-tracker-cli branches <repo-path> --json=false
git-repo-tracker-cli pull-branch <repo-path> <branch>
git-repo-tracker-cli track <repo-path> <branch>
```

### Themes

`theme` chooses light or dark (or follows the OS) and `palette` chooses the
colour family, so the two give six combinations. The families differ in what
colour *means*, not only in hue:

- **slate** — repository names are neutral; colour is spent only on status and
  on controls you can click.
- **ink** — a warm ground of paper and charcoal, with one gold serving as both
  the accent and the "behind" marker.
- **signal** — greyscale with exactly one colour, used only where you need to
  act. A clean repository shows no status glyph at all, and uncommitted changes
  are an outline rather than a filled dot.

A starter file is also checked in as [`config.example.yaml`](https://github.com/laszukdawid/git-repo-tracker/blob/main/config.example.yaml).

The row's **Keep fresh** icon toggles membership in this list. Pulls remain
fast-forward-only; a dirty or diverged repository that cannot be updated safely
is left unchanged and reports the pull error in its row.

## Environment variables

| Variable | Effect |
|----------|--------|
| `GIT_REPO_TRACKER_CONFIG` | Path to the config file (overrides the default location) |
| `GIT_REPO_TRACKER_CACHE` | Path to the state cache file (overrides the default location) |
| `GRT_VIBRANCY=1` | macOS only: enable the experimental `NSVisualEffectView` blur behind the popover (off by default; may not render through Fyne's canvas) |

## State cache

Discovered repos and their last-known status are stored **separately** from the config, as disposable JSON:

- Path: `GIT_REPO_TRACKER_CACHE` if set, else the OS cache dir (`~/Library/Caches/git-repo-tracker/state.json` on macOS).
- It exists so the UI can paint instantly on launch. Deleting it is harmless — it is rebuilt on the next refresh.

## Custom command examples

```yaml
clickAction: custom
customCommand: code {path}                       # VS Code at the repo
```
```yaml
customCommand: open -a "Visual Studio Code" {path}   # quoted app name (macOS)
```
```yaml
customCommand: idea                                  # no {path} → repo appended as last arg
```

The command is executed directly (no shell), so there is no shell-injection surface; `{path}` is substituted into the argument list and quotes only group arguments.
