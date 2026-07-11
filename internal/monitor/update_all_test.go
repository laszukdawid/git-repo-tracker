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

func TestUpdateAllFetchesManualRootsAndPersists(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}

	base := t.TempDir()
	seed := filepath.Join(base, "seed")
	remote := filepath.Join(base, "remote.git")
	tracked := filepath.Join(base, "tracked")
	local := filepath.Join(tracked, "local")
	writer := filepath.Join(base, "writer")
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+os.DevNull,
			"GIT_CONFIG_SYSTEM="+os.DevNull,
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
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

	run(base, "init", "-b", "main", seed)
	write(filepath.Join(seed, "f.txt"), "initial\n")
	run(seed, "add", ".")
	run(seed, "commit", "-m", "initial")
	run(base, "clone", "--bare", seed, remote)
	if err := os.MkdirAll(tracked, 0o755); err != nil {
		t.Fatal(err)
	}
	run(base, "clone", remote, local)
	run(base, "clone", remote, writer)
	write(filepath.Join(writer, "f.txt"), "initial\nupstream\n")
	run(writer, "commit", "-am", "advance remote")
	run(writer, "push")

	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))
	mgr := New(&config.Config{
		Roots: []config.Root{{Path: tracked, Depth: 1, AutoFetch: false}},
	}, nil, nil)
	results := mgr.UpdateAll()
	if len(results) != 1 {
		t.Fatalf("updated %d repositories, want 1: %+v", len(results), results)
	}
	if results[0].Path != local || results[0].Err != "" {
		t.Fatalf("update result = %+v, want successful pull of %s", results[0], local)
	}

	status, err := gitpkg.GetStatus(context.Background(), local)
	if err != nil {
		t.Fatal(err)
	}
	if status.Behind != 0 {
		t.Errorf("local repo remains behind by %d commits", status.Behind)
	}
	cached, ok := loadCache()[local]
	if !ok {
		t.Fatal("updated repository was not persisted to the cache")
	}
	if cached.Behind != 0 {
		t.Errorf("cached behind = %d, want 0", cached.Behind)
	}
}
