package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseStatus(t *testing.T) {
	out := []byte(`# branch.oid abc123
# branch.head feature/x
# branch.upstream origin/feature/x
# branch.ab +2 -3
1 M. N... 100644 100644 100644 aaa bbb staged.go
1 .M N... 100644 100644 100644 ccc ddd modified.go
1 .D N... 100644 100644 000000 eee fff deleted.go
? untracked.txt
`)
	s := parseStatus(out)
	if s.Branch != "feature/x" || s.Upstream != "origin/feature/x" {
		t.Errorf("branch/upstream = %q/%q", s.Branch, s.Upstream)
	}
	if s.Ahead != 2 || s.Behind != 3 {
		t.Errorf("ahead/behind = %d/%d, want 2/3", s.Ahead, s.Behind)
	}
	if s.Staged != 1 || s.Modified != 1 || s.Deleted != 1 || s.Untracked != 1 {
		t.Errorf("counts staged=%d modified=%d deleted=%d untracked=%d", s.Staged, s.Modified, s.Deleted, s.Untracked)
	}
	if !s.Dirty() {
		t.Error("expected dirty")
	}
}

func TestParseStatusDetached(t *testing.T) {
	s := parseStatus([]byte("# branch.head (detached)\n"))
	if !s.Detached || s.Branch != "detached" {
		t.Errorf("detached=%v branch=%q", s.Detached, s.Branch)
	}
}

func TestParseShortstat(t *testing.T) {
	cases := []struct {
		in           string
		wantA, wantD int
	}{
		{" 3 files changed, 12 insertions(+), 4 deletions(-)\n", 12, 4},
		{" 1 file changed, 5 insertions(+)\n", 5, 0},
		{" 1 file changed, 2 deletions(-)\n", 0, 2},
		{"", 0, 0},
	}
	for _, c := range cases {
		a, d := parseShortstat(c.in)
		if a != c.wantA || d != c.wantD {
			t.Errorf("parseShortstat(%q) = %d/%d, want %d/%d", c.in, a, d, c.wantA, c.wantD)
		}
	}
}

// TestStatusNoUpstream checks a repo whose branch has no upstream: branch.ab is
// absent, so ahead/behind must be zero and Upstream empty (no crash, no garbage).
func TestStatusNoUpstream(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_SYSTEM="+os.DevNull,
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@e.com",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-m", "initial")

	st, err := GetStatus(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
	if st.Upstream != "" {
		t.Errorf("upstream = %q, want empty", st.Upstream)
	}
	if st.Ahead != 0 || st.Behind != 0 {
		t.Errorf("ahead/behind = %d/%d, want 0/0", st.Ahead, st.Behind)
	}
	if st.Dirty() {
		t.Error("want clean working tree")
	}
}

// TestAgainstRealGit clones a local repo, advances the origin, fetches, and
// checks that GetStatus/DiffStat/DefaultBranch report the repo as behind with
// the right line counts. No network is involved (the remote is a local path).
func TestAgainstRealGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	ctx := context.Background()
	base := t.TempDir()
	remote := filepath.Join(base, "remote")
	local := filepath.Join(base, "local")

	run := func(dir string, args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_CONFIG_GLOBAL="+os.DevNull,
			"GIT_CONFIG_SYSTEM="+os.DevNull,
			"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
			"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	run(base, "init", "-b", "main", remote)
	write(filepath.Join(remote, "f.txt"), "line1\nline2\n")
	run(remote, "add", ".")
	run(remote, "commit", "-m", "initial")

	run(base, "clone", remote, local)

	// Advance the remote by two lines, then make the local aware via fetch.
	write(filepath.Join(remote, "f.txt"), "line1\nline2\nline3\nline4\n")
	run(remote, "commit", "-am", "add two lines")
	run(local, "fetch")

	st, err := GetStatus(ctx, local)
	if err != nil {
		t.Fatal(err)
	}
	if st.Branch != "main" {
		t.Errorf("branch = %q, want main", st.Branch)
	}
	if st.Upstream != "origin/main" {
		t.Errorf("upstream = %q, want origin/main", st.Upstream)
	}
	if st.Behind != 1 || st.Ahead != 0 {
		t.Errorf("ahead/behind = %d/%d, want 0/1", st.Ahead, st.Behind)
	}

	if def, err := DefaultBranch(ctx, local); err != nil || def != "origin/main" {
		t.Errorf("DefaultBranch = %q, %v; want origin/main", def, err)
	}

	added, deleted, err := DiffStat(ctx, local, "@{u}")
	if err != nil {
		t.Fatal(err)
	}
	if added != 2 || deleted != 0 {
		t.Errorf("DiffStat = +%d -%d, want +2 -0", added, deleted)
	}

	// Introduce an uncommitted change and confirm the dirty counters move.
	write(filepath.Join(local, "f.txt"), "changed\nline2\n")
	st, err = GetStatus(ctx, local)
	if err != nil {
		t.Fatal(err)
	}
	if st.Modified != 1 || !st.Dirty() {
		t.Errorf("after edit: modified=%d dirty=%v, want 1/true", st.Modified, st.Dirty())
	}
}
