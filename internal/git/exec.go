package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

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

// Env returns the current environment with commonBinDirs ensured on PATH and
// interactive credential prompts disabled. Disabling prompts (GIT_TERMINAL_PROMPT=0)
// is essential for background fetches: an auth-required remote must fail fast
// rather than block a worker waiting on a username/password that will never come.
func Env() []string {
	env := append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
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
