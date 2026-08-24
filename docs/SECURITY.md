# Security

git-repo-tracker is a background process that runs `git` unattended inside
every repository it discovers. That makes its threat model different from an
interactive git session, and this page spells out what the app defends
against, what it deliberately does not, and how to report a problem.

## Threat model

| Actor | Capability | In scope? |
|---|---|---|
| A repository the user cloned or downloaded | Controls `.git/config`, hooks, remote URLs, submodules | **Yes** — the app must not execute code on that repo's behalf |
| Another local user on the same machine | Can place repos under a scanned root | Partly — git's own `safe.directory` check refuses repos owned by someone else |
| The network / remote server | Can serve arbitrary pack data | Handled by git itself (fsck, protocol limits) |
| The user's own config file and custom click command | Full control | **No** — the user is trusted; a custom command is whatever they typed |

The key point: git treats a repository's **own** `.git/config` as trusted.
Interactive users accept that because they chose to run git there. This app
runs `git status` every 30 seconds and `git fetch` every 30 minutes in every
repo under `~/projects` (or wherever the roots point), so a crafted repository
that merely *exists* under a root must not be able to run code.

## Hardening applied to every git invocation

All git calls go through one helper (`internal/git`) which prepends these
overrides. A command-line `-c` has the highest precedence in git's config
stack, so they win over the repository's `.git/config`:

| Override | Why |
|---|---|
| `core.fsmonitor=false` | A repo-local value is a command git runs on every `git status`. Verified by `TestRepoLocalConfigCannotExecute`. |
| `core.hooksPath=<empty dir>` | No repo-supplied hook (`post-checkout`, `post-merge`, …) runs from a background pull. |
| `protocol.ext.allow=never` | `ext::` remote URLs are shell commands. |
| `fetch.recurseSubmodules=no` | A repo is not followed into submodules whose URLs and config it also controls. |
| `--no-ext-diff --no-textconv` on `git diff` | Never runs a repo-configured `diff.external` or textconv helper. |
| `GIT_TERMINAL_PROMPT=0` | An auth-required remote fails fast instead of blocking a worker on a prompt. |
| `GIT_OPTIONAL_LOCKS=0` | Background status never takes the index lock, so it cannot collide with the user's editor or a running commit. |

Working-tree safety is independent of the above. Automatic updates use only
`git pull --ff-only`; interactive branch actions may fast-forward, push, merge,
create a tracking branch, or create a linked worktree. Pushes are never forced,
fast-forwards never discard commits, and worktree creation refuses an existing
destination. A conflicted interactive merge is left for the user to resolve or
abort; the app never rebases, stashes, resets, or deletes a worktree.

## Residual risk (known, not mitigated)

git offers no "ignore repo-local config" switch, and some keys cannot be
overridden without breaking legitimate setups:

- `remote.<name>.uploadpack` / `receivepack` — a repo can point these at a
  local executable, which git runs on fetch.
- `core.sshCommand`, `credential.helper`, `core.gitProxy` — same pattern.
  Overriding them would break users whose SSH/credential setup depends on them.
- `filter.<driver>.process` / `clean` / `smudge` — a repository can configure a
  custom content filter that Git runs while a user-requested pull, merge, or
  worktree creation updates files. Driver names come from `.gitattributes`, so
  they cannot be disabled generically without also breaking legitimate Git LFS
  and other filter-backed repositories.

These do not fire on the status poll. Remote and credential helpers can fire on
fetch/push; content filters can fire only after an explicit working-tree action.
Practical guidance:

- Keep `autoFetch` off for roots that hold untrusted clones (the per-root
  toggle in Settings), or point roots at your own project folders only.
  `~/Downloads` is a poor scan root.
- git's `safe.directory` protection still applies: repos owned by another OS
  user are refused outright.

## Other surfaces

- **Custom click command** — executed with `exec` (argv, no shell), so `{path}`
  is a plain argument and cannot inject. The command itself is the user's own.
- **Config / cache files** — written atomically via a private temp file and
  rename; the config lives in the OS config dir (mode `0600` via
  `os.CreateTemp`). No secrets are stored; git credentials are never touched.
- **Repository Info action** — user-initiated and resolves the repository's
  actual local config through Git before opening it in the selected editor. It
  does not parse, copy, or persist the config contents inside this app.
- **Git Console** — keeps at most 500 completed commands in process memory and
  discards them on exit. It stores no stdout or environment values. HTTP(S) URL
  userinfo, common secret query parameters, and bearer authorization values are
  redacted before a record enters the buffer. Repository paths and other stderr
  text remain visible in the window, so copied console output should still be
  reviewed before sharing.
- **Linux D-Bus single-instance** — the session-bus object exposes only
  `OpenApp` and `ShowSettings`. Any process in the user's session can call
  them; both are harmless.
- **Login item** — the LaunchAgent plist / XDG `.desktop` file contain only
  the app's own executable path, XML- and Exec-escaped respectively.
- **CI** — release workflows use the default `GITHUB_TOKEN` plus two PATs held
  as repository secrets. Actions are pinned by major tag; pinning to commit
  SHAs is a recommended follow-up.

## Dependencies

`govulncheck ./...` is the reference check. Run it before a release; the
`golang.org/x/*` modules are kept at the latest patch for this reason.

## Reporting

Please report suspected vulnerabilities privately through GitHub's
*Security → Report a vulnerability* on the repository rather than a public
issue. Include the git version (`git --version`), the OS, and a minimal
repository that demonstrates the problem if you have one.
