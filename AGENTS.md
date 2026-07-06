# AGENTS.md

Guidance for AI coding agents working in this repo. See also:
[README.md](README.md), [CONTRIBUTING.md](CONTRIBUTING.md), and
[docs/ARCHITECTURE.md](docs/ARCHITECTURE.md). Most important extension is
[docs/DEVELOPER_FAQ.md](docs/DEVELOPER_FAQ.md) which should include and be updated whenever something frequently occuring is problematic.

## What this is

A Go + [Fyne](https://fyne.io) v2.7 system-tray app that discovers local git
repos, tracks how far each is behind `origin`, and shows them in a tray popover.
It is a **CGO app** (Fyne) — builds need a C toolchain. macOS-first, also Linux.

## The gate — run before claiming done

```sh
gofmt -w .            # or: task fmt
go build ./...
go vet ./...
go test ./...         # use `go test -race ./...` for monitor/concurrency changes
```

`task check` runs fmt+vet+test. The `-lobjc` linker warning on macOS is harmless
(filter it: `go build ./... 2>&1 | grep -v "duplicate libraries"`).

## Layout

```
cmd/git-repo-tracker/ native app flags/version/wiring
internal/config/      YAML config: load, atomic write + rollback (update()), accessors
internal/git/         git subprocess wrappers (status/fetch/pull/diff) + PATH/env (exec.go)
internal/scan/        concurrent filesystem discovery
internal/monitor/     daemon: registry, refresh loops, worker pool, JSON cache
internal/ui/          Fyne UI; subpackages actions/ and trayicon/ are leaf helpers
internal/loginitem/   launch-at-login
internal/assets/      icon.svg + generator (`go run ./internal/assets/gen` → icon.png)
```

Architecture, data flow, and threading model: **docs/ARCHITECTURE.md**.

## Conventions & gotchas (read before editing the UI)

- **Comments:** favour self-documenting code. Add a comment only for the non-obvious
  *why* (a gotcha, a subtle interaction) — never to narrate *what* the next line
  does. Keep them short.
- **Layering is one-way:** UI → `monitor` → `git`/`scan`. Only `config` and
  `monitor` touch disk. Never do git or filesystem work in the UI layer or inline
  in a click handler — it belongs in the monitor's goroutines/worker pool.
- **Fyne threading:** every UI mutation must run on the main thread. Marshal from
  background goroutines (and from systray/cgo callbacks) with `fyne.Do`. `fyne.Do`
  is async and safe to call even from the main thread; **never** call
  `fyne.DoAndWait` from the main goroutine (it panics). All App view-state
  (maps/slices, `popVisible`, …) is touched only on the main thread.
- **cgo (`native_darwin.go` / `native_darwin_export.go` / `native_other.go`):**
  - A file using `//export` may have **only declarations** in its C preamble —
    keep C *definitions* in `native_darwin.go`, exports in `native_darwin_export.go`.
  - Build a Go-owned C string into an `NSString` **before** any `dispatch_async`
    block; the Go side frees the `char*` on return (else use-after-free).
  - Every function added to `native_darwin.go` needs a matching no-op in
    `native_other.go`, or non-macOS builds break.
- **List rows (`repo_row.go`):** the hover action icons (`iconButton`) are
  deliberately **not** `Hoverable`. The row hit-tests the pointer in `MouseMoved`
  to highlight/tooltip them. Making them Hoverable re-introduces the
  "buttons vanish as you reach for them" bug (row gets `MouseOut`). Inline
  expansion grows the row via `list.SetItemHeight` from `listUpdate`.
- **Tooltips:** Fyne 2.7 has no built-in tooltips. Use `tooltipLayer` (a
  non-intercepting overlay). Do **not** use `widget.PopUp` for tooltips — it
  captures clicks.
- **Marquee (`marquee.go`):** scrolls by rendering a *fitting substring* (no
  clipping). Guard `visible > 0` before animating.
- **git (`internal/git`):** shell out to system `git` (inherits the user's
  credentials). `Pull` is `--ff-only` (never merges/conflicts); `Fetch` never
  touches the working tree; `exec.go` augments PATH and sets
  `GIT_TERMINAL_PROMPT=0` so background fetches fail fast instead of hanging.
- **Atomic writes:** config and cache write a unique temp file + rename.
  `config.update()` rolls back in-memory state if the write fails — don't mutate
  config fields and persist separately; go through `update()`/`Save()`.
- **Live settings:** the refresh loops re-read intervals from config each cycle —
  don't cache interval values in the loop.

## Testing

- `git`/`scan` tests build throwaway repos/trees in `t.TempDir()` and `t.Skip` if
  `git` is absent. git tests isolate config with `GIT_CONFIG_GLOBAL=/dev/null`,
  `GIT_CONFIG_SYSTEM=/dev/null`, and explicit author/committer env.
- Point config/cache at temp dirs with `GIT_REPO_TRACKER_CONFIG` /
  `GIT_REPO_TRACKER_CACHE` (`t.Setenv`) so tests never touch real state.
- Add a test alongside any bug fix or new parsing/discovery/concurrency logic.

## Verifying GUI changes (important: headless limits)

You **cannot** click the tray icon or screenshot the popover from a
non-interactive shell, and `screencapture` is blocked. To sanity-check a UI change
without a display:

1. Build a binary and launch it with a **temp config** that sets `autoFetch:
   false` so it does no network and never mutates the user's repos.
2. Optionally add a temporary debug hook in `app.Run` (gated on an env var) that
   `fyne.Do`s `a.showWindow` / `a.toggleExpand(a.visible[0])` to exercise render
   paths — then **remove it** before finishing.
3. Confirm the process stays alive a few seconds (no crash), then kill it.

```sh
cat > /tmp/grt-smoke.yaml <<'EOF'
roots: [{path: ~/projects, depth: 6, autoFetch: false}]
EOF
go build -o /tmp/grt ./cmd/git-repo-tracker && GIT_REPO_TRACKER_CONFIG=/tmp/grt-smoke.yaml /tmp/grt &
sleep 4; kill %1
```

**Never** trigger a real `git pull` / "update all" / fetch against the user's
actual repositories in a test or smoke run. Visual correctness (layout, colors,
hover/scroll feel) must be verified by the human — call that out explicitly.

## Before you finish

- Run the gate (fmt/build/vet/test; `-race` for concurrency).
- Refactors must be **behavior-preserving** unless a change was requested.
- Keep docs in sync: `README.md`, `docs/CONFIGURATION.md` (config fields/env vars),
  `docs/ARCHITECTURE.md` (design), `config.example.yaml`.
- Commit/push only when asked; messages follow Conventional Commits and stage
  explicit paths (no `git add -A`).
