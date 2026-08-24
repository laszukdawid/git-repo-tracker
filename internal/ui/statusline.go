package ui

import (
	"fmt"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// The footer used to show only "36 repos · 0 behind", which is true but says
// nothing about the work the app does on its own — fetching every half hour,
// pulling keep-fresh repositories in the background. From the outside those were
// indistinguishable from the app doing nothing. This file composes the line that
// narrates them.
//
// Everything here is pure: given a state it returns a string. The timing and the
// state machine live in the UI layer; the wording lives here where it is testable.

// transientHold is how long a completion message stays before the status line
// falls back to the summary.
const transientHold = 4 * time.Second

// statusLine renders the footer's left slot. Priority is deliberate: a just-
// finished operation is the most interesting thing to say, then work in
// progress, and only when neither applies does the plain summary show.
func statusLine(act monitor.Activity, transient string, total, behind int) string {
	if transient != "" {
		return transient
	}
	if s := operationLine(act); s != "" {
		return s
	}
	return fmt.Sprintf("%d repos · %d behind", total, behind)
}

// operationLine describes work in progress, or "" when the monitor is idle.
func operationLine(act monitor.Activity) string {
	verb := ""
	switch act.Kind {
	case ActivityKindScanning:
		return "Scanning folders…"
	case ActivityKindRefreshing:
		verb = "Checking"
	case ActivityKindFetching:
		verb = "Fetching"
	case ActivityKindPulling:
		verb = "Pulling"
	case ActivityKindPushing:
		verb = "Pushing"
	default:
		return ""
	}

	s := verb
	if act.Total > 0 {
		// Count the in-flight repo as the current one, so the line reads
		// "Fetching 1/36" the moment work starts rather than sitting at 0.
		at := act.Done + 1
		if at > act.Total {
			at = act.Total
		}
		s = fmt.Sprintf("%s %d/%d", verb, at, act.Total)
	} else {
		s += "…"
	}
	if act.Repo != "" {
		s += " · " + act.Repo
	}
	return s
}

// Aliases so this file reads without the monitor. prefix on every branch.
const (
	ActivityKindScanning   = monitor.ActivityScanning
	ActivityKindRefreshing = monitor.ActivityRefreshing
	ActivityKindFetching   = monitor.ActivityFetching
	ActivityKindPulling    = monitor.ActivityPulling
	ActivityKindPushing    = monitor.ActivityPushing
)

// pullTally accumulates completions so a batch produces one sentence instead of
// a stream. Updating 12 repositories should say "Updated 12 repos" once, not
// flash twelve names in a second.
type pullTally struct {
	ok       int
	failed   int
	auto     int
	lastRepo string // the only repo name, when there is exactly one
	lastAuto string
}

func (t pullTally) empty() bool { return t.ok == 0 && t.failed == 0 }

// foldEvents merges freshly drained completions into the running tally.
func foldEvents(t pullTally, evs []monitor.ActivityEvent) pullTally {
	for _, ev := range evs {
		if ev.Err != "" {
			t.failed++
			t.lastRepo = ev.Repo
			continue
		}
		t.ok++
		t.lastRepo = ev.Repo
		if ev.Auto {
			t.auto++
			t.lastAuto = ev.Repo
		}
	}
	return t
}

// tallyMessage turns a finished batch into one line, or "" when nothing
// happened. A single automatic pull is named explicitly, because a keep-fresh
// repository moving on its own is exactly the event the user would otherwise
// never learn about.
func tallyMessage(t pullTally) string {
	switch {
	case t.empty():
		return ""
	case t.ok == 1 && t.failed == 0 && t.auto == 1:
		return "Auto-pulled " + t.lastAuto
	case t.ok == 1 && t.failed == 0:
		return "Pulled " + t.lastRepo
	case t.ok == 0 && t.failed == 1:
		return "Pull failed: " + t.lastRepo
	case t.failed == 0:
		return fmt.Sprintf("Updated %d repos", t.ok)
	case t.ok == 0:
		return fmt.Sprintf("%d pulls failed", t.failed)
	default:
		return fmt.Sprintf("Updated %d repos · %d failed", t.ok, t.failed)
	}
}

// nextExpiry returns how long until the soonest transient state lapses, and
// whether there is one at all. A single timer for the whole app is armed from
// this: with one timer per row, a batch of pulls finishing together would fire a
// dozen separate repaints.
func nextExpiry(now time.Time, rows map[string]*rowStatus, transientUntil time.Time) (time.Duration, bool) {
	var soonest time.Time
	consider := func(t time.Time) {
		if t.IsZero() {
			return
		}
		if soonest.IsZero() || t.Before(soonest) {
			soonest = t
		}
	}
	for _, st := range rows {
		if st != nil {
			consider(st.expires)
		}
	}
	consider(transientUntil)

	if soonest.IsZero() {
		return 0, false
	}
	d := soonest.Sub(now)
	if d < 0 {
		d = 0
	}
	return d, true
}
