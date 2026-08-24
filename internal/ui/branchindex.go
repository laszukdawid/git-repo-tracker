package ui

import (
	"strings"
	"sync"

	"fyne.io/fyne/v2"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// Searching by branch name.
//
// The Branches section loads one repository's branches when you open it, which
// is right for browsing but useless for search: typing a branch name should
// find the repository holding it without having opened anything first.
//
// So the search keeps its own index — every repository's branch names, read
// once in the background and refreshed when a fetch or a pull moves that
// repository's refs. It is only names, so it stays small, and it is built off
// the UI thread with a bounded pool: on this machine, thirty-six repositories
// take a couple of hundred milliseconds in total.

// indexWorkers bounds how many repositories are read at once, matching the
// monitor's own refresh pool so the two cannot together swamp the machine.
const indexWorkers = 8

// branchIndex maps a repository path to its branch names, lower-cased for
// matching. It is written from a background goroutine and read on the main
// thread, so it carries its own lock rather than relying on the UI's.
type branchIndex struct {
	mu      sync.RWMutex
	names   map[string][]string
	pending map[string]bool
	built   bool
}

func newBranchIndex() *branchIndex {
	return &branchIndex{names: map[string][]string{}, pending: map[string]bool{}}
}

// get returns one repository's indexed branch names.
func (x *branchIndex) get(path string) []string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.names[path]
}

func (x *branchIndex) set(path string, names []string) {
	x.mu.Lock()
	x.names[path] = names
	delete(x.pending, path)
	x.mu.Unlock()
}

func (x *branchIndex) record(path string, bl monitor.BranchList) {
	if bl.Err != "" {
		x.mu.Lock()
		delete(x.names, path)
		delete(x.pending, path)
		x.mu.Unlock()
		return
	}
	names := make([]string, 0, len(bl.Branches))
	for _, b := range bl.Branches {
		names = append(names, strings.ToLower(b.Name))
	}
	x.set(path, names)
}

// claim reserves a repository for indexing, reporting whether this caller got
// it. It keeps two passes from reading the same repository at once.
func (x *branchIndex) claim(path string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.pending[path] {
		return false
	}
	x.pending[path] = true
	return true
}

// invalidate drops a repository's entry so the next pass re-reads it.
func (x *branchIndex) invalidate(path string) {
	x.mu.Lock()
	delete(x.names, path)
	x.mu.Unlock()
}

// prune forgets repositories that no longer exist.
func (x *branchIndex) prune(live map[string]bool) {
	x.mu.Lock()
	for path := range x.names {
		if !live[path] {
			delete(x.names, path)
		}
	}
	x.mu.Unlock()
}

// matches reports whether any of a repository's branches contains term.
func (x *branchIndex) matches(path, term string) bool {
	for _, name := range x.get(path) {
		if strings.Contains(name, term) {
			return true
		}
	}
	return false
}

// ensureBranchIndex reads the branch names of every tracked repository that has
// none yet, in the background. It is cheap to call repeatedly: repositories
// already indexed are skipped, and one already being read is not read twice.
func (a *App) ensureBranchIndex() {
	if a.bindex == nil {
		a.bindex = newBranchIndex()
	}
	var todo []string
	for _, r := range a.all {
		if a.bindex.get(r.Path) == nil && a.bindex.claim(r.Path) {
			todo = append(todo, r.Path)
		}
	}
	if len(todo) == 0 {
		return
	}

	go func() {
		jobs := make(chan string)
		var wg sync.WaitGroup
		for i := 0; i < indexWorkers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for path := range jobs {
					bl := a.mgr.Branches(path)
					a.bindex.record(path, bl)
				}
			}()
		}
		for _, path := range todo {
			jobs <- path
		}
		close(jobs)
		wg.Wait()

		// Re-run the filter so a search typed while the index was still building
		// picks up what it now knows.
		fyne.Do(func() {
			a.bindex.mu.Lock()
			a.bindex.built = true
			a.bindex.mu.Unlock()
			if a.popVisible && strings.TrimSpace(a.query) != "" {
				a.applyFilter()
			}
		})
	}()
}

// invalidateIndexFor drops index entries the monitor has overtaken, using the
// same stamps that invalidate a loaded branch listing.
func (a *App) invalidateIndexFor(snapshot []monitor.RepoState, moved map[string]bool) {
	if a.bindex == nil {
		return
	}
	for path := range moved {
		a.bindex.invalidate(path)
	}
}
