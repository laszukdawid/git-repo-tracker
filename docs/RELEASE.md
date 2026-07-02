# Release Checklist

Use this before cutting the first public release.

## Scope

The primary release artifact is the native Fyne tray app:

```text
cmd/git-repo-tracker
```

The headless CLI is built from:

```text
cmd/git-repo-tracker-cli
```

The GNOME Shell frontend is optional and remains an integration in this repo for
now:

```text
integrations/gnome-shell
```

## Local Verification

Run the standard gate:

```sh
gofmt -w .
go build ./...
go vet ./...
go test ./...
```

Check docs and optional integration syntax:

```sh
node --check integrations/gnome-shell/extension.js
uvx --with mkdocs-material mkdocs build --strict
```

Build and smoke-check the native app/CLI entrypoints:

```sh
task build
go run ./cmd/git-repo-tracker --help
go run ./cmd/git-repo-tracker-cli status --json
```

Use isolated config/cache paths when testing CLI refreshes:

```sh
GIT_REPO_TRACKER_CONFIG=/tmp/grt-cli.yaml \
GIT_REPO_TRACKER_CACHE=/tmp/grt-cli-state.json \
go run ./cmd/git-repo-tracker-cli status --refresh --json
```

Only run `--fetch` when you intentionally want network access:

```sh
go run ./cmd/git-repo-tracker-cli status --refresh --fetch --json
```

## Release Configuration

Validate GoReleaser when it is installed:

```sh
goreleaser check
```

Expected native app build path:

```text
.goreleaser.yaml -> ./cmd/git-repo-tracker
.github/workflows/release.yml -> ./cmd/git-repo-tracker
```

## Platform Notes

- macOS is the primary native tray experience: left-click opens the rich popover,
  right-click shows the native repo menu.
- Linux AppIndicator native menus are intentionally limited to outdated repos and
  **Open App**. Search, settings and inline detail live in the app window.
- GNOME Shell integration is optional and packaged separately from the native app;
  it depends on `git-repo-tracker-cli`.

## Before Tagging

- Confirm `README.md` screenshots render.
- Confirm `docs/` builds with `mkdocs build --strict`.
- Confirm `config.example.yaml` matches documented config fields.
- Confirm `go.mod` module path is `github.com/laszukdawid/git-repo-tracker`.
- Confirm no generated `site/` output or local binaries are staged.
