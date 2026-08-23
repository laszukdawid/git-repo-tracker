package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// This file turns a repository's state into the sentences shown on hover. The
// status column is a set of coloured glyphs, which is compact but says nothing
// on its own — an orange dot means "you have uncommitted changes here" and
// nothing in the UI used to say so. The tooltip is where that is spelled out,
// so everything here is deliberately plain language rather than git jargon.
//
// Every function is pure and takes `now` as a parameter, so the whole file is
// testable without a clock or a Fyne app.

// repoTooltip is the multi-line tip shown when the pointer rests on a repo row.
// Lines, in order: the error if there is one, the working tree, the branch, and
// when the repo was last synchronised. The path is deliberately absent — it is
// already the first line of the expanded detail panel.
func repoTooltip(r monitor.RepoState, now time.Time) string {
	var lines []string
	if msg := repoErrorMsg(r); msg != "" {
		lines = append(lines, msg)
	}
	if msg := formatOperation(r); msg != "" {
		lines = append(lines, msg)
	}
	lines = append(lines, formatWorkTree(r), formatBranch(r), formatSyncTimes(r, now))
	return strings.Join(lines, "\n")
}

// keepFreshTooltip explains the permanent badge on a keep-fresh repository and
// reports when its last automatic pull ran — the feedback that was missing when
// toggling keep-fresh appeared to do nothing at all.
func keepFreshTooltip(r monitor.RepoState, now time.Time) string {
	if r.LastAutoPull.IsZero() {
		return "Kept fresh automatically\nNo automatic pull yet"
	}
	return "Kept fresh automatically\nLast automatic pull " + humanizeLong(r.LastAutoPull, now)
}

// formatOperation explains the warning triangle: a half-finished merge or
// rebase, or conflicts still to resolve. It is stated first because nothing else
// about the repository can be acted on until it is settled.
func formatOperation(r monitor.RepoState) string {
	var parts []string
	switch r.Operation {
	case "merge":
		parts = append(parts, "A merge is in progress")
	case "rebase":
		parts = append(parts, "A rebase is in progress")
	case "cherry-pick":
		parts = append(parts, "A cherry-pick is in progress")
	case "revert":
		parts = append(parts, "A revert is in progress")
	case "bisect":
		parts = append(parts, "A bisect is in progress")
	}
	if r.Conflicts > 0 {
		parts = append(parts, plural(r.Conflicts, "file", "files")+" with conflicts")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + " — finish or abort it first"
}

// formatWorkTree describes uncommitted work in plain words. This is the line
// that explains the dirty mark beside the name.
func formatWorkTree(r monitor.RepoState) string {
	var parts []string
	if r.Modified > 0 {
		parts = append(parts, plural(r.Modified, "file", "files")+" changed")
	}
	if r.Staged > 0 {
		parts = append(parts, plural(r.Staged, "file", "files")+" staged")
	}
	if r.Deleted > 0 {
		parts = append(parts, plural(r.Deleted, "file", "files")+" deleted")
	}
	if r.Untracked > 0 {
		parts = append(parts, plural(r.Untracked, "new file", "new files"))
	}
	if len(parts) == 0 {
		return "No uncommitted changes"
	}
	return strings.Join(parts, ", ")
}

// formatBranch names the current branch and how it stands against its remote,
// spelled out rather than as ↑/↓ counters.
func formatBranch(r monitor.RepoState) string {
	if r.Detached {
		return "Not on a branch (detached HEAD)"
	}
	branch := r.Branch
	if branch == "" {
		return "Branch unknown"
	}
	against := r.Upstream
	if against == "" {
		return "On " + branch + ", no upstream branch set"
	}
	switch {
	case r.Ahead > 0 && r.Behind > 0:
		return fmt.Sprintf("On %s, %s ahead and %d behind %s",
			branch, plural(r.Ahead, "commit", "commits"), r.Behind, against)
	case r.Behind > 0:
		return fmt.Sprintf("On %s, %s behind %s",
			branch, plural(r.Behind, "commit", "commits"), against)
	case r.Ahead > 0:
		return fmt.Sprintf("On %s, %s ahead of %s",
			branch, plural(r.Ahead, "commit", "commits"), against)
	default:
		return fmt.Sprintf("On %s, level with %s", branch, against)
	}
}

// formatSyncTimes reports the last fetch and the last pull. They are different
// events and the distinction matters: a fetch only updates what the app knows,
// while a pull is what actually moved the working copy.
func formatSyncTimes(r monitor.RepoState, now time.Time) string {
	fetched := "never fetched"
	if !r.LastFetch.IsZero() {
		fetched = "Fetched " + humanizeLong(r.LastFetch, now)
	}
	pulled := "never pulled"
	if !r.LastPull.IsZero() {
		pulled = "pulled " + humanizeLong(r.LastPull, now)
	}
	return fetched + " · " + pulled
}

// humanizeLong renders a past instant verbosely, for tooltips where there is
// room for a real sentence. It differs from humanizeTime (used in the compact
// detail panel) in two ways: it spells the units out, and a zero time reads
// "never" rather than "unknown" — for a pull stamp, never having happened is a
// fact, not missing information.
func humanizeLong(t, now time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return "just now" // clock skew; don't claim the future
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d.Minutes()), "minute", "minutes") + " ago"
	case d < 6*time.Hour:
		return plural(int(d.Hours()), "hour", "hours") + " ago"
	case sameDay(t, now):
		return "today " + t.Local().Format("15:04")
	case sameDay(t, now.AddDate(0, 0, -1)):
		return "yesterday " + t.Local().Format("15:04")
	case d < 7*24*time.Hour:
		return t.Local().Format("Mon 15:04")
	default:
		return t.Local().Format("2006-01-02 15:04")
	}
}

func sameDay(a, b time.Time) bool {
	a, b = a.Local(), b.Local()
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// plural renders a count with the right noun form: "1 file", "3 files".
func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
