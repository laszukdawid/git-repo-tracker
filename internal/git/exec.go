package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const commandTraceLimit = 500

// CommandTrace is one completed Git subprocess. It contains the exact argv that
// was executed after credentials are redacted, but never stdout or environment
// values. Traces live in memory only and are discarded when the app exits.
type CommandTrace struct {
	ID         uint64
	StartedAt  time.Time
	Duration   time.Duration
	RepoPath   string
	Executable string
	Args       []string
	ExitCode   int
	Stderr     string
}

var commandTraceStore struct {
	sync.Mutex
	nextID uint64
	items  []CommandTrace
	notify func()
}

// SetCommandTraceNotify installs the lightweight callback fired after a trace
// is recorded. The callback must marshal UI work onto its own main thread.
func SetCommandTraceNotify(fn func()) {
	commandTraceStore.Lock()
	commandTraceStore.notify = fn
	commandTraceStore.Unlock()
}

// CommandTraces returns an immutable snapshot ordered oldest to newest.
func CommandTraces() []CommandTrace {
	commandTraceStore.Lock()
	defer commandTraceStore.Unlock()
	out := make([]CommandTrace, len(commandTraceStore.items))
	for i, item := range commandTraceStore.items {
		out[i] = item
		out[i].Args = append([]string(nil), item.Args...)
	}
	return out
}

// ClearCommandTraces drops the current session's history.
func ClearCommandTraces() {
	commandTraceStore.Lock()
	commandTraceStore.items = nil
	commandTraceStore.nextID = 0
	commandTraceStore.Unlock()
}

func recordCommandTrace(trace CommandTrace) {
	trace.Executable = redactTraceText(trace.Executable)
	trace.RepoPath = redactTraceText(trace.RepoPath)
	trace.Stderr = redactTraceText(trace.Stderr)
	trace.Args = append([]string(nil), trace.Args...)
	for i := range trace.Args {
		trace.Args[i] = redactTraceText(trace.Args[i])
	}

	commandTraceStore.Lock()
	commandTraceStore.nextID++
	trace.ID = commandTraceStore.nextID
	commandTraceStore.items = append(commandTraceStore.items, trace)
	if extra := len(commandTraceStore.items) - commandTraceLimit; extra > 0 {
		copy(commandTraceStore.items, commandTraceStore.items[extra:])
		commandTraceStore.items = commandTraceStore.items[:commandTraceLimit]
	}
	notify := commandTraceStore.notify
	commandTraceStore.Unlock()
	if notify != nil {
		notify()
	}
}

// CommandLine formats the invocation for display or copying. Arguments are
// shell-quoted for readability only; execution never goes through a shell.
func (t CommandTrace) CommandLine() string {
	parts := make([]string, 0, len(t.Args)+1)
	parts = append(parts, shellQuote(t.Executable))
	for _, arg := range t.Args {
		parts = append(parts, shellQuote(arg))
	}
	return strings.Join(parts, " ")
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' ||
			r >= '0' && r <= '9' || strings.ContainsRune("_@%+=:,./-", r))
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}

var (
	traceURLCredentials = regexp.MustCompile(`(?i)\b(https?://)[^/\s'\"]+@`)
	traceQuerySecret    = regexp.MustCompile(`(?i)([?&](?:access_token|token|password|passwd|secret)=)[^&\s'\"]+`)
	traceBearerSecret   = regexp.MustCompile(`(?i)(Authorization:\s*Bearer\s+)[^\s'\"]+`)
)

func redactTraceText(value string) string {
	value = traceURLCredentials.ReplaceAllString(value, `${1}***@`)
	value = traceQuerySecret.ReplaceAllString(value, `${1}***`)
	return traceBearerSecret.ReplaceAllString(value, `${1}***`)
}

// commonBinDirs are PATH entries that a login/Finder-launched macOS GUI process
// typically does NOT inherit (launchd gives a minimal PATH). git lives in one of
// these, and — crucially — so do the credential/SSH helpers git shells out to
// during a fetch. We both resolve git here and prepend these to the PATH of
// every git subprocess so its children resolve too.
var commonBinDirs = []string{
	"/opt/homebrew/bin",
	"/usr/local/bin",
	"/opt/local/bin",
	"/usr/bin",
	"/bin",
}

// gitPath is resolved once. exec.LookPath uses the process PATH at startup,
// which is minimal under launchd; the commonBinDirs fallback covers that case.
var gitPath = resolveGit()

func resolveGit() string {
	if p, err := exec.LookPath("git"); err == nil {
		if abs, err := filepath.Abs(p); err == nil {
			return abs
		}
		return p
	}
	for _, dir := range commonBinDirs {
		candidate := filepath.Join(dir, "git")
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return "git" // last resort; produces a clear "not found" error
}

// Binary returns the resolved git executable path.
func Binary() string { return gitPath }

// hardeningArgs are `-c key=value` overrides passed to every git invocation. A
// command-line -c has the highest configuration precedence, so it wins over the
// repository's own .git/config — the one config layer an attacker controls when
// they hand the user a repository. Each entry closes a code-execution path that
// git would otherwise take on behalf of a background status/fetch/pull:
//
//   - core.fsmonitor: a repo-local value is a command git runs on every `git status`.
//   - core.hooksPath: points hooks at an empty directory so no repo-supplied hook
//     (post-checkout, post-merge, ... on pull) ever runs from this process.
//   - protocol.ext.allow=never: ext:: remote URLs are shell commands; keep them
//     off even if a user's global config enabled them for interactive use.
//   - fetch.recurseSubmodules=no: do not follow a repo into submodules whose
//     URLs/config it also controls.
//
// Residual risk is documented in docs/SECURITY.md: remote.<name>.uploadpack,
// core.sshCommand and credential.helper in a repo-local config are still
// honoured by git and cannot be neutralised without breaking legitimate setups.
var hardeningArgs = []string{
	"-c", "core.fsmonitor=false",
	"-c", "core.hooksPath=" + emptyHooksDir(),
	"-c", "protocol.ext.allow=never",
	"-c", "fetch.recurseSubmodules=no",
	// A repo-local `true` would rewrite the commit-graph on every fetch we make —
	// including the per-branch fast-forwards, which are one click each.
	"-c", "fetch.writeCommitGraph=false",
}

// emptyHooksDir returns the path of an empty directory used as core.hooksPath.
// It is created once under the OS temp dir; an empty hooks directory is the
// unambiguous way to disable hooks (git silently finds none to run).
func emptyHooksDir() string {
	dir := filepath.Join(os.TempDir(), "git-repo-tracker-no-hooks")
	_ = os.MkdirAll(dir, 0o700)
	return dir
}

// Env returns the current environment with commonBinDirs ensured on PATH and
// interactive credential prompts disabled. Disabling prompts (GIT_TERMINAL_PROMPT=0)
// is essential for background fetches: an auth-required remote must fail fast
// rather than block a worker waiting on a username/password that will never come.
// GIT_OPTIONAL_LOCKS=0 keeps `git status` from taking the index lock, so the
// background poll never collides with the user's editor or a running commit.
func Env() []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0")
	current := os.Getenv("PATH")
	have := map[string]bool{}
	for _, d := range filepath.SplitList(current) {
		have[d] = true
	}
	var missing []string
	for _, d := range commonBinDirs {
		if !have[d] {
			missing = append(missing, d)
		}
	}
	if len(missing) == 0 {
		return env
	}
	sep := string(os.PathListSeparator)
	newPath := strings.TrimPrefix(current+sep+strings.Join(missing, sep), sep)

	out := make([]string, 0, len(env))
	replaced := false
	for _, e := range env {
		if strings.HasPrefix(e, "PATH=") {
			out = append(out, "PATH="+newPath)
			replaced = true
			continue
		}
		out = append(out, e)
	}
	if !replaced {
		out = append(out, "PATH="+newPath)
	}
	return out
}
