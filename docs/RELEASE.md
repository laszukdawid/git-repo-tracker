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

### macOS `.app` bundle & Homebrew cask

GoReleaser (OSS) only builds the **universal binary**. Building the `.app` bundle
and generating an `app` Homebrew cask are GoReleaser Pro features, so they are
done with a script in `.github/workflows/release.yml` instead:

```text
build/macos/package-app.sh   # wraps the binary in git-repo-tracker.app (LSUIElement)
build/macos/Info.plist        # bundle metadata; __VERSION__ substituted at package time
build/macos/icon.icns         # app icon (regenerate from icon.png with iconutil)
build/macos/cask.rb.tmpl      # the app cask; __VERSION__/__SHA256__ filled in, pushed to the tap
```

Validate the whole macOS chain locally (no publishing, no tag):

```sh
goreleaser release --snapshot --clean          # builds the universal binary into ./dist
task bundle                                     # or: build/macos/package-app.sh ... --out dist
open dist/git-repo-tracker.app                  # confirm it launches as a menu-bar agent (no Dock icon)
```

Confirm the rendered cask is well-formed with `brew style` (run it against a copy
placed under a tap's `Casks/` directory, where the cask RuboCop config applies).

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
- Confirm `build/macos/icon.icns` is current if `icon.png` changed.
- Confirm `dist/git-repo-tracker.app` launches as a menu-bar agent (no Dock icon).
- Confirm no generated `site/` or `dist/` output or local binaries are staged.
