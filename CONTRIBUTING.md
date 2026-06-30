# Contributing

Thanks for your interest in git-repo-tracker! This is a Go + Fyne desktop app;
the notes below cover the dev setup and conventions.

## Prerequisites

- **Go 1.26+** and `git` on your `PATH`.
- A C toolchain (Fyne is a CGO app): Xcode command-line tools on macOS; on
  Debian/Ubuntu run `task linux-deps` (installs `gcc`, `libgl1-mesa-dev`,
  `xorg-dev`, `libxkbcommon-dev`).
- [Task](https://taskfile.dev) (optional but recommended) for the shortcuts below.
- [uv](https://docs.astral.sh/uv/) — only for `task docs` (runs MkDocs via `uvx`).

## Common commands

```sh
task run     # go run .
task build   # build ./git-repo-tracker
task test    # go test ./...
task check   # gofmt + go vet + go test    ← run before pushing
task fmt     # gofmt -w .
task vet     # go vet ./...
task icon    # regenerate icon.png from internal/assets/icon.svg
task bundle  # macOS .app bundle (after `task install-fyne`)
task docs    # serve the docs (MkDocs Material) at http://localhost:8000
```

Run the race detector when touching the monitor or any concurrency:

```sh
go test -race ./...
```

## Project layout

```
main.go                  flags, version, wiring
internal/
  config/                YAML config: load, atomic write + rollback, accessors
  git/                   git subprocess wrappers (status/fetch/pull/diff) + PATH/env
  scan/                  concurrent filesystem discovery of repos
  monitor/               daemon: registry, refresh loops, worker pool, cache
  ui/                    Fyne UI (tray, popover, rows, settings, theme, tooltips)
    native_darwin*.go    macOS-only cgo (positioning, rounded corners, auto-hide)
    native_other.go      no-op stubs for non-macOS
  loginitem/             launch-at-login (macOS plist / Linux .desktop)
  assets/                icon.svg + a generator (`go run ./internal/assets/gen`)
```

See [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) for how the layers fit together
and the threading/caching model.

## Conventions

- **Formatting:** `gofmt` (run `task fmt`); CI and `task check` enforce it.
- **Layering:** UI talks to the `monitor`; the `monitor` calls `git`/`scan`; only
  `config`/`monitor` touch disk. Keep git/filesystem work out of the UI layer.
- **Threading:** all Fyne UI mutations happen on the main thread — marshal from
  background goroutines (and the systray/cgo callbacks) with `fyne.Do`. UI
  view-state is only touched on the main thread.
- **No blocking the UI:** anything that shells out to git runs in the monitor's
  goroutines/worker pool, never inline in a click handler.
- **Comments** explain the *why* (especially the Fyne/cgo workarounds), not the
  obvious *what*.
- **macOS cgo:** a file using `//export` may contain only declarations in its C
  preamble — keep C definitions in `native_darwin.go` and exports in
  `native_darwin_export.go`. Always build a C string into an `NSString` before an
  async block (the Go side frees the original on return).

## Tests

- `config`, `git`, `scan`, and `monitor` have unit tests; `git`/`scan` build
  throwaway repos/trees in `t.TempDir()` and skip if `git` is absent.
- The cache and config honor `GIT_REPO_TRACKER_CACHE` / `GIT_REPO_TRACKER_CONFIG`
  so tests can point them at a temp dir. Use these to avoid touching real state.
- Please add a test alongside any bug fix or new parsing/discovery logic.

## Commits & releases

- Commit messages follow **Conventional Commits** (`feat:`, `fix:`, `chore:`,
  `feat!:` / `BREAKING CHANGE:` …). The `test-and-tag` workflow derives the
  version bump from them.
- On a push to `main` that passes tests, CI tags a new SemVer release, which
  triggers GoReleaser (macOS cask + Linux archive). You don't tag by hand.
- Before opening a PR: `task check` (and `go test -race ./...` for concurrency
  changes).
