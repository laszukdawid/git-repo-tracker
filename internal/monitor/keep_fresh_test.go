package monitor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	gitpkg "github.com/laszukdawid/git-repo-tracker/internal/git"
)

func TestRefreshAutomaticallyPullsKeepFreshRepo(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)
	t.Setenv("GIT_AUTHOR_NAME", "test")
	t.Setenv("GIT_AUTHOR_EMAIL", "test@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "test")
	t.Setenv("GIT_COMMITTER_EMAIL", "test@example.com")
	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))

	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	remote := filepath.Join(base, "remote.git")
	root := filepath.Join(base, "tracked")
	local := filepath.Join(root, "local")
	writer := filepath.Join(base, "writer")
	runGit := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	runGit(base, "init", "-b", "main", seed)
	write(filepath.Join(seed, "f.txt"), "initial\n")
	runGit(seed, "add", ".")
	runGit(seed, "commit", "-m", "initial")
	runGit(base, "clone", "--bare", seed, remote)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(base, "clone", remote, local)
	runGit(base, "clone", remote, writer)
	write(filepath.Join(writer, "f.txt"), "initial\nupstream\n")
	runGit(writer, "commit", "-am", "advance remote")
	runGit(writer, "push")

	mgr := New(&config.Config{
		Roots:     []config.Root{{Path: root, Depth: 1, AutoFetch: false}},
		KeepFresh: []string{local},
	}, nil, nil)
	mgr.RefreshNow(true)

	status, err := gitpkg.GetStatus(context.Background(), local)
	if err != nil {
		t.Fatal(err)
	}
	if status.Behind != 0 {
		t.Errorf("keep-fresh repo remains behind by %d commits", status.Behind)
	}
	data, err := os.ReadFile(filepath.Join(local, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "initial\nupstream\n" {
		t.Errorf("working tree was not fast-forwarded: %q", data)
	}
	snapshot := mgr.Snapshot()
	if len(snapshot) != 1 || !snapshot[0].KeepFresh {
		t.Fatalf("snapshot does not expose keep-fresh state: %+v", snapshot)
	}
}
