package monitor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	gitpkg "github.com/laszukdawid/git-repo-tracker/internal/git"
)

// The scenario the branch chip exists for: a colleague pushed to a branch this
// machine is NOT standing on, and nothing has fetched since. Fast-forwarding
// alone would be a no-op — the remote-tracking ref is still where it was — so
// UpdateBranch fetches first and only then moves the branch.
func TestUpdateBranchFetchesBeforeFastForwarding(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	seed := filepath.Join(base, "seed")
	remote := filepath.Join(base, "remote.git")
	tracked := filepath.Join(base, "tracked")
	local := filepath.Join(tracked, "local")
	writer := filepath.Join(base, "writer")

	run(base, "init", "-b", "main", seed)
	write(filepath.Join(seed, "f.txt"), "initial\n")
	run(seed, "add", ".")
	run(seed, "commit", "-m", "initial")
	run(seed, "branch", "topic")
	run(base, "clone", "--bare", seed, remote)
	if err := os.MkdirAll(tracked, 0o755); err != nil {
		t.Fatal(err)
	}
	run(base, "clone", remote, local)
	run(local, "branch", "--track", "topic", "origin/topic")
	run(base, "clone", remote, writer)

	// Someone else advances "topic" on the remote. This clone knows nothing yet.
	run(writer, "checkout", "topic")
	write(filepath.Join(writer, "f.txt"), "initial\nfrom a colleague\n")
	run(writer, "commit", "-am", "advance topic")
	run(writer, "push", "origin", "topic")

	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))
	mgr := New(&config.Config{
		Roots: []config.Root{{Path: tracked, Depth: 1, AutoFetch: false}},
	}, nil, nil)

	// Nothing has fetched, so the listing reports the branch as level: this is
	// exactly why the chip cannot be hidden when Behind is 0.
	before := mgr.Branches(local)
	if b, ok := findBranch(before, "topic"); !ok || b.Behind != 0 {
		t.Fatalf("before the fetch, topic reports behind=%d — the fixture is wrong", b.Behind)
	}

	headBefore := run(local, "rev-parse", "HEAD")
	did, err := mgr.SyncBranch(local, "topic")
	if err != nil {
		t.Fatalf("SyncBranch: %v", err)
	}
	if did != BranchPull {
		t.Errorf("SyncBranch did %v, want a pull", did)
	}

	want := run(writer, "rev-parse", "topic")
	if got := run(local, "rev-parse", "topic"); got != want {
		t.Errorf("topic is at %s, want the remote's %s", got, want)
	}
	// The working tree is untouched: HEAD did not move and nothing is dirty.
	if got := run(local, "rev-parse", "HEAD"); got != headBefore {
		t.Errorf("HEAD moved from %s to %s", headBefore, got)
	}
	if out := run(local, "status", "--porcelain"); out != "" {
		t.Errorf("working tree changed:\n%s", out)
	}
	st, err := gitpkg.GetStatus(context.Background(), local)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" {
		t.Errorf("checked-out branch is now %q", st.Branch)
	}

	// Pressing it again must say so rather than reporting a pull that moved
	// nothing, and must not be reported as a failure.
	_, err = mgr.SyncBranch(local, "topic")
	if !errors.Is(err, ErrBranchUpToDate) {
		t.Errorf("second update returned %v, want ErrBranchUpToDate", err)
	}
}

func findBranch(bl BranchList, name string) (BranchInfo, bool) {
	for _, b := range bl.Branches {
		if b.Name == name {
			return b, true
		}
	}
	return BranchInfo{}, false
}

// The whole point of the control: it does what the branch needs, and "diverged —
// cannot fast-forward" is not the answer to two of the three cases.
func TestSyncBranchPushesMergesAndPulls(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	base := t.TempDir()
	run := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	seed := filepath.Join(base, "seed")
	remote := filepath.Join(base, "remote.git")
	tracked := filepath.Join(base, "tracked")
	local := filepath.Join(tracked, "local")
	writer := filepath.Join(base, "writer")

	run(base, "init", "-b", "main", seed)
	write(filepath.Join(seed, "f.txt"), "initial\n")
	run(seed, "add", ".")
	run(seed, "commit", "-m", "initial")
	run(seed, "branch", "mine")    // will be pushed
	run(seed, "branch", "tangled") // will diverge
	run(base, "clone", "--bare", seed, remote)
	if err := os.MkdirAll(tracked, 0o755); err != nil {
		t.Fatal(err)
	}
	run(base, "clone", remote, local)
	run(local, "branch", "--track", "mine", "origin/mine")
	run(local, "branch", "--track", "tangled", "origin/tangled")
	run(base, "clone", remote, writer)

	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))
	mgr := New(&config.Config{
		Roots: []config.Root{{Path: tracked, Depth: 1, AutoFetch: false}},
	}, nil, nil)

	// --- ahead only: this must PUSH, not report a failed fast-forward --------
	wt := filepath.Join(base, "mine-wt")
	run(local, "worktree", "add", "-q", wt, "mine")
	write(filepath.Join(wt, "f.txt"), "initial\nmy work\n")
	run(wt, "commit", "-am", "my work")

	did, err := mgr.SyncBranch(local, "mine")
	if err != nil {
		t.Fatalf("syncing an ahead-only branch: %v", err)
	}
	if did != BranchPush {
		t.Errorf("sync did %v, want a push", did)
	}
	if got, want := run(writer, "ls-remote", remote, "refs/heads/mine")[:40], run(local, "rev-parse", "mine"); got != want {
		t.Errorf("remote mine is at %s, want %s", got, want)
	}

	// --- diverged, checked out: this must MERGE -----------------------------
	twt := filepath.Join(base, "tangled-wt")
	run(local, "worktree", "add", "-q", twt, "tangled")
	write(filepath.Join(twt, "local-only.txt"), "mine\n")
	run(twt, "add", ".")
	run(twt, "commit", "-m", "local side")

	run(writer, "checkout", "tangled")
	write(filepath.Join(writer, "remote-only.txt"), "theirs\n")
	run(writer, "add", ".")
	run(writer, "commit", "-m", "remote side")
	run(writer, "push", "origin", "tangled")

	did, err = mgr.SyncBranch(local, "tangled")
	if err != nil {
		t.Fatalf("syncing a diverged branch: %v", err)
	}
	if did != BranchMerge {
		t.Errorf("sync did %v, want a merge", did)
	}
	// Both sides are present afterwards, and the merge happened in the worktree
	// that holds the branch — not in the repository the app was pointed at.
	for _, f := range []string{"local-only.txt", "remote-only.txt"} {
		if _, err := os.Stat(filepath.Join(twt, f)); err != nil {
			t.Errorf("%s missing after the merge: %v", f, err)
		}
	}
	if run(local, "rev-parse", "--abbrev-ref", "HEAD") != "main" {
		t.Error("the main checkout changed branch")
	}
	// And now it is only ahead, so the same control becomes a push.
	if got := PlanBranch(mustBranch(t, mgr, local, "tangled")); got != BranchPush {
		t.Errorf("after merging, the plan is %v, want a push", got)
	}
}

func mustBranch(t *testing.T, m *Manager, path, name string) BranchInfo {
	t.Helper()
	for _, b := range m.Branches(path).Branches {
		if b.Name == name {
			return b
		}
	}
	t.Fatalf("branch %q not found", name)
	return BranchInfo{}
}
