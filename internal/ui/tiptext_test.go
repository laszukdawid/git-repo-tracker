package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

var tipNow = time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC)

func TestFormatWorkTree(t *testing.T) {
	cases := []struct {
		name string
		repo monitor.RepoState
		want string
	}{
		{"clean", monitor.RepoState{}, "No uncommitted changes"},
		{"one modified", monitor.RepoState{Modified: 1}, "1 file changed"},
		{"several kinds", monitor.RepoState{Modified: 4, Untracked: 2}, "4 files changed, 2 new files"},
		{"staged and deleted", monitor.RepoState{Staged: 1, Deleted: 3}, "1 file staged, 3 files deleted"},
		{"one new file", monitor.RepoState{Untracked: 1}, "1 new file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatWorkTree(tc.repo); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFormatBranch(t *testing.T) {
	up := "origin/main"
	cases := []struct {
		name string
		repo monitor.RepoState
		want string
	}{
		{"detached", monitor.RepoState{Detached: true, Branch: "detached"}, "Not on a branch (detached HEAD)"},
		{"no upstream", monitor.RepoState{Branch: "main"}, "On main, no upstream branch set"},
		{"level", monitor.RepoState{Branch: "main", Upstream: up}, "On main, level with origin/main"},
		{"behind", monitor.RepoState{Branch: "main", Upstream: up, Behind: 12}, "On main, 12 commits behind origin/main"},
		{"behind one", monitor.RepoState{Branch: "main", Upstream: up, Behind: 1}, "On main, 1 commit behind origin/main"},
		{"ahead", monitor.RepoState{Branch: "main", Upstream: up, Ahead: 2}, "On main, 2 commits ahead of origin/main"},
		{"diverged", monitor.RepoState{Branch: "main", Upstream: up, Ahead: 3, Behind: 1}, "On main, 3 commits ahead and 1 behind origin/main"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatBranch(tc.repo); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHumanizeLong(t *testing.T) {
	cases := []struct {
		name string
		when time.Time
		want string
	}{
		{"never", time.Time{}, "never"},
		{"seconds", tipNow.Add(-20 * time.Second), "just now"},
		{"one minute", tipNow.Add(-time.Minute), "1 minute ago"},
		{"minutes", tipNow.Add(-5 * time.Minute), "5 minutes ago"},
		{"hours", tipNow.Add(-2 * time.Hour), "2 hours ago"},
		{"future clock skew", tipNow.Add(time.Hour), "just now"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := humanizeLong(tc.when, tipNow); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}

	// Beyond a week the exact date is shown rather than a vague interval.
	old := tipNow.AddDate(0, 0, -30)
	if got := humanizeLong(old, tipNow); !strings.Contains(got, old.Local().Format("2006-01-02")) {
		t.Errorf("month-old stamp = %q, want an absolute date", got)
	}
}

func TestFormatSyncTimes(t *testing.T) {
	never := formatSyncTimes(monitor.RepoState{}, tipNow)
	if never != "never fetched · never pulled" {
		t.Errorf("untouched repo = %q", never)
	}

	r := monitor.RepoState{LastFetch: tipNow.Add(-5 * time.Minute), LastPull: tipNow.Add(-2 * time.Hour)}
	got := formatSyncTimes(r, tipNow)
	if got != "Fetched 5 minutes ago · pulled 2 hours ago" {
		t.Errorf("got %q", got)
	}
}

// The tooltip is the only place the status glyphs are explained, so it must
// always carry the working tree, the branch and the sync times — and never the
// path, which already heads the expanded detail panel.
func TestRepoTooltipComposition(t *testing.T) {
	r := monitor.RepoState{
		Path: "/work/api", Name: "api", Branch: "main", Upstream: "origin/main",
		Behind: 3, Modified: 2, LastFetch: tipNow.Add(-time.Minute),
	}
	got := repoTooltip(r, tipNow)
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines for a healthy repo, got %d: %q", len(lines), got)
	}
	if lines[0] != "2 files changed" {
		t.Errorf("line 1 = %q", lines[0])
	}
	if !strings.Contains(lines[1], "3 commits behind") {
		t.Errorf("line 2 = %q", lines[1])
	}
	if !strings.Contains(lines[2], "never pulled") {
		t.Errorf("line 3 = %q", lines[2])
	}
	if strings.Contains(got, r.Path) {
		t.Error("tooltip must not repeat the repo path — it heads the detail panel")
	}
}

func TestRepoTooltipLeadsWithError(t *testing.T) {
	r := monitor.RepoState{Name: "api", Branch: "main", FetchErr: "could not read from remote repository"}
	lines := strings.Split(repoTooltip(r, tipNow), "\n")
	if len(lines) != 4 {
		t.Fatalf("want 4 lines when there is an error, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], "Fetch error:") {
		t.Errorf("error should lead, got %q", lines[0])
	}
}

func TestKeepFreshTooltip(t *testing.T) {
	got := keepFreshTooltip(monitor.RepoState{KeepFresh: true}, tipNow)
	if !strings.Contains(got, "No automatic pull yet") {
		t.Errorf("never-pulled = %q", got)
	}
	got = keepFreshTooltip(monitor.RepoState{KeepFresh: true, LastAutoPull: tipNow.Add(-3 * time.Minute)}, tipNow)
	if !strings.Contains(got, "3 minutes ago") {
		t.Errorf("recently pulled = %q", got)
	}
	// A manual pull must not be reported as an automatic one.
	got = keepFreshTooltip(monitor.RepoState{KeepFresh: true, LastPull: tipNow}, tipNow)
	if !strings.Contains(got, "No automatic pull yet") {
		t.Errorf("manual pull leaked into the auto badge: %q", got)
	}
}
