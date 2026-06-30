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
	"fmt"
	"os/exec"
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
}

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
	return parseStatus(out), nil
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
	out, err := run(ctx, readTimeout, repoPath, "diff", "--shortstat", "HEAD..."+ref)
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
func Fetch(ctx context.Context, repoPath string) error {
	_, err := run(ctx, fetchTimeout, repoPath, "fetch", "--quiet")
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
func run(ctx context.Context, timeout time.Duration, repoPath string, args ...string) ([]byte, error) {
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	full := append([]string{"-C", repoPath}, args...)
	cmd := exec.CommandContext(c, Binary(), full...)
	cmd.Env = Env()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), lastLine(msg))
	}
	return stdout.Bytes(), nil
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
			s.Modified++ // unmerged entry — needs attention, count as modified
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
