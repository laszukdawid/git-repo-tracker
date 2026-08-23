package monitor

import (
	"sort"
	"sync"
	"time"
)

// This file answers one question the monitor could not answer before: "what are
// you doing right now?". The registry told the UI only *that* something changed,
// never that a fetch pass was running, how far along it was, or which repository
// it was working on — so the footer could report totals but never progress.
//
// The design is deliberately pull-based. Nothing here is pushed to the UI: the
// Manager emits a bare signal, and the UI reads value copies on its own thread.
// That makes a data race structurally impossible rather than merely avoided.

// maxEvents bounds the completion queue. Events are announcements, not a log —
// if nobody drained them for this long the oldest ones are no longer worth
// showing, and an undrained queue (the headless CLI never drains) must not grow.
const maxEvents = 32

// ActivityKind is the sort of background work in progress, ordered by how much
// the user cares to see it when several passes overlap.
type ActivityKind int

const (
	ActivityIdle       ActivityKind = iota
	ActivityScanning                // walking the configured roots; no per-repo count
	ActivityRefreshing              // cheap local-status pass
	ActivityFetching                // remote pass: fetch, then status
	ActivityPulling                 // one or more fast-forwards in flight
	ActivityPushing                 // publishing a branch to its remote
)

// Activity is a snapshot of what the monitor is doing at one instant.
type Activity struct {
	Kind  ActivityKind
	Repo  string // name of a repository being worked on; "" when not applicable
	Done  int    // repositories finished in this pass
	Total int    // repositories in this pass; 0 when the work isn't countable
}

// Busy reports whether any background work is in progress.
func (a Activity) Busy() bool { return a.Kind != ActivityIdle }

// ActivityEvent is one completed operation worth announcing once. Unlike
// Activity (which describes the present) an event describes something that
// already happened and would otherwise leave no trace — most importantly a
// keep-fresh pull, which the user never asked for interactively.
type ActivityEvent struct {
	Repo string // repository name
	Auto bool   // true when it came from a keep-fresh background pull
	Err  string // empty on success
	At   time.Time
}

// activityOp is one in-flight pass. The counter lives here, on the pass, and
// never on the workers: a refresh runs refreshWorkers goroutines against one
// shared job channel, so per-worker counters could not produce a coherent
// "8 of 36".
type activityOp struct {
	t        *activityTracker
	id       int64
	kind     ActivityKind
	total    int
	done     int
	inflight []string // repo names, oldest first
}

// start records that a repository is now being worked on.
func (o *activityOp) start(repo string) {
	if o == nil {
		return
	}
	o.t.mu.Lock()
	o.inflight = append(o.inflight, repo)
	o.t.mu.Unlock()
	o.t.signal()
}

// finish records that a repository is done. Call it from a defer so a cancelled
// worker still decrements and the counter cannot stall short of its total.
func (o *activityOp) finish(repo string) {
	if o == nil {
		return
	}
	o.t.mu.Lock()
	o.done++
	for i, name := range o.inflight {
		if name == repo {
			o.inflight = append(o.inflight[:i], o.inflight[i+1:]...)
			break
		}
	}
	o.t.mu.Unlock()
	o.t.signal()
}

// end removes the pass from the tracker.
func (o *activityOp) end() {
	if o == nil {
		return
	}
	o.t.mu.Lock()
	delete(o.t.ops, o.id)
	o.t.mu.Unlock()
	o.t.signal()
}

// activityTracker holds every in-flight pass plus the queue of completions
// waiting to be announced. All state is behind mu; notify is called after the
// lock is released so a listener can call back in without deadlocking.
type activityTracker struct {
	mu     sync.Mutex
	ops    map[int64]*activityOp
	nextID int64
	events []ActivityEvent
	notify func()
}

func newActivityTracker() *activityTracker {
	return &activityTracker{ops: map[int64]*activityOp{}}
}

// setNotify installs the change signal. Passing nil disables signalling, which
// is what a headless caller wants.
func (t *activityTracker) setNotify(fn func()) {
	t.mu.Lock()
	t.notify = fn
	t.mu.Unlock()
}

func (t *activityTracker) signal() {
	t.mu.Lock()
	fn := t.notify
	t.mu.Unlock()
	if fn != nil {
		fn()
	}
}

// begin opens a pass. total is the number of repositories it will cover, or 0
// when the work has no countable unit. The returned op must be ended.
func (t *activityTracker) begin(kind ActivityKind, total int) *activityOp {
	t.mu.Lock()
	t.nextID++
	op := &activityOp{t: t, id: t.nextID, kind: kind, total: total}
	t.ops[op.id] = op
	t.mu.Unlock()
	t.signal()
	return op
}

// post queues a completion for announcement, dropping the oldest once the queue
// is full.
func (t *activityTracker) post(ev ActivityEvent) {
	t.mu.Lock()
	t.events = append(t.events, ev)
	if len(t.events) > maxEvents {
		t.events = t.events[len(t.events)-maxEvents:]
	}
	t.mu.Unlock()
	t.signal()
}

// snapshot collapses every in-flight pass into the single line the UI shows.
func (t *activityTracker) snapshot() Activity {
	t.mu.Lock()
	defer t.mu.Unlock()
	return pickActivity(t.ops)
}

// drain removes and returns the queued completions.
func (t *activityTracker) drain() []ActivityEvent {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) == 0 {
		return nil
	}
	out := t.events
	t.events = nil
	return out
}

// pickActivity reduces the in-flight passes to one Activity. The local loop, the
// remote loop and user-initiated pulls genuinely overlap, so "last writer wins"
// would flicker between unrelated passes.
//
// It picks the highest-priority kind present and then aggregates every pass of
// that kind, which is what lets a batch of individually-started pulls report a
// correct denominator: "Pulling 3/12" without any batch API.
//
// Repo is the oldest still-running entry rather than the newest — with eight
// workers churning, the newest changes several times a second while the oldest
// is usually the slow one actually worth naming.
func pickActivity(ops map[int64]*activityOp) Activity {
	var best ActivityKind
	for _, op := range ops {
		if op.kind > best {
			best = op.kind
		}
	}
	if best == ActivityIdle {
		return Activity{}
	}

	act := Activity{Kind: best}
	// Sort by id so the "oldest" repo is deterministic rather than map-order.
	ids := make([]int64, 0, len(ops))
	for id, op := range ops {
		if op.kind == best {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		op := ops[id]
		act.Done += op.done
		act.Total += op.total
		if act.Repo == "" && len(op.inflight) > 0 {
			act.Repo = op.inflight[0]
		}
	}
	return act
}
