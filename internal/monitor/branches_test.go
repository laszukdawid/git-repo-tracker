package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Branch listings must never reach the on-disk cache. They are large, they go
// stale the moment anyone commits, and the cache is painted at startup before
// any git runs — showing a day-old branch list there would be worse than showing
// none.
func TestBranchesAreNotPersisted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("GIT_REPO_TRACKER_CACHE", path)

	if err := saveCache([]RepoState{{Path: "/x", Name: "x", Branch: "main", Behind: 2}}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, word := range []string{"branches", "remoteOnly", "worktree", "loadedAt"} {
		if strings.Contains(string(data), word) {
			t.Errorf("the cache carries branch data (%q): %s", word, data)
		}
	}

	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if v, _ := doc["version"].(float64); int(v) != cacheVersion {
		t.Errorf("cache version = %v, want %d — adding branches must not have bumped it", doc["version"], cacheVersion)
	}
}

func TestBranchErrorMessagePassesThroughPlainErrors(t *testing.T) {
	if got := BranchErrorMessage(nil); got != "" {
		t.Errorf("nil error = %q, want empty", got)
	}
}

// PlanBranch is the single decision the control is drawn from and acts on. If
// these two ever disagree, the button says one thing and does another.
func TestPlanBranch(t *testing.T) {
	cases := []struct {
		name string
		info BranchInfo
		want BranchAction
	}{
		{"level", BranchInfo{Upstream: "origin/x"}, BranchNothing},
		{"behind", BranchInfo{Upstream: "origin/x", Behind: 3}, BranchPull},
		{"ahead — a push, not a failed fast-forward",
			BranchInfo{Upstream: "origin/x", Ahead: 34}, BranchPush},
		{"diverged with somewhere to merge",
			BranchInfo{Upstream: "origin/x", Ahead: 4, Behind: 142, Worktree: "/w/x"}, BranchMerge},
		{"diverged with nowhere to merge",
			BranchInfo{Upstream: "origin/x", Ahead: 4, Behind: 142}, BranchBlocked},
		{"no upstream", BranchInfo{Ahead: 2}, BranchNothing},
		{"upstream gone", BranchInfo{Upstream: "origin/x", Behind: 3, Gone: true}, BranchNothing},
		{"remote-only", BranchInfo{RemoteOnly: true, Behind: 9}, BranchNothing},
	}
	for _, tc := range cases {
		if got := PlanBranch(tc.info); got != tc.want {
			t.Errorf("%s: PlanBranch = %v, want %v", tc.name, got, tc.want)
		}
	}
}
