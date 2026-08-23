package monitor

import (
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
)

func TestPickActivityIdleWhenNoOps(t *testing.T) {
	if got := pickActivity(map[int64]*activityOp{}); got.Busy() {
		t.Fatalf("empty tracker should be idle, got %+v", got)
	}
}

func TestPickActivityPrefersHigherKindAndAggregates(t *testing.T) {
	tr := newActivityTracker()
	// A background fetch pass and two individually-started pulls, exactly what
	// happens when the user hits "update all" during a scheduled refresh.
	fetch := tr.begin(ActivityFetching, 36)
	fetch.start("beta")
	pullA := tr.begin(ActivityPulling, 1)
	pullA.start("api")
	pullB := tr.begin(ActivityPulling, 1)
	pullB.start("web")
	pullB.finish("web")

	got := tr.snapshot()
	if got.Kind != ActivityPulling {
		t.Errorf("Kind = %v, want ActivityPulling (pulling outranks fetching)", got.Kind)
	}
	if got.Total != 2 {
		t.Errorf("Total = %d, want 2 (both pull ops aggregated)", got.Total)
	}
	if got.Done != 1 {
		t.Errorf("Done = %d, want 1", got.Done)
	}
	// The oldest in-flight entry wins so the name does not flicker between workers.
	if got.Repo != "api" {
		t.Errorf("Repo = %q, want \"api\"", got.Repo)
	}

	pullA.end()
	pullB.end()
	if got := tr.snapshot(); got.Kind != ActivityFetching || got.Total != 36 {
		t.Errorf("after pulls end: %+v, want the fetch pass to resurface", got)
	}
	fetch.end()
	if tr.snapshot().Busy() {
		t.Error("tracker should be idle once every op ended")
	}
}

func TestActivityOpReportsOldestInflight(t *testing.T) {
	tr := newActivityTracker()
	op := tr.begin(ActivityFetching, 3)
	op.start("first")
	op.start("second")
	if got := tr.snapshot().Repo; got != "first" {
		t.Fatalf("Repo = %q, want \"first\"", got)
	}
	op.finish("first")
	if got := tr.snapshot().Repo; got != "second" {
		t.Fatalf("after finishing first, Repo = %q, want \"second\"", got)
	}
	if got := tr.snapshot().Done; got != 1 {
		t.Fatalf("Done = %d, want 1", got)
	}
}

// The counter must stay coherent when refreshWorkers goroutines share one op —
// the whole reason the tally lives on the pass and not on the workers.
func TestActivityOpConcurrentWorkers(t *testing.T) {
	tr := newActivityTracker()
	const n = 50
	op := tr.begin(ActivityFetching, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := string(rune('a' + i%26))
			op.start(name)
			op.finish(name)
		}(i)
	}
	wg.Wait()
	got := tr.snapshot()
	if got.Done != n {
		t.Errorf("Done = %d, want %d", got.Done, n)
	}
	if got.Repo != "" {
		t.Errorf("Repo = %q, want empty once nothing is in flight", got.Repo)
	}
}

func TestActivityEventsDrainOnceAndAreBounded(t *testing.T) {
	tr := newActivityTracker()
	tr.post(ActivityEvent{Repo: "api", At: time.Unix(1, 0)})
	tr.post(ActivityEvent{Repo: "web", Auto: true, At: time.Unix(2, 0)})

	got := tr.drain()
	if len(got) != 2 || got[0].Repo != "api" || !got[1].Auto {
		t.Fatalf("drain returned %+v", got)
	}
	if again := tr.drain(); again != nil {
		t.Errorf("second drain returned %+v, want nil — events deliver once", again)
	}

	// An undrained queue must not grow without bound: the headless CLI never drains.
	for i := 0; i < maxEvents*3; i++ {
		tr.post(ActivityEvent{Repo: "x"})
	}
	if n := len(tr.drain()); n != maxEvents {
		t.Errorf("queue held %d events, want it capped at %d", n, maxEvents)
	}
}

func TestActivityTrackerSignalsOnEveryTransition(t *testing.T) {
	tr := newActivityTracker()
	var mu sync.Mutex
	calls := 0
	tr.setNotify(func() { mu.Lock(); calls++; mu.Unlock() })

	op := tr.begin(ActivityFetching, 1)
	op.start("api")
	op.finish("api")
	op.end()
	tr.post(ActivityEvent{Repo: "api"})

	mu.Lock()
	defer mu.Unlock()
	if calls != 5 {
		t.Errorf("notify fired %d times, want 5 (begin, start, finish, end, post)", calls)
	}
}

// A nil op is what a caller holds when activity reporting is not wired up; it
// must be inert rather than panic.
func TestNilActivityOpIsInert(t *testing.T) {
	var op *activityOp
	op.start("x")
	op.finish("x")
	op.end()
}

func TestScheduledFetchUsesRepoMutationLock(t *testing.T) {
	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))
	path := filepath.Join(t.TempDir(), "repo")
	mgr := New(&config.Config{}, nil, nil)
	defer mgr.cancel()
	mgr.repos[path] = &RepoState{Path: path}

	lockValue, _ := mgr.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	locked := true
	defer func() {
		if locked {
			lock.Unlock()
		}
	}()

	started := make(chan struct{}, 1)
	mgr.act.setNotify(func() {
		if mgr.act.snapshot().Repo == filepath.Base(path) {
			select {
			case started <- struct{}{}:
			default:
			}
		}
	})
	done := make(chan struct{})
	go func() {
		mgr.refreshAllWithFetch(true, true)
		close(done)
	}()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled fetch did not start")
	}
	select {
	case <-done:
		t.Fatal("scheduled fetch completed while the repo mutation lock was held")
	case <-time.After(250 * time.Millisecond):
	}

	lock.Unlock()
	locked = false
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("scheduled fetch did not resume after the repo mutation lock was released")
	}
}
