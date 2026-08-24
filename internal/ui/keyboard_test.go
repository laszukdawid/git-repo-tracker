package ui

import (
	"testing"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

func TestMatchesQuery(t *testing.T) {
	r := monitor.RepoState{Name: "SamplePlugin", Branch: "feature/x", Path: "/tmp/work/sw67/plugins/SamplePlugin"}
	// Branch names the index has read for this repository.
	indexed := []string{"main", "release/24.4", "ticket-1553-cache"}
	cases := []struct {
		q    string
		want bool
	}{
		{"sample", true},         // name, case-insensitive
		{"feature", true},        // current branch
		{"sw67", true},           // parent folder, not in the name
		{"work sw67", true},      // every word must match somewhere
		{"sw67 plugins", true},   // order-independent
		{"release/24", true},     // a branch that is not the current one
		{"ticket-1553", true},    // ditto, by ticket number
		{"sample release", true}, // one word from the name, one from a branch
		{"sw66", false},          // sibling folder
		{"sample sw66", false},   // one word missing → no match
		{"release/25", false},    // no branch matches
	}
	for _, tc := range cases {
		if got := matchesQuery(r, tc.q, indexed); got != tc.want {
			t.Errorf("matchesQuery(%q) = %v, want %v", tc.q, got, tc.want)
		}
	}

	// Before the index has read a repository, only its current branch is
	// searchable — the search must still work, just with less reach.
	if matchesQuery(r, "release/24", nil) {
		t.Error("an unindexed repository should not match a branch it has not reported")
	}
	if !matchesQuery(r, "feature", nil) {
		t.Error("the current branch is searchable without the index")
	}
}

// A repository pulled in by a branch nobody can see must say which branch that
// was: searching "v3" and being shown a row whose branch reads "v2" is
// indistinguishable from a broken search otherwise.
func TestMatchQueryReportsTheMatchingBranch(t *testing.T) {
	r := monitor.RepoState{Name: "shop", Branch: "v2", Path: "/tmp/www/shop"}
	indexed := []string{"v2", "origin/v3-rewrite", "main"}

	hit, ok := matchQuery(r, "v3", indexed)
	if !ok || hit != "origin/v3-rewrite" {
		t.Errorf("matchQuery(v3) = %q, %v; want the branch that matched", hit, ok)
	}

	// Matching on the repository's own text needs no explanation.
	if hit, ok := matchQuery(r, "shop", indexed); !ok || hit != "" {
		t.Errorf("matchQuery(shop) = %q, %v; want no hint", hit, ok)
	}
	// The current branch is the repository's own text too.
	if hit, ok := matchQuery(r, "v2", indexed); !ok || hit != "" {
		t.Errorf("matchQuery(v2) = %q, %v; want no hint", hit, ok)
	}
	if _, ok := matchQuery(r, "v4", indexed); ok {
		t.Error("v4 matches nothing and must not match")
	}
}

func navApp(items ...popoverItem) *App {
	return &App{visible: items, selIdx: -1}
}

func repoItem(path string) popoverItem {
	return popoverItem{repo: monitor.RepoState{Path: path, Name: path}}
}

func TestMoveSelectionSkipsHeadersAndClamps(t *testing.T) {
	a := navApp(
		popoverItem{header: true, root: "~/a"},
		repoItem("/a/one"),
		repoItem("/a/two"),
		popoverItem{header: true, root: "~/b"},
		repoItem("/b/three"),
	)
	a.moveSelection(1) // from none: lands on the first repo, not the header
	if a.selIdx != 1 || a.selPath != "/a/one" {
		t.Fatalf("first Down: idx=%d path=%q", a.selIdx, a.selPath)
	}
	a.moveSelection(1)
	a.moveSelection(1) // crosses the second header
	if a.selIdx != 4 {
		t.Fatalf("Down over header: idx=%d", a.selIdx)
	}
	a.moveSelection(1) // clamp at the end
	if a.selIdx != 4 {
		t.Fatalf("clamp at end: idx=%d", a.selIdx)
	}
	a.moveSelection(-1)
	a.moveSelection(-1)
	a.moveSelection(-1)
	if a.selIdx != 1 {
		t.Fatalf("Up back to first repo: idx=%d", a.selIdx)
	}
	a.moveSelection(-1) // header above; selection stays
	if a.selIdx != 1 {
		t.Fatalf("clamp at start: idx=%d", a.selIdx)
	}
	if r, ok := a.selectedRepo(); !ok || r.Path != "/a/one" {
		t.Fatalf("selectedRepo = %v %v", r.Path, ok)
	}
}

func TestReconcileSelectionFollowsRepoThenFirstMatch(t *testing.T) {
	a := navApp(popoverItem{header: true}, repoItem("/x"), repoItem("/y"))
	a.setSelection(2)

	// Same repo still visible at a new index → highlight follows it.
	a.visible = []popoverItem{popoverItem{header: true}, repoItem("/y")}
	a.reconcileSelection()
	if a.selIdx != 1 || a.selPath != "/y" {
		t.Fatalf("follow: idx=%d path=%q", a.selIdx, a.selPath)
	}

	// Repo filtered out while a query is active → first match is pre-selected
	// so Return opens it without an extra keystroke.
	a.query = "z"
	a.visible = []popoverItem{popoverItem{header: true}, repoItem("/z1"), repoItem("/z2")}
	a.reconcileSelection()
	if a.selIdx != 1 || a.selPath != "/z1" {
		t.Fatalf("first match: idx=%d path=%q", a.selIdx, a.selPath)
	}

	// No query → nothing is pre-selected.
	a.query = ""
	a.selPath = "/gone"
	a.reconcileSelection()
	if a.selIdx != -1 || a.selPath != "" {
		t.Fatalf("cleared: idx=%d path=%q", a.selIdx, a.selPath)
	}
	if _, ok := a.selectedRepo(); ok {
		t.Fatal("selectedRepo should report none")
	}
}
