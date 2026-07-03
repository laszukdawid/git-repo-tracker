# Configuration

Configuration is a YAML file. It's hand-editable and also editable in-app via **Settings**. On Linux, use the tray's **Open App** item first, then the Settings button in the app window. Changes made in Settings are written atomically; hand-edits take effect via **Reload Config** where that action is exposed, or by restarting the app.

## File location

Resolved in this order:

1. `GIT_REPO_TRACKER_CONFIG` environment variable, if set (an explicit path).
2. The OS config directory, otherwise:
   - **macOS:** `~/Library/Application Support/git-repo-tracker/config.yaml`
   - **Linux:** `~/.config/git-repo-tracker/config.yaml`

On first launch, if no file exists, a starter config is written — seeded with a `~/projects` root if that directory exists.

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
| `fetchIntervalMinutes` | int | `30` | Remote (fetch) refresh cadence; ≤0 → default |
| `localRefreshSeconds` | int | `30` | Local status cadence; ≤0 → default |
| `clickAction` | string | `open-folder` | `open-folder` \| `terminal` \| `editor` \| `custom` |
| `customCommand` | string | `""` | Used only when `clickAction: custom` |
| `launchAtLogin` | bool | `false` | Reflects the actual login-item state |

A starter file is also checked in as [`config.example.yaml`](https://github.com/laszukdawid/git-repo-tracker/blob/main/config.example.yaml).

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
