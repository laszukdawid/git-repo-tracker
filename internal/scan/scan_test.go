package scan

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
)

func mkRepo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(path, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestDiscover(t *testing.T) {
	base := t.TempDir()

	mkRepo(t, filepath.Join(base, "repoA"))
	mkRepo(t, filepath.Join(base, "repoA", "nested")) // inside a repo: must be skipped
	mkRepo(t, filepath.Join(base, "plain", "repoB"))  // repo one level down
	mkRepo(t, filepath.Join(base, "node_modules", "junk"))
	if err := os.MkdirAll(filepath.Join(base, "plain", "empty"), 0o755); err != nil {
		t.Fatal(err)
	}

	roots := []config.Root{{Path: base, Depth: 0}}
	got := Discover(context.Background(), roots, []string{"node_modules"})

	want := []string{
		filepath.Join(base, "plain", "repoB"),
		filepath.Join(base, "repoA"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover() = %v, want %v", got, want)
	}
}

func TestDiscoverIgnoredDirIsRepo(t *testing.T) {
	base := t.TempDir()
	mkRepo(t, filepath.Join(base, "vendor"))   // an ignored dir that is itself a repo
	mkRepo(t, filepath.Join(base, "realrepo")) // a normal repo

	got := Discover(context.Background(), []config.Root{{Path: base}}, []string{"vendor"})
	want := []string{filepath.Join(base, "realrepo")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Discover() = %v, want %v (ignored dir that is a repo must be pruned)", got, want)
	}
}

func TestDiscoverDepthLimit(t *testing.T) {
	base := t.TempDir()
	mkRepo(t, filepath.Join(base, "a", "b", "c", "deep")) // depth 4 below base

	// Depth 2 must not reach the deep repo.
	if got := Discover(context.Background(), []config.Root{{Path: base, Depth: 2}}, nil); len(got) != 0 {
		t.Errorf("depth 2: got %v, want none", got)
	}
	// Depth 5 reaches it.
	got := Discover(context.Background(), []config.Root{{Path: base, Depth: 5}}, nil)
	want := []string{filepath.Join(base, "a", "b", "c", "deep")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("depth 5: got %v, want %v", got, want)
	}
}
