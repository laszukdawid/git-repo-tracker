package git

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Branch operations against real repositories.
//
// The claim these tests exist to defend is that a per-branch pull can never lose
// work: git refuses anything that is not a fast-forward, refuses a branch that
// is checked out anywhere, and leaves the working tree and HEAD untouched. That
// is only worth believing if it is exercised against real git.

// gitEnv isolates a test repo from the developer's own git configuration.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com",
	)
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s (in %s): %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

func commit(t *testing.T, dir, file, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", file)
	gitRun(t, dir, "commit", "-q", "-m", "add "+file+" "+content)
}

// branchFixture builds a remote repository and a clone of it, with branches in
// every state the UI has to handle.
func branchFixture(t *testing.T) (local, remote string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	root := t.TempDir()
	remote = filepath.Join(root, "remote")
	local = filepath.Join(root, "local")

	gitRun(t, root, "init", "-q", "-b", "main", remote)
	commit(t, remote, "a.txt", "1")
	gitRun(t, remote, "branch", "behind-me")
	gitRun(t, remote, "branch", "diverged-me")
	gitRun(t, remote, "branch", "wt-branch")
	gitRun(t, remote, "branch", "remote-only")

	gitRun(t, root, "clone", "-q", remote, local)
	// Local counterparts for the branches this test moves.
	for _, b := range []string{"behind-me", "diverged-me", "wt-branch"} {
		gitRun(t, local, "branch", "--track", b, "origin/"+b)
	}
	return local, remote
}

// advanceRemote adds a commit to a branch of the remote and fetches it.
func advanceRemote(t *testing.T, local, remote, branch, file, content string) {
	t.Helper()
	gitRun(t, remote, "checkout", "-q", branch)
	commit(t, remote, file, content)
	gitRun(t, remote, "checkout", "-q", "main")
	gitRun(t, local, "fetch", "-q", "origin")
}

func headOf(t *testing.T, dir, ref string) string {
	t.Helper()
	return gitRun(t, dir, "rev-parse", ref)
}

func TestBranchesListsLocalAndRemoteOnly(t *testing.T) {
	local, _ := branchFixture(t)
	bs, err := Branches(context.Background(), local, "origin")
	if err != nil {
		t.Fatal(err)
	}

	byName := map[string]Branch{}
	for _, b := range bs {
		if _, dup := byName[b.Name]; dup {
			t.Errorf("%s appears twice — a local branch must not also list its remote", b.Name)
		}
		byName[b.Name] = b
	}
	if !byName["main"].Current {
		t.Error("main should be the current branch")
	}
	if bs[0].Name != "main" {
		t.Errorf("the current branch sorts first, got %q", bs[0].Name)
	}
	ro, ok := byName["remote-only"]
	if !ok || ro.Kind != BranchRemoteOnly {
		t.Errorf("remote-only branch missing or misclassified: %+v", ro)
	}
}

func TestFastForwardBranchNotCheckedOut(t *testing.T) {
	local, remote := branchFixture(t)
	advanceRemote(t, local, remote, "behind-me", "b.txt", "2")

	ctx := context.Background()
	before := headOf(t, local, "HEAD")
	b := mustLookup(t, ctx, local, "behind-me")
	if b.Behind == 0 {
		t.Fatal("fixture is wrong: behind-me should be behind its upstream")
	}

	if err := FastForwardBranch(ctx, local, b); err != nil {
		t.Fatalf("fast-forward: %v", err)
	}
	after := mustLookup(t, ctx, local, "behind-me")
	if after.Behind != 0 {
		t.Errorf("still %d behind after a fast-forward", after.Behind)
	}
	// The branch moved; HEAD and the working tree did not.
	if headOf(t, local, "HEAD") != before {
		t.Error("HEAD moved — a branch that is not checked out must not touch it")
	}
	assertClean(t, ctx, local)

	// Running it again is a no-op, not an error.
	if err := FastForwardBranch(ctx, local, after); err != nil {
		t.Errorf("second fast-forward should be a silent no-op: %v", err)
	}
}

func TestFastForwardBranchDiverged(t *testing.T) {
	local, remote := branchFixture(t)
	advanceRemote(t, local, remote, "diverged-me", "r.txt", "remote")
	// A local commit on the same branch, made without checking it out.
	gitRun(t, local, "checkout", "-q", "diverged-me")
	commit(t, local, "l.txt", "local")
	gitRun(t, local, "checkout", "-q", "main")

	ctx := context.Background()
	b := mustLookup(t, ctx, local, "diverged-me")
	before := headOf(t, local, "refs/heads/diverged-me")

	err := FastForwardBranch(ctx, local, b)
	if err == nil {
		t.Fatal("a diverged branch must not fast-forward")
	}
	var be *BranchError
	if !errors.As(err, &be) || be.Kind != BranchErrDiverged {
		t.Fatalf("error = %v, want a diverged BranchError", err)
	}
	// Proof that --quiet was not passed: the marker git prints must be in stderr.
	var ce *cmdError
	if errors.As(err, &ce) && !strings.Contains(ce.Stderr, "non-fast-forward") {
		t.Errorf("stderr lacks the (non-fast-forward) marker: %q", ce.Stderr)
	}
	if headOf(t, local, "refs/heads/diverged-me") != before {
		t.Error("a refused fast-forward must not move the ref")
	}
	assertClean(t, ctx, local)
}

// A branch checked out in a linked worktree is the normal case for anyone who
// works with worktrees, so it must pull — in that worktree, exactly as pulling
// there by hand would, and without checking anything out anywhere else.
func TestFastForwardBranchInALinkedWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("worktree paths differ on Windows")
	}
	local, remote := branchFixture(t)
	wt := filepath.Join(filepath.Dir(local), "wt")
	gitRun(t, local, "worktree", "add", "-q", wt, "wt-branch")
	advanceRemote(t, local, remote, "wt-branch", "w.txt", "3")

	ctx := context.Background()
	b := mustLookup(t, ctx, local, "wt-branch")
	if b.WorktreePath == "" {
		t.Fatal("the listing should report where the branch is checked out")
	}
	mainHead := headOf(t, local, "HEAD")

	if err := FastForwardBranch(ctx, local, b); err != nil {
		t.Fatalf("FastForwardBranch: %v", err)
	}

	if got, want := headOf(t, local, "wt-branch"), headOf(t, local, "origin/wt-branch"); got != want {
		t.Errorf("wt-branch is at %s, want %s", got, want)
	}
	// The worktree's own files moved with it — that is what a pull there means.
	if data, err := os.ReadFile(filepath.Join(wt, "w.txt")); err != nil || string(data) != "3" {
		t.Errorf("worktree file = %q (%v), want the fetched content", data, err)
	}
	// The repository this was launched from did not move and stayed clean.
	if got := headOf(t, local, "HEAD"); got != mainHead {
		t.Errorf("the main worktree's HEAD moved from %s to %s", mainHead, got)
	}
	assertClean(t, ctx, local)
}

// The same pull must refuse when that worktree has uncommitted work, and leave
// the edit alone.
func TestFastForwardBranchInADirtyWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("worktree paths differ on Windows")
	}
	local, remote := branchFixture(t)
	wt := filepath.Join(filepath.Dir(local), "wt")
	gitRun(t, local, "worktree", "add", "-q", wt, "wt-branch")
	advanceRemote(t, local, remote, "wt-branch", "a.txt", "changed upstream")
	if err := os.WriteFile(filepath.Join(wt, "a.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	b := mustLookup(t, ctx, local, "wt-branch")
	before := headOf(t, local, "wt-branch")

	err := FastForwardBranch(ctx, local, b)
	var be *BranchError
	if !errors.As(err, &be) || be.Kind != BranchErrDirtyTree {
		t.Fatalf("error = %v, want a dirty-tree BranchError", err)
	}
	if got := headOf(t, local, "wt-branch"); got != before {
		t.Errorf("the branch moved anyway: %s -> %s", before, got)
	}
	if data, readErr := os.ReadFile(filepath.Join(wt, "a.txt")); readErr != nil || string(data) != "mine" {
		t.Errorf("local edit lost: %q (%v)", data, readErr)
	}
}

func TestFastForwardCurrentBranchWithDirtyTree(t *testing.T) {
	local, remote := branchFixture(t)
	advanceRemote(t, local, remote, "main", "a.txt", "changed upstream")
	// Dirty the very file the fast-forward would overwrite.
	if err := os.WriteFile(filepath.Join(local, "a.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	b := mustLookup(t, ctx, local, "main")
	err := FastForwardBranch(ctx, local, b)
	var be *BranchError
	if !errors.As(err, &be) || be.Kind != BranchErrDirtyTree {
		t.Fatalf("error = %v, want a dirty-tree BranchError", err)
	}
	// The local edit is still there — nothing was discarded to make room.
	data, readErr := os.ReadFile(filepath.Join(local, "a.txt"))
	if readErr != nil || string(data) != "mine" {
		t.Errorf("local edit lost: %q (%v)", data, readErr)
	}
}

func TestFastForwardBranchWithoutUpstream(t *testing.T) {
	local, _ := branchFixture(t)
	gitRun(t, local, "branch", "orphan")

	ctx := context.Background()
	b := mustLookup(t, ctx, local, "orphan")
	err := FastForwardBranch(ctx, local, b)
	var be *BranchError
	if !errors.As(err, &be) || be.Kind != BranchErrNoUpstream {
		t.Fatalf("error = %v, want a no-upstream BranchError", err)
	}
}

func TestCreateLocalBranch(t *testing.T) {
	local, _ := branchFixture(t)
	ctx := context.Background()
	before := headOf(t, local, "HEAD")

	if err := CreateLocalBranch(ctx, local, "remote-only", "refs/remotes/origin/remote-only"); err != nil {
		t.Fatalf("create: %v", err)
	}
	b := mustLookup(t, ctx, local, "remote-only")
	if b.Upstream != "refs/remotes/origin/remote-only" {
		t.Errorf("upstream = %q, want the remote ref it tracks", b.Upstream)
	}
	if headOf(t, local, "HEAD") != before {
		t.Error("creating a branch must not check it out")
	}
	assertClean(t, ctx, local)

	err := CreateLocalBranch(ctx, local, "remote-only", "refs/remotes/origin/remote-only")
	var be *BranchError
	if !errors.As(err, &be) || be.Kind != BranchErrExists {
		t.Fatalf("second create = %v, want an already-exists BranchError", err)
	}
}

// Removing CreateBranchWorktree, checking out the wrong ref, or creating the
// checkout inside the repository must make this fail: the worktree's HEAD and
// the original checkout are both observable parts of the contract.
func TestCreateBranchWorktreeChecksOutLocalBranch(t *testing.T) {
	local, _ := branchFixture(t)
	gitRun(t, local, "branch", "feature/login")
	ctx := context.Background()
	before := headOf(t, local, "HEAD")
	target := filepath.Join(filepath.Dir(local), ".worktrees", filepath.Base(local), "feature", "login")

	b := mustLookup(t, ctx, local, "feature/login")
	if err := CreateBranchWorktree(ctx, local, target, b); err != nil {
		t.Fatalf("create worktree: %v", err)
	}
	if got := gitRun(t, target, "branch", "--show-current"); got != "feature/login" {
		t.Errorf("worktree branch = %q, want feature/login", got)
	}
	wantPath, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustLookup(t, ctx, local, "feature/login").WorktreePath; got != wantPath {
		t.Errorf("reported worktree = %q, want %q", got, wantPath)
	}
	if got := headOf(t, local, "HEAD"); got != before {
		t.Errorf("original checkout HEAD moved from %s to %s", before, got)
	}
	assertClean(t, ctx, local)
}

// A remote-only row has no refs/heads entry yet. The editor action must create
// both that local tracking branch and its checkout in one safe git operation.
func TestCreateBranchWorktreeTracksRemoteOnlyBranch(t *testing.T) {
	local, _ := branchFixture(t)
	ctx := context.Background()
	target := filepath.Join(filepath.Dir(local), ".worktrees", filepath.Base(local), "remote-only")
	remote := Branch{
		Name: "remote-only", Kind: BranchRemoteOnly,
		RemoteRef: "refs/remotes/origin/remote-only",
	}

	if err := CreateBranchWorktree(ctx, local, target, remote); err != nil {
		t.Fatalf("create tracking worktree: %v", err)
	}
	if got := gitRun(t, target, "branch", "--show-current"); got != "remote-only" {
		t.Errorf("worktree branch = %q, want remote-only", got)
	}
	b := mustLookup(t, ctx, local, "remote-only")
	if b.Upstream != "refs/remotes/origin/remote-only" {
		t.Errorf("upstream = %q, want refs/remotes/origin/remote-only", b.Upstream)
	}
}

// A pre-existing non-empty destination belongs to the user. Worktree creation
// must refuse it and preserve its contents rather than trying to repair it.
func TestCreateBranchWorktreeDoesNotOverwriteDestination(t *testing.T) {
	local, _ := branchFixture(t)
	ctx := context.Background()
	target := filepath.Join(filepath.Dir(local), ".worktrees", filepath.Base(local), "behind-me")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := CreateBranchWorktree(ctx, local, target, mustLookup(t, ctx, local, "behind-me"))
	var branchErr *BranchError
	if !errors.As(err, &branchErr) || branchErr.Kind != BranchErrWorktreeExists {
		t.Fatalf("creating over a non-empty destination = %v, want BranchErrWorktreeExists", err)
	}
	if branchErr.Detail != target {
		t.Errorf("error destination = %q, want %q", branchErr.Detail, target)
	}
	if got, readErr := os.ReadFile(sentinel); readErr != nil || string(got) != "mine" {
		t.Errorf("destination content changed: data=%q err=%v", got, readErr)
	}
}

// A branch's upstream is not always origin/<same name>. Building the ref by
// hand instead of reading %(upstream) would fetch from the wrong place — this
// is the guard against that.
func TestFastForwardFollowsNonOriginUpstream(t *testing.T) {
	local, _ := branchFixture(t)
	root := filepath.Dir(local)
	other := filepath.Join(root, "other")
	gitRun(t, root, "init", "-q", "-b", "main", other)
	commit(t, other, "o.txt", "1")

	gitRun(t, local, "remote", "add", "other", other)
	gitRun(t, local, "fetch", "-q", "other")
	gitRun(t, local, "branch", "--track", "from-other", "other/main")

	ctx := context.Background()
	b := mustLookup(t, ctx, local, "from-other")
	if b.Upstream != "refs/remotes/other/main" {
		t.Fatalf("upstream = %q, want refs/remotes/other/main", b.Upstream)
	}

	commit(t, other, "o2.txt", "2")
	gitRun(t, local, "fetch", "-q", "other")
	b = mustLookup(t, ctx, local, "from-other")
	if b.Behind != 1 {
		t.Fatalf("behind = %d, want 1", b.Behind)
	}
	if err := FastForwardBranch(ctx, local, b); err != nil {
		t.Fatalf("fast-forward from a non-origin upstream: %v", err)
	}
	if after := mustLookup(t, ctx, local, "from-other"); after.Behind != 0 {
		t.Errorf("still %d behind", after.Behind)
	}
}

// The per-branch fetch must not write FETCH_HEAD: the user's own
// `git fetch && git merge FETCH_HEAD` would otherwise pick up our ref.
func TestFastForwardDoesNotWriteFetchHead(t *testing.T) {
	local, remote := branchFixture(t)
	advanceRemote(t, local, remote, "behind-me", "b.txt", "2")

	fetchHead := filepath.Join(local, ".git", "FETCH_HEAD")
	want, err := os.ReadFile(fetchHead)
	if err != nil {
		t.Skipf("no FETCH_HEAD to compare: %v", err)
	}

	ctx := context.Background()
	if err := FastForwardBranch(ctx, local, mustLookup(t, ctx, local, "behind-me")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(fetchHead)
	if err != nil {
		t.Fatalf("FETCH_HEAD disappeared: %v", err)
	}
	if string(got) != string(want) {
		t.Error("the per-branch fetch overwrote the user's FETCH_HEAD")
	}
}

func mustLookup(t *testing.T, ctx context.Context, repo, name string) Branch {
	t.Helper()
	b, err := LookupBranch(ctx, repo, name)
	if err != nil {
		t.Fatalf("lookup %s: %v", name, err)
	}
	return b
}

// assertClean fails if the working tree carries changes.
func assertClean(t *testing.T, ctx context.Context, repo string) {
	t.Helper()
	st, err := GetStatus(ctx, repo)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Dirty() {
		t.Errorf("working tree was modified: %+v", st)
	}
}

// A repository stopped in the middle of a merge has to say so. Nothing else
// about it — behind, ahead, dirty — is worth acting on until it is settled, and
// porcelain=v2 does not report the operation at all, so this is read from the
// marker files git leaves behind.
func TestInProgressReportsAMergeAndItsConflicts(t *testing.T) {
	local, _ := branchFixture(t)
	ctx := context.Background()

	if op := InProgress(local); op != "" {
		t.Fatalf("a settled repository reports %q", op)
	}

	// Two branches change the same line, so merging stops on a conflict.
	gitRun(t, local, "checkout", "-q", "-b", "left")
	commit(t, local, "a.txt", "left side")
	gitRun(t, local, "checkout", "-q", "main")
	commit(t, local, "a.txt", "right side")
	// The merge is expected to fail; run it directly rather than through gitRun.
	cmd := exec.Command("git", "merge", "left")
	cmd.Dir = local
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the merge was supposed to conflict:\n%s", out)
	}

	if op := InProgress(local); op != "merge" {
		t.Errorf("InProgress = %q, want \"merge\"", op)
	}
	st, err := GetStatus(ctx, local)
	if err != nil {
		t.Fatal(err)
	}
	if st.Conflicts == 0 {
		t.Error("status reports no conflicts")
	}
	if !st.Unsettled() {
		t.Error("a conflicted merge must read as unsettled")
	}

	gitRun(t, local, "merge", "--abort")
	if op := InProgress(local); op != "" {
		t.Errorf("after --abort, InProgress = %q", op)
	}
}

// A linked worktree has its own operation state: .git is a file pointing at that
// worktree's own directory, and reading the shared one would report a merge
// happening in a different checkout entirely.
func TestInProgressIsPerWorktree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("worktree paths differ on Windows")
	}
	local, _ := branchFixture(t)
	wt := filepath.Join(filepath.Dir(local), "wt")
	gitRun(t, local, "worktree", "add", "-q", wt, "wt-branch")

	// Conflict inside the worktree only.
	gitRun(t, wt, "checkout", "-q", "-b", "wt-left")
	commit(t, wt, "a.txt", "left side")
	gitRun(t, wt, "checkout", "-q", "wt-branch")
	commit(t, wt, "a.txt", "right side")
	cmd := exec.Command("git", "merge", "wt-left")
	cmd.Dir = wt
	cmd.Env = gitEnv()
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the merge was supposed to conflict:\n%s", out)
	}

	if op := InProgress(wt); op != "merge" {
		t.Errorf("worktree InProgress = %q, want \"merge\"", op)
	}
	if op := InProgress(local); op != "" {
		t.Errorf("the main checkout reports %q — it is not the one merging", op)
	}
}
