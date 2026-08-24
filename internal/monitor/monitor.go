// Package monitor is the daemon layer: it owns the registry of discovered
// repositories and the background schedulers that keep their status fresh. It
// calls into the git and scan packages and reports changes back to the UI
// through a single onChange callback (debounced so a burst of per-repo updates
// becomes at most one UI rebuild per window).
package monitor

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/git"
	"github.com/laszukdawid/git-repo-tracker/internal/scan"
)

const (
	// refreshWorkers bounds how many git subprocesses run at once during a
	// refresh, so scanning dozens of repos can't fork-bomb the machine.
	refreshWorkers = 8
	// notifyDebounce coalesces rapid per-repo updates into one UI rebuild.
	notifyDebounce = 200 * time.Millisecond
	// activityDebounce paces progress updates. It is shorter than notifyDebounce
	// because it only repaints the status line, not the whole list.
	activityDebounce = 100 * time.Millisecond
)

// RepoState is a snapshot of one repository's tracked status. It is both the
// value handed to the UI and the unit persisted to the cache.
type RepoState struct {
	Path     string `json:"path"`
	Name     string `json:"name"`
	Branch   string `json:"branch"`
	Upstream string `json:"upstream"`
	Detached bool   `json:"detached"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`

	Staged    int `json:"staged"`
	Modified  int `json:"modified"`
	Deleted   int `json:"deleted"`
	Untracked int `json:"untracked"`
	Conflicts int `json:"conflicts,omitempty"`

	// Operation is the multi-step git operation in progress — "merge", "rebase",
	// "cherry-pick", "revert", "bisect" — or "". A repository in one has to be
	// finished or aborted before anything else is worth doing to it.
	Operation string `json:"operation,omitempty"`

	LinesAdded   int  `json:"linesAdded"`   // lines the repo is behind by
	LinesDeleted int  `json:"linesDeleted"` // lines removed upstream since fork point
	Dirty        bool `json:"dirty"`

	LastLocal time.Time `json:"lastLocal"`
	LastFetch time.Time `json:"lastFetch"`
	// LastPull is any successful fast-forward, manual or automatic; LastAutoPull
	// only the keep-fresh ones. Two fields because the keep-fresh badge reports
	// specifically when the *automatic* pull last ran, and a manual pull must not
	// be presented as one.
	LastPull     time.Time `json:"lastPull"`
	LastAutoPull time.Time `json:"lastAutoPull"`
	Err          string    `json:"err,omitempty"`      // last local-status error
	FetchErr     string    `json:"fetchErr,omitempty"` // last fetch error
	KeepFresh    bool      `json:"-"`                  // user opted into automatic fast-forward pulls
}

// Updatable reports whether the repo is behind its remote and worth pulling.
func (r RepoState) Updatable() bool { return r.Behind > 0 }

// Unsettled reports whether the repository is mid-merge, mid-rebase, or holding
// unresolved conflicts. It outranks every other state: nothing else about the
// repository is worth acting on until it is finished or aborted.
func (r RepoState) Unsettled() bool { return r.Operation != "" || r.Conflicts > 0 }

// NeedsPush reports whether the current branch has commits its upstream does
// not. It is deliberately separate from Updatable: "you have work to send" and
// "there is work to fetch" are different jobs, and one icon for both says
// nothing about either.
func (r RepoState) NeedsPush() bool { return r.Ahead > 0 && !r.Detached }

// UpdateResult records the outcome of one pull attempted by UpdateAll.
type UpdateResult struct {
	Path string `json:"path"`
	Err  string `json:"error,omitempty"`
}

// Manager owns the repo registry and the refresh schedulers. Safe for
// concurrent use.
type Manager struct {
	cfg        *config.Config
	onChange   func()
	onActivity func()
	log        func(string, ...any)

	// act records what the background loops are doing so the UI can narrate it.
	act *activityTracker

	mu    sync.RWMutex
	repos map[string]*RepoState
	// Different repositories may pull concurrently, but overlapping refresh loops
	// must never run two mutating git processes in the same working tree.
	pullLocks sync.Map // map[string]*sync.Mutex

	notifyC   chan struct{}
	activityC chan struct{}
	trigger   chan struct{}

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates a Manager seeded from the on-disk cache so callers can render
// immediately. onChange is invoked (from background goroutines) when state
// changes; logf receives human-readable messages. Call Start to begin polling.
func New(cfg *config.Config, onChange func(), logf func(string, ...any)) *Manager {
	if onChange == nil {
		onChange = func() {}
	}
	if logf == nil {
		logf = func(string, ...any) {}
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{
		cfg:        cfg,
		onChange:   onChange,
		onActivity: func() {},
		log:        logf,
		act:        newActivityTracker(),
		repos:      loadCache(),
		notifyC:    make(chan struct{}, 1),
		activityC:  make(chan struct{}, 1),
		trigger:    make(chan struct{}, 1),
		ctx:        ctx,
		cancel:     cancel,
	}
	m.act.setNotify(m.activityNotify)
	return m
}

// SetOnActivity installs the callback fired (debounced) whenever the monitor's
// progress changes. It is a setter rather than a New parameter so headless
// callers keep their existing two-argument construction and get no-op behaviour.
func (m *Manager) SetOnActivity(fn func()) {
	if fn == nil {
		fn = func() {}
	}
	m.onActivity = fn
}

// Activity returns what the monitor is doing right now, as a value copy the
// caller may read from any goroutine.
func (m *Manager) Activity() Activity { return m.act.snapshot() }

// DrainActivityEvents removes and returns the completions queued since the last
// call. Completions are announcements, so each is delivered exactly once.
func (m *Manager) DrainActivityEvents() []ActivityEvent { return m.act.drain() }

// Start kicks off the background loops. It returns immediately; the first paint
// happens synchronously from the cache.
func (m *Manager) Start() {
	m.onChange() // paint from cache before any git work
	m.wg.Add(3)
	go m.notifier()
	go m.localLoop()
	go m.remoteLoop()
}

// Stop cancels in-flight git operations, waits for the loops to exit, and saves
// the final state.
func (m *Manager) Stop() {
	m.cancel()
	m.wg.Wait()
	m.persist()
}

// Snapshot returns a copy of all repo states, sorted by name then path, for the
// UI to render.
func (m *Manager) Snapshot() []RepoState {
	keepFresh := m.cfg.KeepFreshRepos()
	m.mu.RLock()
	out := make([]RepoState, 0, len(m.repos))
	for _, r := range m.repos {
		copy := *r
		copy.KeepFresh = keepFresh[r.Path]
		out = append(out, copy)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if strings.EqualFold(out[i].Name, out[j].Name) {
			return out[i].Path < out[j].Path
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Counts returns the total number of tracked repos and how many are updatable.
func (m *Manager) Counts() (total, behind int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, r := range m.repos {
		total++
		if r.Behind > 0 {
			behind++
		}
	}
	return total, behind
}

// Refresh requests an immediate discovery + fetch pass (non-blocking).
func (m *Manager) Refresh() {
	select {
	case m.trigger <- struct{}{}:
	default: // a refresh is already queued
	}
}

// SetKeepFresh persists whether a repository should be fast-forwarded whenever
// a refresh discovers that it is behind. Enabling it queues a remote refresh so
// an apparently synced repository is checked immediately.
func (m *Manager) SetKeepFresh(path string, enabled bool) error {
	if err := m.cfg.SetKeepFresh(path, enabled); err != nil {
		return err
	}
	m.notify()
	if enabled {
		m.Refresh()
	}
	return nil
}

// RefreshNow performs a synchronous discovery + status pass for headless callers.
// When withFetch is true it also runs fetch for roots that allow it. The UI should
// keep using Refresh so it never blocks the main thread.
func (m *Manager) RefreshNow(withFetch bool) {
	m.discover()
	m.refreshAll(withFetch)
}

// UpdateAll fetches every discovered repository, then fast-forwards those that
// are behind. A manual update deliberately ignores autoFetch: that setting only
// controls the background remote-refresh loop.
func (m *Manager) UpdateAll() []UpdateResult {
	m.discover()
	m.refreshAllWithFetch(true, true)

	var targets []RepoState
	for _, r := range m.Snapshot() {
		if r.Updatable() {
			targets = append(targets, r)
		}
	}
	results := make([]UpdateResult, len(targets))
	if len(targets) == 0 {
		return results
	}

	type job struct {
		index int
		path  string
	}
	jobs := make(chan job)
	workers := min(refreshWorkers, len(targets))
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				result := UpdateResult{Path: j.path}
				if err := m.Pull(j.path); err != nil {
					result.Err = err.Error()
				}
				results[j.index] = result
			}
		}()
	}
	for i, r := range targets {
		jobs <- job{index: i, path: r.Path}
	}
	close(jobs)
	wg.Wait()

	m.persist()
	return results
}

// pullSource says who asked for a pull. It replaces an earlier boolean that
// doubled as both "re-check whether we're still behind" and "this came from
// keep-fresh" — conflating the two made it impossible to record honestly
// whether the last pull was automatic.
type pullSource int

const (
	pullManual pullSource = iota
	pullAuto              // keep-fresh; implies a behind re-check before pulling
)

// FetchRepo fetches one repository because the user asked for it, then
// re-reads its status.
//
// It deliberately ignores the root's autoFetch setting: that governs the
// background remote loop, not a click. Without this there is no way to make a
// branch listing current on a root with autoFetch off — every branch reads as
// level with its upstream, so no branch offers anything to do, and the section
// looks like it has no actions at all.
//
// It takes the same per-repo lock as a pull, so a fetch and a pull can never
// overlap on one repository.
func (m *Manager) FetchRepo(path string) (int, error) {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	name := filepath.Base(path)
	op := m.act.begin(ActivityFetching, 1)
	op.start(name)
	defer op.end()
	defer op.finish(name)

	// Snapshot the remote-tracking refs so the caller can say what came in. A
	// fetch that finds nothing and a fetch that updates forty branches are
	// indistinguishable otherwise, and "I pressed it and nothing happened" is
	// exactly what silence looks like.
	before, _ := git.RemoteRefs(m.ctx, path)

	if err := git.Fetch(m.ctx, path); err != nil {
		m.update(path, func(r *RepoState) { r.FetchErr = err.Error() })
		m.notify()
		return 0, err
	}
	after, _ := git.RemoteRefs(m.ctx, path)

	m.update(path, func(r *RepoState) {
		r.FetchErr = ""
		r.LastFetch = time.Now()
	})
	m.refreshOne(path)
	m.notify()
	return git.CountChangedRefs(before, after), nil
}

// Pull fast-forwards a repo to its upstream and refreshes its status. It blocks
// (it hits the network), so callers should invoke it from a goroutine.
func (m *Manager) Pull(path string) error {
	return m.pull(path, pullManual)
}

func (m *Manager) pull(path string, src pullSource) error {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	if src == pullAuto {
		m.refreshOne(path)
		if !m.canAutoPull(path) {
			return nil // no longer behind: nothing to announce, nothing to do
		}
	}

	// Opened only once the pull is certain to run, so a keep-fresh no-op never
	// flashes "Pulling" in the status bar.
	name := filepath.Base(path)
	op := m.act.begin(ActivityPulling, 1)
	op.start(name)
	defer op.end()
	defer op.finish(name)

	if err := git.Pull(m.ctx, path); err != nil {
		m.update(path, func(r *RepoState) { r.FetchErr = err.Error() })
		m.act.post(ActivityEvent{Repo: name, Auto: src == pullAuto, Err: err.Error(), At: time.Now()})
		m.notify()
		return err
	}
	now := time.Now()
	m.update(path, func(r *RepoState) {
		r.FetchErr = ""
		// A pull implies a fetch, so both stamps advance.
		r.LastFetch = now
		r.LastPull = now
		if src == pullAuto {
			r.LastAutoPull = now
		}
	})
	m.refreshOne(path)
	m.act.post(ActivityEvent{Repo: name, Auto: src == pullAuto, At: now})
	m.notify()
	return nil
}

// Details bundles a repo's path with the latest local and origin commit info,
// for the expandable detail panel. It blocks on git, so call it from a goroutine.
type Details struct {
	Path       string    `json:"path"`
	OriginRef  string    `json:"originRef"`
	LocalHash  string    `json:"localHash"`
	LocalTime  time.Time `json:"localTime"`
	LocalMsg   string    `json:"localMsg"`
	OriginHash string    `json:"originHash"`
	OriginTime time.Time `json:"originTime"`
	OriginMsg  string    `json:"originMsg"`
}

// Details gathers the latest local (HEAD) and origin commit for a repo.
func (m *Manager) Details(path string) Details {
	d := Details{Path: path}
	if c, err := git.LastCommit(m.ctx, path, "HEAD"); err == nil {
		d.LocalHash, d.LocalTime, d.LocalMsg = c.Hash, c.Time, c.Subject
	}

	ref := ""
	m.mu.RLock()
	if r, ok := m.repos[path]; ok {
		ref = r.Upstream
	}
	m.mu.RUnlock()
	if ref == "" {
		if def, err := git.DefaultBranch(m.ctx, path); err == nil {
			ref = def
		}
	}
	d.OriginRef = ref
	if ref != "" {
		if c, err := git.LastCommit(m.ctx, path, ref); err == nil {
			d.OriginHash, d.OriginTime, d.OriginMsg = c.Hash, c.Time, c.Subject
		}
	}
	return d
}

// remoteLoop discovers repos and runs the (network) fetch pass: once at startup,
// then on the configured interval, plus whenever Refresh is called. The interval
// is re-read each cycle via a fresh timer, so config changes apply without a
// restart.
func (m *Manager) remoteLoop() {
	defer m.wg.Done()
	m.discover()
	m.refreshAll(true)

	for {
		timer := time.NewTimer(m.cfg.FetchInterval())
		select {
		case <-m.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		case <-m.trigger:
			timer.Stop()
		}
		if m.ctx.Err() != nil {
			return
		}
		m.discover()
		m.refreshAll(true)
	}
}

// localLoop runs the cheap local-status pass on a short interval so dirty state
// and ahead/behind stay current between fetches.
func (m *Manager) localLoop() {
	defer m.wg.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(m.cfg.LocalRefresh()):
			m.refreshAll(false)
		}
	}
}

// discover walks the configured roots and reconciles the registry: new repos are
// added, repos that vanished are dropped.
func (m *Manager) discover() {
	op := m.act.begin(ActivityScanning, 0) // a directory walk has no per-repo unit
	paths := scan.Discover(m.ctx, m.cfg.RootList(), m.cfg.IgnoreDirs())
	op.end()
	// A cancelled walk (e.g. during Stop) returns a partial list; reconciling
	// against it would wrongly delete repos and then persist that damaged state.
	if m.ctx.Err() != nil {
		return
	}
	present := make(map[string]bool, len(paths))

	m.mu.Lock()
	for _, p := range paths {
		present[p] = true
		if _, ok := m.repos[p]; !ok {
			m.repos[p] = &RepoState{Path: p, Name: filepath.Base(p)}
		}
	}
	for p := range m.repos {
		if !present[p] {
			delete(m.repos, p)
		}
	}
	m.mu.Unlock()
	m.notify()
}

// refreshAll updates every tracked repo through a bounded worker pool. When
// withFetch is set, fetch-eligible repos are fetched first.
func (m *Manager) refreshAll(withFetch bool) {
	m.refreshAllWithFetch(withFetch, false)
}

// fetchForRefresh serializes a scheduled fetch with every repo and branch
// mutation that uses pullLocks. It releases the lock before keep-fresh calls
// pull, because pull acquires the same lock for its own mutation.
func (m *Manager) fetchForRefresh(path string) error {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()
	return git.Fetch(m.ctx, path)
}

// refreshAllWithFetch updates every tracked repo. forceFetch makes a
// user-requested update fetch every repository instead of only auto-fetch roots.
func (m *Manager) refreshAllWithFetch(withFetch, forceFetch bool) {
	m.mu.RLock()
	paths := make([]string, 0, len(m.repos))
	for p := range m.repos {
		paths = append(paths, p)
	}
	m.mu.RUnlock()
	if len(paths) == 0 {
		return
	}
	fetchSet := m.fetchEligible()
	keepFresh := m.cfg.KeepFreshRepos()

	// One op owns the counter for the whole pass. The workers only report which
	// repository they are on; if each kept its own tally, eight of them sharing a
	// job channel could never produce a coherent "8 of 36".
	kind := ActivityRefreshing
	if withFetch {
		kind = ActivityFetching
	}
	op := m.act.begin(kind, len(paths))
	defer op.end()

	jobs := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < refreshWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range jobs {
				if m.ctx.Err() != nil {
					continue // drain remaining jobs without work
				}
				name := filepath.Base(p)
				op.start(name)
				if withFetch && (forceFetch || fetchSet[p] || keepFresh[p]) {
					if err := m.fetchForRefresh(p); err != nil {
						m.update(p, func(r *RepoState) { r.FetchErr = err.Error() })
					} else {
						m.update(p, func(r *RepoState) { r.FetchErr = ""; r.LastFetch = time.Now() })
					}
				}
				m.refreshOne(p)
				if keepFresh[p] && m.canAutoPull(p) {
					if err := m.pull(p, pullAuto); err != nil {
						m.log("keep fresh pull %s failed: %v", p, err)
					}
				}
				op.finish(name)
				m.notify()
			}
		}()
	}
	for _, p := range paths {
		select {
		case <-m.ctx.Done():
		case jobs <- p:
			continue
		}
		break
	}
	close(jobs)
	wg.Wait()

	m.persist()
	m.notify()
}

func (m *Manager) canAutoPull(path string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	r, ok := m.repos[path]
	return ok && r.Behind > 0 && r.Err == "" && r.FetchErr == ""
}

// refreshOne recomputes a single repo's local status and behind/line stats.
func (m *Manager) refreshOne(path string) {
	st, err := git.GetStatus(m.ctx, path)
	if err != nil {
		m.update(path, func(r *RepoState) {
			r.Err = err.Error()
			r.LastLocal = time.Now()
		})
		return
	}

	// Pick a comparison ref: the branch's upstream if it has one, else the
	// remote's default branch.
	ref := st.Upstream
	if ref == "" {
		if def, derr := git.DefaultBranch(m.ctx, path); derr == nil {
			ref = def
		}
	}
	behind := st.Behind
	var added, deleted int
	if ref != "" {
		if a, d, derr := git.DiffStat(m.ctx, path, ref); derr == nil {
			added, deleted = a, d
		}
		if st.Upstream == "" { // branch.ab was absent; compute behind explicitly
			if b, berr := git.CountBehind(m.ctx, path, ref); berr == nil {
				behind = b
			}
		}
	}

	m.update(path, func(r *RepoState) {
		r.Branch = st.Branch
		r.Upstream = st.Upstream
		r.Detached = st.Detached
		r.Ahead = st.Ahead
		r.Behind = behind
		r.Staged = st.Staged
		r.Modified = st.Modified
		r.Deleted = st.Deleted
		r.Untracked = st.Untracked
		r.Conflicts = st.Conflicts
		r.Operation = st.Operation
		r.LinesAdded = added
		r.LinesDeleted = deleted
		r.Dirty = st.Dirty()
		r.Err = ""
		r.LastLocal = time.Now()
	})
}

// fetchEligible maps each tracked repo path to whether its root has autoFetch
// enabled. A repo belongs to the deepest configured root that contains it.
func (m *Manager) fetchEligible() map[string]bool {
	type expRoot struct {
		prefix string
		fetch  bool
	}
	var roots []expRoot
	for _, r := range m.cfg.RootList() {
		if p := config.ExpandPath(r.Path); p != "" {
			roots = append(roots, expRoot{prefix: p, fetch: r.AutoFetch})
		}
	}

	m.mu.RLock()
	paths := make([]string, 0, len(m.repos))
	for p := range m.repos {
		paths = append(paths, p)
	}
	m.mu.RUnlock()

	out := make(map[string]bool, len(paths))
	for _, p := range paths {
		best := -1
		for i, r := range roots {
			if p == r.prefix || strings.HasPrefix(p, r.prefix+string(filepath.Separator)) {
				if best == -1 || len(roots[i].prefix) > len(roots[best].prefix) {
					best = i
				}
			}
		}
		if best >= 0 {
			out[p] = roots[best].fetch
		}
	}
	return out
}

// update applies a mutation to one repo state if it still exists.
func (m *Manager) update(path string, mutate func(*RepoState)) {
	m.mu.Lock()
	if r, ok := m.repos[path]; ok {
		mutate(r)
	}
	m.mu.Unlock()
}

// persist writes the current registry to the cache.
func (m *Manager) persist() {
	if err := saveCache(m.Snapshot()); err != nil {
		m.log("cache save failed: %v", err)
	}
}

// notify signals the debouncer that state changed.
func (m *Manager) notify() {
	select {
	case m.notifyC <- struct{}{}:
	default:
	}
}

// activityNotify signals the debouncer that progress changed. Like notify it is
// a non-blocking send on a cap-1 channel, which matters more here than it looks:
// the headless CLI drives the Manager without Start(), so nothing is draining
// this channel and a blocking send would wedge every refresh and pull.
func (m *Manager) activityNotify() {
	select {
	case m.activityC <- struct{}{}:
	default:
	}
}

// notifier coalesces notify signals: after the first signal it waits one debounce
// window, then fires a single onChange. This keeps a 70-repo refresh from
// triggering 70 tray rebuilds.
// It carries a second, faster timer for progress. The two are kept apart on
// purpose: onChange drives a full UI rebuild (snapshot, regrouping, list and
// tray refresh), which is far too heavy to run at the rate progress ticks —
// while onActivity only repaints one line of text.
func (m *Manager) notifier() {
	defer m.wg.Done()
	timer := time.NewTimer(notifyDebounce)
	timer.Stop()
	actTimer := time.NewTimer(activityDebounce)
	actTimer.Stop()
	pending, actPending := false, false
	for {
		select {
		case <-m.ctx.Done():
			timer.Stop()
			actTimer.Stop()
			return
		case <-m.notifyC:
			if !pending {
				pending = true
				timer.Reset(notifyDebounce)
			}
		case <-timer.C:
			if pending {
				pending = false
				m.onChange()
			}
		case <-m.activityC:
			if !actPending {
				actPending = true
				actTimer.Reset(activityDebounce)
			}
		case <-actTimer.C:
			if actPending {
				actPending = false
				m.onActivity()
			}
		}
	}
}
