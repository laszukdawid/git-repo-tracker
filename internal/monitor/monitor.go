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

	LinesAdded   int  `json:"linesAdded"`   // lines the repo is behind by
	LinesDeleted int  `json:"linesDeleted"` // lines removed upstream since fork point
	Dirty        bool `json:"dirty"`

	LastLocal time.Time `json:"lastLocal"`
	LastFetch time.Time `json:"lastFetch"`
	Err       string    `json:"err,omitempty"`      // last local-status error
	FetchErr  string    `json:"fetchErr,omitempty"` // last fetch error
}

// Updatable reports whether the repo is behind its remote and worth pulling.
func (r RepoState) Updatable() bool { return r.Behind > 0 }

// Manager owns the repo registry and the refresh schedulers. Safe for
// concurrent use.
type Manager struct {
	cfg      *config.Config
	onChange func()
	log      func(string, ...any)

	mu    sync.RWMutex
	repos map[string]*RepoState

	notifyC chan struct{}
	trigger chan struct{}

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
	return &Manager{
		cfg:      cfg,
		onChange: onChange,
		log:      logf,
		repos:    loadCache(),
		notifyC:  make(chan struct{}, 1),
		trigger:  make(chan struct{}, 1),
		ctx:      ctx,
		cancel:   cancel,
	}
}

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
	m.mu.RLock()
	out := make([]RepoState, 0, len(m.repos))
	for _, r := range m.repos {
		out = append(out, *r)
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

// RefreshNow performs a synchronous discovery + status pass for headless callers.
// When withFetch is true it also runs fetch for roots that allow it. The UI should
// keep using Refresh so it never blocks the main thread.
func (m *Manager) RefreshNow(withFetch bool) {
	m.discover()
	m.refreshAll(withFetch)
}

// Pull fast-forwards a repo to its upstream and refreshes its status. It blocks
// (it hits the network), so callers should invoke it from a goroutine.
func (m *Manager) Pull(path string) error {
	if err := git.Pull(m.ctx, path); err != nil {
		m.update(path, func(r *RepoState) { r.FetchErr = err.Error() })
		m.notify()
		return err
	}
	m.update(path, func(r *RepoState) { r.FetchErr = ""; r.LastFetch = time.Now() })
	m.refreshOne(path)
	m.notify()
	return nil
}

// Details bundles a repo's path with the latest local and origin commit info,
// for the expandable detail panel. It blocks on git, so call it from a goroutine.
type Details struct {
	Path       string
	OriginRef  string
	LocalHash  string
	LocalTime  time.Time
	LocalMsg   string
	OriginHash string
	OriginTime time.Time
	OriginMsg  string
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
	paths := scan.Discover(m.ctx, m.cfg.RootList(), m.cfg.IgnoreDirs())
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
				if withFetch && fetchSet[p] {
					if err := git.Fetch(m.ctx, p); err != nil {
						m.update(p, func(r *RepoState) { r.FetchErr = err.Error() })
					} else {
						m.update(p, func(r *RepoState) { r.FetchErr = ""; r.LastFetch = time.Now() })
					}
				}
				m.refreshOne(p)
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

// notifier coalesces notify signals: after the first signal it waits one debounce
// window, then fires a single onChange. This keeps a 70-repo refresh from
// triggering 70 tray rebuilds.
func (m *Manager) notifier() {
	defer m.wg.Done()
	timer := time.NewTimer(notifyDebounce)
	timer.Stop()
	pending := false
	for {
		select {
		case <-m.ctx.Done():
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
		}
	}
}
