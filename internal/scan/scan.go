// Package scan discovers git repositories beneath configured root directories.
// The walk is pruned aggressively — it stops descending the moment it finds a
// repo and skips ignored directory names — so even large trees scan quickly.
package scan

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
)

// Discover walks every root concurrently and returns the absolute paths of all
// git repositories found, deduplicated and sorted. A directory containing a
// .git entry is recorded and not descended into, so nested repos / submodules
// are not double-counted. Directory names present in ignore are pruned.
func Discover(ctx context.Context, roots []config.Root, ignore []string) []string {
	ignoreSet := make(map[string]bool, len(ignore))
	for _, n := range ignore {
		if n = strings.TrimSpace(n); n != "" {
			ignoreSet[n] = true
		}
	}

	var (
		mu    sync.Mutex
		found = map[string]bool{}
		wg    sync.WaitGroup
	)
	for _, root := range roots {
		wg.Add(1)
		go func(r config.Root) {
			defer wg.Done()
			repos := walkRoot(ctx, r, ignoreSet)
			mu.Lock()
			for _, p := range repos {
				found[p] = true
			}
			mu.Unlock()
		}(root)
	}
	wg.Wait()

	out := make([]string, 0, len(found))
	for p := range found {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// IsRepo reports whether dir is the top level of a git repository, i.e. it
// contains a .git entry (a directory for normal clones, a file for worktrees
// and submodules).
func IsRepo(dir string) bool {
	info, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil && (info.IsDir() || info.Mode().IsRegular())
}

func walkRoot(ctx context.Context, root config.Root, ignore map[string]bool) []string {
	base := config.ExpandPath(root.Path)
	if base == "" {
		return nil
	}
	baseDepth := strings.Count(filepath.Clean(base), string(os.PathSeparator))

	var repos []string
	_ = filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entry: skip it but keep walking siblings
		}
		if ctx.Err() != nil {
			return filepath.SkipAll
		}
		if !d.IsDir() {
			return nil
		}
		// Prune ignored directories first, so an ignored dir that happens to be a
		// repo (e.g. a clone under node_modules/vendor) is skipped, not reported.
		if path != base && ignore[d.Name()] {
			return filepath.SkipDir
		}
		if IsRepo(path) {
			repos = append(repos, path)
			return filepath.SkipDir // a repo is a leaf; never descend into it
		}
		if root.Depth > 0 {
			depth := strings.Count(filepath.Clean(path), string(os.PathSeparator)) - baseDepth
			if depth >= root.Depth {
				return filepath.SkipDir // reached the configured depth limit
			}
		}
		return nil
	})
	return repos
}
