package git

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Per-branch inspection and fast-forwarding.
//
// The app could previously only pull the branch you were standing on. Everything
// here exists to lift that limit without ever touching the working tree: a
// branch that is not checked out is advanced by fetching into its ref, which git
// refuses unless the move is a genuine fast-forward, and refuses outright if the
// branch is checked out in any worktree. Nothing here can lose work.

// branchFormat is one for-each-ref line per local branch, NUL-separated so a
// commit subject containing spaces, commas or dots parses cleanly. Refnames
// cannot contain control characters, so NUL is unambiguous.
//
// One invocation covers every branch: asking git for ahead/behind per branch
// instead costs a separate revision walk each time (~900ms for 61 branches
// versus ~37ms for this).
const branchFormat = "%(refname:lstrip=2)%00%(objectname)%00%(upstream)%00" +
	"%(upstream:track,nobracket)%00%(committerdate:unix)%00%(committerdate:iso-strict)%00" +
	"%(worktreepath)%00%(HEAD)%00%(contents:subject)"

// remoteBranchFormat lists remote-tracking refs. The %(if)%(symref) guard skips
// origin/HEAD, which is a symbolic ref rather than a branch — it still emits a
// blank line, so empty lines must be dropped when parsing.
const remoteBranchFormat = "%(if)%(symref)%(then)%(else)%(refname:lstrip=3)%00" +
	"%(objectname)%00%(committerdate:unix)%00%(contents:subject)%(end)"

// BranchKind separates branches that exist locally from ones that only exist on
// the remote and could be created.
type BranchKind int

const (
	BranchLocal BranchKind = iota
	BranchRemoteOnly
)

// Branch is one entry of a repository's branch listing.
type Branch struct {
	Name     string // "feature/x"
	Kind     BranchKind
	ObjectID string // full sha of the tip
	// Upstream is the FULL %(upstream) ref, e.g. "refs/remotes/origin/foo".
	// Never rebuild this as "origin/"+Name: a branch's upstream may live on a
	// different remote, or even be another local branch.
	Upstream     string
	UpstreamName string // display form, e.g. "origin/foo"
	Ahead        int
	Behind       int
	Gone         bool   // the configured upstream ref no longer exists
	Current      bool   // this is HEAD
	WorktreePath string // non-empty when checked out somewhere; fetching into it will be refused
	CommitTime   time.Time
	Subject      string
	RemoteRef    string // remote-only rows: "refs/remotes/origin/<name>"
}

// HasUpstream reports whether the branch can be fast-forwarded at all.
func (b Branch) HasUpstream() bool { return b.Upstream != "" }

// Behindness reports whether pulling this branch would do anything.
func (b Branch) Updatable() bool { return b.Behind > 0 && !b.Gone }

// LocalBranches lists every local branch, newest commit first.
func LocalBranches(ctx context.Context, repoPath string) ([]Branch, error) {
	out, err := run(ctx, readTimeout, repoPath,
		"for-each-ref", "--format="+branchFormat, "--sort=-committerdate", "refs/heads")
	if err != nil {
		return nil, err
	}
	return parseLocalBranches(out), nil
}

// RemoteBranches lists the remote's branches. A repository with no such remote
// yields nothing and no error — that is a normal state, not a failure.
func RemoteBranches(ctx context.Context, repoPath, remote string) ([]Branch, error) {
	out, err := run(ctx, readTimeout, repoPath,
		"for-each-ref", "--format="+remoteBranchFormat, "--sort=-committerdate", "refs/remotes/"+remote)
	if err != nil {
		return nil, err
	}
	return parseRemoteBranches(out, remote), nil
}

// Branches returns local branches plus the remote-only ones, sorted for display:
// the current branch first, then by most recent commit.
func Branches(ctx context.Context, repoPath, remote string) ([]Branch, error) {
	local, err := LocalBranches(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	// A missing remote is not an error; the local list still stands on its own.
	remoteOnly, _ := RemoteBranches(ctx, repoPath, remote)
	all := mergeBranches(local, remoteOnly)
	sortBranches(all)
	return all, nil
}

// LookupBranch re-reads one branch. Branch operations resolve their target
// through this rather than trusting the listing the UI was rendered from, which
// may be seconds old.
func LookupBranch(ctx context.Context, repoPath, name string) (Branch, error) {
	out, err := run(ctx, readTimeout, repoPath,
		"for-each-ref", "--format="+branchFormat, "refs/heads/"+name)
	if err != nil {
		return Branch{}, err
	}
	bs := parseLocalBranches(out)
	if len(bs) == 0 {
		return Branch{}, &BranchError{Kind: BranchErrMissing, Branch: name}
	}
	return bs[0], nil
}

// FastForwardBranch advances one branch to its upstream.
//
// There are three cases, and which one applies is decided by where the branch is
// checked out — never by what the caller would prefer:
//
//   - Not checked out anywhere: fetch from the repository into the branch's own
//     ref. Nothing is checked out and no working tree is touched.
//   - Checked out here: merge --ff-only. A fetch into a checked-out ref is
//     refused by design.
//   - Checked out in a LINKED WORKTREE: merge --ff-only *in that worktree*. This
//     is the common case for anyone who works with worktrees — most of their
//     branches live in one, and refusing them all would leave the control
//     useless. The worktree's files advance exactly as they would if the user
//     had pulled there by hand; git refuses if it has uncommitted changes, and
//     nothing is ever checked out anywhere new.
//
// No form reaches the network: the app has already fetched, and the object store
// is shared with every linked worktree, so the data is local by the time a user
// clicks.
func FastForwardBranch(ctx context.Context, repoPath string, b Branch) error {
	if !b.HasUpstream() {
		return &BranchError{Kind: BranchErrNoUpstream, Branch: b.Name}
	}

	var err error
	switch {
	case b.Current:
		// advice.diverging=false suppresses nine lines of hint noise on failure.
		_, err = run(ctx, readTimeout, repoPath,
			"-c", "advice.diverging=false", "merge", "--ff-only", "--quiet", b.Upstream)
	case b.WorktreePath != "":
		// Same merge, run where the branch actually lives.
		_, err = run(ctx, readTimeout, b.WorktreePath,
			"-c", "advice.diverging=false", "merge", "--ff-only", "--quiet", b.Upstream)
	default:
		// No --quiet here: it silences the "(non-fast-forward)" line, leaving a
		// bare exit code with nothing to classify.
		_, err = run(ctx, readTimeout, repoPath,
			"fetch", "--no-write-fetch-head", "--no-auto-maintenance", "--no-tags",
			".", b.Upstream+":refs/heads/"+b.Name)
	}
	if err != nil {
		return wrapBranchError(b.Name, err)
	}
	return nil
}

// RemoteRefs maps every remote-tracking ref to the commit it points at. It is
// how a fetch reports what it actually brought in: git fetch --quiet says
// nothing, and without --quiet its output is prose meant for a terminal. Two
// cheap local reads either side of the fetch give an exact count instead.
func RemoteRefs(ctx context.Context, repoPath string) (map[string]string, error) {
	out, err := run(ctx, readTimeout, repoPath,
		"for-each-ref", "--format=%(refname)%00%(objectname)", "refs/remotes")
	if err != nil {
		return nil, err
	}
	refs := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		name, id, ok := strings.Cut(strings.TrimSpace(line), "\x00")
		if ok && name != "" {
			refs[name] = id
		}
	}
	return refs, nil
}

// CountChangedRefs reports how many entries differ between two snapshots,
// counting refs that appeared or vanished as well as those that moved.
func CountChangedRefs(before, after map[string]string) int {
	n := 0
	for name, id := range after {
		if before[name] != id {
			n++
		}
	}
	for name := range before {
		if _, still := after[name]; !still {
			n++
		}
	}
	return n
}

// PushBranch publishes a branch to its upstream remote.
//
// It is never forced and never leases: git's own non-fast-forward check is the
// safety net, and a rejected push is reported as "the remote moved, pull first"
// rather than worked around. Nothing here can overwrite anyone else's commits.
//
// The refspec is explicit — <branch>:<the name it has on the remote> — so this
// works for a branch that is not checked out, without a checkout, and cannot be
// affected by push.default.
func PushBranch(ctx context.Context, repoPath string, b Branch) error {
	if !b.HasUpstream() {
		return &BranchError{Kind: BranchErrNoUpstream, Branch: b.Name}
	}
	remote, name, ok := splitUpstream(b.UpstreamName)
	if !ok {
		return &BranchError{Kind: BranchErrNoUpstream, Branch: b.Name}
	}
	if _, err := run(ctx, fetchTimeout, repoPath,
		"push", remote, "refs/heads/"+b.Name+":refs/heads/"+name); err != nil {
		return wrapBranchError(b.Name, err)
	}
	return nil
}

// MergeUpstream integrates a diverged branch's upstream into it, in the checkout
// that holds the branch.
//
// This is the one operation that needs a working tree, and the reason a diverged
// branch that is checked out nowhere cannot be brought up to date at all: a
// merge has to happen somewhere. On conflict git stops and leaves the merge in
// progress — which the row then reports, rather than the app trying to be clever
// about someone else's conflict.
func MergeUpstream(ctx context.Context, worktreePath string, b Branch) error {
	if !b.HasUpstream() {
		return &BranchError{Kind: BranchErrNoUpstream, Branch: b.Name}
	}
	if worktreePath == "" {
		return &BranchError{Kind: BranchErrNotCheckedOut, Branch: b.Name}
	}
	if _, err := run(ctx, readTimeout, worktreePath,
		"-c", "advice.diverging=false", "merge", "--no-edit", b.Upstream); err != nil {
		return wrapBranchError(b.Name, err)
	}
	return nil
}

// splitUpstream turns "origin/feature/x" into ("origin", "feature/x").
func splitUpstream(upstreamName string) (remote, branch string, ok bool) {
	remote, branch, ok = strings.Cut(upstreamName, "/")
	if !ok || remote == "" || branch == "" {
		return "", "", false
	}
	return remote, branch, true
}

// CreateLocalBranch creates a local branch tracking a remote one. It does not
// check anything out, so the working tree is untouched.
func CreateLocalBranch(ctx context.Context, repoPath, name, remoteFullRef string) error {
	if _, err := run(ctx, readTimeout, repoPath,
		"branch", "--track", "--quiet", name, remoteFullRef); err != nil {
		return wrapBranchError(name, err)
	}
	return nil
}

// CreateBranchWorktree checks out one branch into worktreePath. A remote-only
// branch is created locally and configured to track its remote ref as part of
// the same git operation. Git owns destination safety: it refuses a non-empty
// directory and never overwrites files already there.
func CreateBranchWorktree(ctx context.Context, repoPath, worktreePath string, b Branch) error {
	args := []string{"worktree", "add", "--quiet"}
	if b.Kind == BranchRemoteOnly {
		if b.RemoteRef == "" {
			return &BranchError{Kind: BranchErrNoUpstream, Branch: b.Name}
		}
		args = append(args, "--track", "-b", b.Name, worktreePath, b.RemoteRef)
	} else {
		args = append(args, worktreePath, b.Name)
	}
	if _, err := run(ctx, fetchTimeout, repoPath, args...); err != nil {
		var commandErr *cmdError
		if errors.As(err, &commandErr) &&
			strings.Contains(commandErr.Stderr, worktreePath) &&
			strings.Contains(strings.ToLower(commandErr.Stderr), "already exists") {
			return &BranchError{
				Kind: BranchErrWorktreeExists, Branch: b.Name, Detail: worktreePath, err: err,
			}
		}
		return wrapBranchError(b.Name, err)
	}
	return nil
}

// --- errors -----------------------------------------------------------------

// BranchErrKind is why a branch operation was refused. The UI switches on this
// rather than matching git's wording itself.
type BranchErrKind int

const (
	BranchErrUnknown BranchErrKind = iota
	BranchErrDiverged
	BranchErrCheckedOut
	BranchErrDirtyTree
	BranchErrUntracked
	BranchErrNoUpstream
	BranchErrGone
	BranchErrLocked
	BranchErrExists
	BranchErrWorktreeExists
	BranchErrMissing
	BranchErrTimeout
	// BranchErrPushRejected: the remote has commits this branch does not.
	BranchErrPushRejected
	// BranchErrConflicted: a merge started and stopped on conflicts. The repo is
	// now mid-merge, which the status glyph reports.
	BranchErrConflicted
	// BranchErrNotCheckedOut: the branch has diverged and is checked out nowhere,
	// so there is no working tree to merge in.
	BranchErrNotCheckedOut
)

// BranchError is a refused branch operation, already classified.
type BranchError struct {
	Kind   BranchErrKind
	Branch string
	Detail string // worktree path, or the first meaningful stderr line
	err    error
}

func (e *BranchError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return e.Branch + ": " + e.Message()
}

func (e *BranchError) Unwrap() error { return e.err }

// Message is the short, human sentence the UI shows beneath the branch.
func (e *BranchError) Message() string {
	switch e.Kind {
	case BranchErrDiverged:
		return "diverged — cannot fast-forward"
	case BranchErrCheckedOut:
		if e.Detail != "" {
			return "checked out at " + e.Detail
		}
		return "checked out in another worktree"
	case BranchErrDirtyTree:
		return "uncommitted changes — commit or stash first"
	case BranchErrUntracked:
		return "untracked files are in the way"
	case BranchErrNoUpstream:
		return "no upstream branch set"
	case BranchErrGone:
		return "upstream branch no longer exists"
	case BranchErrLocked:
		return "another git process is using this repo — try again"
	case BranchErrExists:
		return "a local branch with this name already exists"
	case BranchErrWorktreeExists:
		return "worktree destination already exists: " + e.Detail
	case BranchErrMissing:
		return "branch not found"
	case BranchErrPushRejected:
		return "the remote has moved — pull first, then push"
	case BranchErrConflicted:
		return "merge stopped on conflicts — resolve them, or abort the merge"
	case BranchErrNotCheckedOut:
		return "diverged, and checked out nowhere — check it out to merge or rebase"
	case BranchErrTimeout:
		return "timed out"
	default:
		if e.Detail != "" {
			return e.Detail
		}
		return "failed"
	}
}

var checkedOutRe = regexp.MustCompile(`checked out at '([^']*)'`)

// wrapBranchError classifies a failed git invocation.
func wrapBranchError(branch string, err error) error {
	var ce *cmdError
	if !errors.As(err, &ce) {
		return &BranchError{Kind: BranchErrUnknown, Branch: branch, Detail: err.Error(), err: err}
	}
	be := classifyBranchError(branch, ce.ExitCode, ce.Stderr)
	be.err = err
	return be
}

// classifyBranchError maps git's stderr onto a BranchErrKind. It reads the whole
// stderr, not one line: the decisive phrase is rarely the last thing git prints.
func classifyBranchError(branch string, exitCode int, stderr string) *BranchError {
	be := &BranchError{Kind: BranchErrUnknown, Branch: branch}
	switch {
	case strings.Contains(stderr, "Automatic merge failed"),
		strings.Contains(stderr, "CONFLICT ("):
		be.Kind = BranchErrConflicted
	case strings.Contains(stderr, "Updates were rejected"),
		strings.Contains(stderr, "failed to push some refs"):
		// A push git would not take. Keyed on the push-only wording, NOT on
		// "! [rejected]" — a fetch into a local ref prints that line too, and
		// classifying a refused fast-forward as a refused push would tell the
		// user to do exactly the wrong thing.
		be.Kind = BranchErrPushRejected
	case strings.Contains(stderr, "non-fast-forward"),
		strings.Contains(stderr, "Not possible to fast-forward"):
		be.Kind = BranchErrDiverged
	case strings.Contains(stderr, "refusing to fetch into branch"),
		strings.Contains(stderr, "cannot force update the branch"):
		be.Kind = BranchErrCheckedOut
		if m := checkedOutRe.FindStringSubmatch(stderr); len(m) == 2 {
			be.Detail = m[1]
		}
	case strings.Contains(stderr, "local changes to the following files would be overwritten"):
		be.Kind = BranchErrDirtyTree
	case strings.Contains(stderr, "untracked working tree files would be overwritten"):
		be.Kind = BranchErrUntracked
	case strings.Contains(stderr, "cannot lock ref"),
		strings.Contains(stderr, "unable to update local ref"),
		strings.Contains(stderr, "Another git process seems to be running"):
		be.Kind = BranchErrLocked
	case strings.Contains(stderr, "couldn't find remote ref"),
		strings.Contains(stderr, "no such ref"):
		be.Kind = BranchErrGone
	case strings.Contains(stderr, "already exists"):
		be.Kind = BranchErrExists
	default:
		be.Detail = firstErrorLine(stderr)
		if be.Detail == "" {
			be.Detail = fmt.Sprintf("git exited with status %d", exitCode)
		}
	}
	return be
}

// --- parsing ----------------------------------------------------------------

func parseLocalBranches(out []byte) []Branch {
	var bs []Branch
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 9 {
			continue // malformed; skip rather than guess
		}
		ahead, behind, gone := parseTrack(f[3])
		b := Branch{
			Name:         f[0],
			Kind:         BranchLocal,
			ObjectID:     f[1],
			Upstream:     f[2],
			UpstreamName: shortRef(f[2]),
			Ahead:        ahead,
			Behind:       behind,
			Gone:         gone,
			WorktreePath: f[6],
			// %(HEAD) is "*" for the current branch and a single space otherwise.
			Current: strings.TrimSpace(f[7]) == "*",
			Subject: f[8],
		}
		if secs, err := strconv.ParseInt(f[4], 10, 64); err == nil {
			b.CommitTime = time.Unix(secs, 0)
		} else if t, err := time.Parse(time.RFC3339, f[5]); err == nil {
			b.CommitTime = t
		}
		bs = append(bs, b)
	}
	return bs
}

func parseRemoteBranches(out []byte, remote string) []Branch {
	var bs []Branch
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		// origin/HEAD is skipped by the format's %(if) guard but still emits a
		// blank line.
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, "\x00")
		if len(f) < 4 {
			continue
		}
		b := Branch{
			Name:         f[0],
			Kind:         BranchRemoteOnly,
			ObjectID:     f[1],
			Subject:      f[3],
			RemoteRef:    "refs/remotes/" + remote + "/" + f[0],
			UpstreamName: remote + "/" + f[0],
		}
		if secs, err := strconv.ParseInt(f[2], 10, 64); err == nil {
			b.CommitTime = time.Unix(secs, 0)
		}
		bs = append(bs, b)
	}
	return bs
}

// parseTrack reads %(upstream:track,nobracket): "", "gone", "ahead 1",
// "behind 2" or "ahead 1, behind 2".
func parseTrack(s string) (ahead, behind int, gone bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	if s == "gone" {
		return 0, 0, true
	}
	for _, part := range strings.Split(s, ",") {
		fields := strings.Fields(part)
		if len(fields) != 2 {
			continue
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			continue
		}
		switch fields[0] {
		case "ahead":
			ahead = n
		case "behind":
			behind = n
		}
	}
	return ahead, behind, false
}

// mergeBranches drops remote branches that already have a local counterpart, so
// each branch appears exactly once.
func mergeBranches(local, remote []Branch) []Branch {
	haveLocal := make(map[string]bool, len(local))
	for _, b := range local {
		haveLocal[b.Name] = true
	}
	out := make([]Branch, 0, len(local)+len(remote))
	out = append(out, local...)
	for _, b := range remote {
		if !haveLocal[b.Name] {
			out = append(out, b)
		}
	}
	return out
}

// sortBranches orders branches the way they are looked for: the branch you are
// standing on, then the local ones with something to pull, then the rest of your
// local branches — most recently touched first — and only then the ones that
// exist solely on the remote.
//
// Local before remote matters more than date: a repository with a busy remote
// would otherwise bury your own branches under a wall of other people's. And
// behind-before-level matters within your own: in a repository with two hundred
// branches, the seven that can actually be pulled are otherwise scattered
// through a list nobody scrolls to the end of.
func sortBranches(bs []Branch) {
	sort.SliceStable(bs, func(i, j int) bool {
		if bs[i].Current != bs[j].Current {
			return bs[i].Current
		}
		if (bs[i].Kind == BranchRemoteOnly) != (bs[j].Kind == BranchRemoteOnly) {
			return bs[j].Kind == BranchRemoteOnly
		}
		if bs[i].Kind != BranchRemoteOnly {
			if a, b := bs[i].Behind > 0, bs[j].Behind > 0; a != b {
				return a
			}
		}
		if !bs[i].CommitTime.Equal(bs[j].CommitTime) {
			return bs[i].CommitTime.After(bs[j].CommitTime)
		}
		return bs[i].Name < bs[j].Name
	})
}

// shortRef turns "refs/remotes/origin/foo" into "origin/foo" and
// "refs/heads/foo" into "foo".
func shortRef(ref string) string {
	switch {
	case ref == "":
		return ""
	case strings.HasPrefix(ref, "refs/remotes/"):
		return strings.TrimPrefix(ref, "refs/remotes/")
	case strings.HasPrefix(ref, "refs/heads/"):
		return strings.TrimPrefix(ref, "refs/heads/")
	default:
		return ref
	}
}
