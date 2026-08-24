package ui

import (
	"testing"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

func TestStatusLinePriority(t *testing.T) {
	idle := monitor.Activity{}
	fetching := monitor.Activity{Kind: monitor.ActivityFetching, Repo: "api", Done: 7, Total: 36}

	if got := statusLine(idle, "", 36, 2); got != "36 repos · 2 behind" {
		t.Errorf("idle = %q", got)
	}
	if got := statusLine(fetching, "", 36, 2); got != "Fetching 8/36 · api" {
		t.Errorf("busy = %q", got)
	}
	// A just-finished message outranks live progress: it is the thing the user
	// was waiting to hear.
	if got := statusLine(fetching, "Updated 3 repos", 36, 2); got != "Updated 3 repos" {
		t.Errorf("transient = %q", got)
	}
}

func TestOperationLine(t *testing.T) {
	cases := []struct {
		name string
		act  monitor.Activity
		want string
	}{
		{"idle", monitor.Activity{}, ""},
		{"scanning", monitor.Activity{Kind: monitor.ActivityScanning}, "Scanning folders…"},
		{"checking", monitor.Activity{Kind: monitor.ActivityRefreshing, Done: 0, Total: 12}, "Checking 1/12"},
		{"pulling with name", monitor.Activity{Kind: monitor.ActivityPulling, Repo: "web", Done: 2, Total: 12}, "Pulling 3/12 · web"},
		{"no total", monitor.Activity{Kind: monitor.ActivityFetching, Repo: "api"}, "Fetching… · api"},
		// The in-flight repo counts as current, but the count must never exceed
		// the total once the last one is being finished.
		{"clamped", monitor.Activity{Kind: monitor.ActivityFetching, Done: 36, Total: 36}, "Fetching 36/36"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := operationLine(tc.act); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestFoldEventsAndTallyMessage(t *testing.T) {
	cases := []struct {
		name   string
		events []monitor.ActivityEvent
		want   string
	}{
		{"nothing", nil, ""},
		{"one manual", []monitor.ActivityEvent{{Repo: "api"}}, "Pulled api"},
		{"one automatic", []monitor.ActivityEvent{{Repo: "web", Auto: true}}, "Auto-pulled web"},
		{"one failure", []monitor.ActivityEvent{{Repo: "api", Err: "diverged"}}, "Pull failed: api"},
		{"a batch", []monitor.ActivityEvent{{Repo: "a"}, {Repo: "b"}, {Repo: "c"}}, "Updated 3 repos"},
		{"mixed batch", []monitor.ActivityEvent{{Repo: "a"}, {Repo: "b"}, {Repo: "c", Err: "x"}}, "Updated 2 repos · 1 failed"},
		{"all failed", []monitor.ActivityEvent{{Repo: "a", Err: "x"}, {Repo: "b", Err: "y"}}, "2 pulls failed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tallyMessage(foldEvents(pullTally{}, tc.events))
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

// Events arriving in several drains must produce one sentence, not one per
// drain — that is the whole reason the tally is accumulated rather than emitted.
func TestFoldEventsAccumulatesAcrossDrains(t *testing.T) {
	tally := foldEvents(pullTally{}, []monitor.ActivityEvent{{Repo: "a"}})
	tally = foldEvents(tally, []monitor.ActivityEvent{{Repo: "b"}, {Repo: "c"}})
	if got := tallyMessage(tally); got != "Updated 3 repos" {
		t.Errorf("got %q, want \"Updated 3 repos\"", got)
	}
}

func TestNextExpiry(t *testing.T) {
	now := time.Date(2026, 8, 22, 15, 0, 0, 0, time.UTC)

	if _, ok := nextExpiry(now, map[string]*rowStatus{}, time.Time{}); ok {
		t.Error("nothing pending should report no expiry")
	}

	// A running pull has no expiry; only finished states lapse.
	rows := map[string]*rowStatus{"/a": {phase: rowPulling}}
	if _, ok := nextExpiry(now, rows, time.Time{}); ok {
		t.Error("an in-flight pull should not schedule an expiry")
	}

	rows["/b"] = &rowStatus{phase: rowPulled, expires: now.Add(3 * time.Second)}
	rows["/c"] = &rowStatus{phase: rowPulled, expires: now.Add(9 * time.Second)}
	d, ok := nextExpiry(now, rows, now.Add(5*time.Second))
	if !ok || d != 3*time.Second {
		t.Errorf("got %v (%v), want the soonest, 3s", d, ok)
	}

	// The status-bar message can be the soonest thing to lapse.
	d, ok = nextExpiry(now, map[string]*rowStatus{}, now.Add(2*time.Second))
	if !ok || d != 2*time.Second {
		t.Errorf("transient-only: got %v (%v)", d, ok)
	}

	// An already-lapsed deadline fires immediately rather than in the past.
	if d, ok := nextExpiry(now, map[string]*rowStatus{}, now.Add(-time.Minute)); !ok || d != 0 {
		t.Errorf("overdue: got %v (%v), want 0", d, ok)
	}
}
