package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestRepoLocalConfigCannotExecute proves the hardening overrides hold against a
// hostile repository: a .git/config that sets core.fsmonitor to a shell command
// (which plain `git status` would execute) and a post-merge hook. Neither may run
// when the monitor inspects or fast-forwards the repo.
func TestRepoLocalConfigCannotExecute(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a POSIX shell command as the payload")
	}
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	marker := filepath.Join(dir, "pwned")
	repo := filepath.Join(dir, "repo")
	mustGit(t, "", "init", "-q", repo)
	mustGit(t, repo, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")

	// Payload 1: core.fsmonitor runs on every `git status`.
	mustGit(t, repo, "config", "core.fsmonitor", "touch "+marker+"; echo")
	// Payload 2: a hook in the repo's own hooks dir.
	hook := filepath.Join(repo, ".git", "hooks", "post-checkout")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Sanity: without hardening, the payload fires (otherwise the test proves nothing).
	plain := exec.Command("git", "-C", repo, "status", "--porcelain=v2")
	_ = plain.Run()
	if _, err := os.Stat(marker); err != nil {
		t.Skip("this git build does not honour repo-local core.fsmonitor; nothing to defend against")
	}
	os.Remove(marker)

	ctx := context.Background()
	if _, err := GetStatus(ctx, repo); err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("repo-local core.fsmonitor executed through GetStatus")
	}
	// A checkout to HEAD triggers post-checkout if hooks were honoured.
	if _, err := run(ctx, readTimeout, repo, "checkout", "-q", "HEAD"); err != nil {
		t.Fatalf("checkout: %v", err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("repo-local hook executed through run()")
	}
}

func mustGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
