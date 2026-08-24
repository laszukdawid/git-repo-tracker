package monitor

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
)

// Flattening the branch or putting the checkout under repoPath would break the
// agreed layout and make the original repository appear dirty.
func TestBranchWorktreePathMirrorsBranchHierarchyBesideRepository(t *testing.T) {
	repo := filepath.Join(string(filepath.Separator), "projects", "my-app")
	want := filepath.Join(string(filepath.Separator), "projects", ".worktrees", "my-app", "feature", "login")

	got, err := branchWorktreePath(repo, "feature/login")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("worktree path = %q, want %q", got, want)
	}
}

func TestBranchWorktreePathRejectsEscapingBranch(t *testing.T) {
	repo := filepath.Join(string(filepath.Separator), "projects", "my-app")
	if _, err := branchWorktreePath(repo, "../outside"); err == nil {
		t.Fatal("path traversal branch was accepted")
	}
}

// Removing the existing-checkout fast path must make this fail: opening the
// current branch means opening its repository, not trying to check it out twice.
func TestEnsureBranchWorktreeReusesExistingCheckout(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "my-app")
	runWorktreeGit(t, t.TempDir(), "init", "-q", "-b", "main", repo)
	runWorktreeGit(t, repo, "-c", "user.name=t", "-c", "user.email=t@example.com",
		"commit", "--allow-empty", "-q", "-m", "initial")
	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))
	mgr := New(&config.Config{}, nil, nil)

	got, err := mgr.EnsureBranchWorktree(repo, BranchInfo{Name: "main", Current: true, Worktree: repo})
	if err != nil {
		t.Fatalf("reuse current checkout: %v", err)
	}
	want, err := filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("checkout = %q, want existing %q", got, want)
	}
}

func runWorktreeGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_CONFIG_SYSTEM="+os.DevNull,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
