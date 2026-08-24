// Package git provides read-only inspection of local git repositories plus the
// single mutating operation we need — fetch — by shelling out to the user's git
// binary. Going through git (rather than a pure-Go library) means we inherit
// whatever SSH keys and credential helpers already work on the command line and
// match C-git's speed on large working trees.
package git

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// readTimeout bounds cheap local queries so a wedged repo can't hang the UI.
	readTimeout = 20 * time.Second
	// fetchTimeout is generous because fetch hits the network.
	fetchTimeout = 2 * time.Minute
)

// Status is a snapshot of a repository's local state, parsed from a single
// `git status --porcelain=v2 --branch` invocation.
type Status struct {
	Branch   string // current branch name, or "detached"
	Upstream string // tracking ref, e.g. "origin/main"; empty if none
	Detached bool
	Ahead    int // commits HEAD is ahead of upstream
	Behind   int // commits HEAD is behind upstream

	Staged    int // entries with a staged (index) change
	Modified  int // worktree-modified entries
	Deleted   int // worktree-deleted entries
	Untracked int // untracked entries
	Conflicts int // unmerged entries — a merge or rebase left them to resolve

	// Operation is the multi-step git operation this repository is in the middle
	// of, if any: "merge", "rebase", "cherry-pick", "revert" or "bisect".
	Operation string
}

// Unsettled reports whether the repository is mid-operation or holding
// conflicts — a state that has to be finished or aborted before anything else,
// and one no amount of pulling will help.
func (s Status) Unsettled() bool { return s.Operation != "" || s.Conflicts > 0 }

// Dirty reports whether the working tree or index has any local changes.
func (s Status) Dirty() bool {
	return s.Staged+s.Modified+s.Deleted+s.Untracked > 0
}

// GetStatus runs `git status --porcelain=v2 --branch` and parses the result.
func GetStatus(ctx context.Context, repoPath string) (Status, error) {
	out, err := run(ctx, readTimeout, repoPath, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return Status{}, err
	}
	st := parseStatus(out)
	st.Operation = InProgress(repoPath)
	return st, nil
}

// InProgress names the multi-step operation the repository is in the middle of,
// or "" when it is not in one.
//
// It reads the marker files directly rather than running git: porcelain=v2 does
// not report the operation at all, and the alternative — parsing the prose of
// `git status --long` — would break on a translated or reworded git. The files
// have been stable for the whole of git's modern history, and this runs for
// every repository on every refresh, so not spending a process on it matters.
func InProgress(repoPath string) string {
	dir := gitDir(repoPath)
	if dir == "" {
		return ""
	}
	// Order matters: a rebase that stops on a conflict also writes MERGE_MSG, and
	// an interactive rebase is the more specific truth.
	for _, c := range []struct{ marker, name string }{
		{"rebase-merge", "rebase"},
		{"rebase-apply", "rebase"},
		{"MERGE_HEAD", "merge"},
		{"CHERRY_PICK_HEAD", "cherry-pick"},
		{"REVERT_HEAD", "revert"},
		{"BISECT_LOG", "bisect"},
	} {
		if _, err := os.Stat(filepath.Join(dir, c.marker)); err == nil {
			return c.name
		}
	}
	return ""
}

// gitDir resolves a repository's git directory without running git. In a linked
// worktree .git is a file holding "gitdir: <path>", and that path is where the
// operation markers for THAT worktree live — the shared directory's markers
// belong to a different checkout entirely.
func gitDir(repoPath string) string {
	p := filepath.Join(repoPath, ".git")
	fi, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if fi.IsDir() {
		return p
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	rest, ok := strings.CutPrefix(line, "gitdir:")
	if !ok {
		return ""
	}
	rest = strings.TrimSpace(rest)
	if !filepath.IsAbs(rest) {
		rest = filepath.Join(repoPath, rest)
	}
	return rest
}

// DefaultBranch returns the remote-tracking ref the origin's HEAD points at
// (e.g. "origin/main"), used as a comparison point when the current branch has
// no upstream configured.
func DefaultBranch(ctx context.Context, repoPath string) (string, error) {
	out, err := run(ctx, readTimeout, repoPath, "symbolic-ref", "--short", "refs/remotes/origin/HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// DiffStat returns the inserted/deleted line counts of the incoming changes
// between HEAD and ref, using a three-dot diff (merge-base(HEAD,ref)..ref) so it
// measures exactly what you would pull in — the lines you are behind by.
func DiffStat(ctx context.Context, repoPath, ref string) (added, deleted int, err error) {
	// --no-ext-diff / --no-textconv: never run a repo-configured diff.external or
	// diff.<driver>.textconv helper from a background process.
	out, err := run(ctx, readTimeout, repoPath, "diff", "--shortstat", "--no-ext-diff", "--no-textconv", "HEAD..."+ref)
	if err != nil {
		return 0, 0, err
	}
	a, d := parseShortstat(string(out))
	return a, d, nil
}

// CountBehind returns how many commits ref has that HEAD does not (HEAD..ref).
// Used when the current branch has no configured upstream and we compare against
// the remote's default branch instead.
func CountBehind(ctx context.Context, repoPath, ref string) (int, error) {
	out, err := run(ctx, readTimeout, repoPath, "rev-list", "--count", "HEAD.."+ref)
	if err != nil {
		return 0, err
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return n, nil
}

// Fetch updates the repository's remote-tracking refs. It never touches the
// working tree or local branches, so it is safe to run in the background.
// --no-write-fetch-head matters for politeness rather than safety: without it
// every background fetch overwrites the user's own .git/FETCH_HEAD, so their
// `git fetch && git merge FETCH_HEAD` would silently merge our ref instead.
func Fetch(ctx context.Context, repoPath string) error {
	_, err := run(ctx, fetchTimeout, repoPath, "fetch", "--quiet", "--no-write-fetch-head")
	return err
}

// Pull fast-forwards the current branch to its upstream. --ff-only guarantees we
// never create a merge commit or leave the repo in a conflicted state: if the
// local branch has diverged, the pull fails cleanly and the caller surfaces it.
func Pull(ctx context.Context, repoPath string) error {
	_, err := run(ctx, fetchTimeout, repoPath, "pull", "--ff-only", "--quiet")
	return err
}

// CommitInfo describes a single commit.
type CommitInfo struct {
	Hash    string
	Time    time.Time
	Subject string
}

// LastCommit returns the most recent commit reachable from ref (e.g. "HEAD" or
// "origin/main") with its committer time and subject. Fields are NUL-separated
// so a subject containing spaces parses cleanly.
func LastCommit(ctx context.Context, repoPath, ref string) (CommitInfo, error) {
	out, err := run(ctx, readTimeout, repoPath, "log", "-1", "--format=%cI%x00%h%x00%s", ref)
	if err != nil {
		return CommitInfo{}, err
	}
	parts := strings.SplitN(strings.TrimRight(string(out), "\n"), "\x00", 3)
	if len(parts) < 3 {
		return CommitInfo{}, fmt.Errorf("unexpected log output for %s", ref)
	}
	t, _ := time.Parse(time.RFC3339, parts[0])
	return CommitInfo{Hash: parts[1], Time: t, Subject: parts[2]}, nil
}

// run executes git -C <repoPath> <args...> with a timeout, returning stdout and
// surfacing stderr in the error so auth/permission problems are visible.
//
// Every invocation carries hardeningArgs: this app runs git unattended inside
// every repository it discovers, including ones the user merely cloned or
// downloaded, and git treats a repository's own .git/config as trusted. Without
// these overrides a crafted repo could run arbitrary commands the moment it is
// scanned (e.g. core.fsmonitor fires on `git status`, hooks fire on fetch/pull).
func run(ctx context.Context, timeout time.Duration, repoPath string, args ...string) ([]byte, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := make([]string, 0, 2+len(hardeningArgs)+len(args))
	full = append(full, "-C", repoPath)
	full = append(full, hardeningArgs...)
	full = append(full, args...)
	cmd := exec.CommandContext(c, Binary(), full...)
	cmd.Env = Env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	started := time.Now()
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			exitCode = exit.ExitCode()
		}
	}
	recordCommandTrace(CommandTrace{
		StartedAt: started, Duration: time.Since(started), RepoPath: repoPath,
		Executable: Binary(), Args: full, ExitCode: exitCode, Stderr: stderr.String(),
	})
	if err != nil {
		code := -1
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			code = exit.ExitCode()
		}
		return nil, &cmdError{Args: args, ExitCode: code, Stderr: stderr.String(), err: err}
	}
	return stdout.Bytes(), nil
}

// ConfigPath resolves the repository-local config file. Git performs the lookup
// because linked worktrees use a .git file and share config with their primary
// checkout; joining repoPath with ".git/config" would point at the wrong place.
func ConfigPath(ctx context.Context, repoPath string) (string, error) {
	out, err := run(ctx, readTimeout, repoPath,
		"rev-parse", "--path-format=absolute", "--git-path", "config")
	if err != nil {
		return "", err
	}
	path := strings.TrimSpace(string(out))
	if path == "" {
		return "", fmt.Errorf("git returned an empty config path")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(repoPath, path)
	}
	return filepath.Clean(path), nil
}

// cmdError is a failed git invocation, preserved in full.
//
// run() used to collapse a failure straight into a string, discarding the exit
// code and every stderr line but the last. That is enough for a status poll,
// where the last line is the whole story, but it makes classification
// impossible: a refused fast-forward says "(non-fast-forward)" and then
// continues into a hint block, and a merge blocked by local changes ends with
// the bare word "Aborting".
//
// Error() reproduces the old string exactly, so existing callers — and the error
// text persisted in the state cache — are unchanged.
type cmdError struct {
	Args     []string
	ExitCode int // -1 when the process never ran or was killed
	Stderr   string
	err      error
}

func (e *cmdError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" && e.err != nil {
		msg = e.err.Error()
	}
	return fmt.Sprintf("git %s: %s", strings.Join(e.Args, " "), lastLine(msg))
}

func (e *cmdError) Unwrap() error { return e.err }

// firstErrorLine returns the first line of stderr that states a reason, skipping
// git's hint/warning/remote chatter and stripping the "fatal: "/"error: " prefix.
//
// It is the counterpart to lastLine: git leads with the reason and pads
// afterwards, so the last line is right for a one-line auth failure and wrong
// for a merge or fetch refusal.
func firstErrorLine(stderr string) string {
	for _, line := range strings.Split(strings.TrimSpace(stderr), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "hint:"), strings.HasPrefix(line, "warning:"),
			strings.HasPrefix(line, "remote:"), strings.HasPrefix(line, "Warning:"):
			continue
		}
		line = strings.TrimPrefix(line, "fatal: ")
		line = strings.TrimPrefix(line, "error: ")
		return line
	}
	return ""
}

// parseStatus interprets porcelain v2 output. See `git help status` (Porcelain
// Format Version 2) for the line grammar.
func parseStatus(out []byte) Status {
	var s Status
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024) // tolerate very long path lines
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		switch line[0] {
		case '#':
			parseHeader(line, &s)
		case '1', '2':
			// Changed tracked entry. The second whitespace field is the XY
			// status code; it never contains spaces, so Fields is safe even
			// though later path fields may.
			if f := strings.Fields(line); len(f) >= 2 && len(f[1]) == 2 {
				countXY(f[1][0], f[1][1], &s)
			}
		case 'u':
			// Unmerged: counted as modified so the repo still reads as dirty, and
			// separately as a conflict so the row can say which it is.
			s.Modified++
			s.Conflicts++
		case '?':
			s.Untracked++
		}
	}
	return s
}

func parseHeader(line string, s *Status) {
	switch {
	case strings.HasPrefix(line, "# branch.head "):
		v := strings.TrimPrefix(line, "# branch.head ")
		if v == "(detached)" {
			s.Detached = true
			s.Branch = "detached"
		} else {
			s.Branch = v
		}
	case strings.HasPrefix(line, "# branch.upstream "):
		s.Upstream = strings.TrimPrefix(line, "# branch.upstream ")
	case strings.HasPrefix(line, "# branch.ab "):
		for _, p := range strings.Fields(strings.TrimPrefix(line, "# branch.ab ")) {
			if len(p) < 2 {
				continue
			}
			n, _ := strconv.Atoi(p[1:])
			switch p[0] {
			case '+':
				s.Ahead = n
			case '-':
				s.Behind = n
			}
		}
	}
}

// countXY tallies a changed entry from its two-character status code, where x is
// the staged (index) status and y is the worktree status.
func countXY(x, y byte, s *Status) {
	if x != '.' {
		s.Staged++
	}
	switch y {
	case 'M', 'T':
		s.Modified++
	case 'D':
		s.Deleted++
	}
}

// parseShortstat reads a line like
//
//	" 3 files changed, 12 insertions(+), 4 deletions(-)"
//
// where the insertions/deletions clauses are each optional.
func parseShortstat(s string) (added, deleted int) {
	for _, part := range strings.Split(s, ",") {
		f := strings.Fields(strings.TrimSpace(part))
		if len(f) < 2 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		switch {
		case strings.HasPrefix(f[1], "insertion"):
			added = n
		case strings.HasPrefix(f[1], "deletion"):
			deleted = n
		}
	}
	return added, deleted
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}
