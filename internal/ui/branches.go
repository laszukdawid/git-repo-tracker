package ui

import (
	"errors"
	"fmt"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/dialog"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
	"github.com/laszukdawid/git-repo-tracker/internal/ui/actions"
)

// The Branches section: loading it, acting on it, and keeping the popover the
// right height while it changes. Everything in this file runs on the main
// thread except the bodies of the goroutines, which hand back through fyne.Do.

// branchKey identifies one branch of one repository.
type branchKey struct{ path, branch string }

func (a *App) worktreeSectionFor(path string) worktreeSectionState {
	return worktreeSectionState{
		open: a.worktreeOpen[path], loading: a.worktreeLoading[path],
		gen: a.worktreeGen[path], list: a.worktrees[path],
	}
}

func (a *App) bumpWorktreeGen(path string) { a.worktreeGen[path]++ }

// toggleWorktrees reloads on every reopen. A linked checkout can be added or
// removed by another terminal without changing the primary repository status,
// so a long-lived cached list would otherwise quietly lie.
func (a *App) toggleWorktrees(r monitor.RepoState) {
	open := !a.worktreeOpen[r.Path]
	a.worktreeOpen[r.Path] = open
	if open && !a.worktreeLoading[r.Path] {
		a.fetchWorktrees(r)
	}
	a.bumpWorktreeGen(r.Path)
	a.reflowExpanded()
}

func (a *App) refreshWorktrees(r monitor.RepoState) {
	if !a.worktreeLoading[r.Path] {
		a.fetchWorktrees(r)
	}
}

func (a *App) fetchWorktrees(r monitor.RepoState) {
	path := r.Path
	a.worktreeLoading[path] = true
	a.bumpWorktreeGen(path)
	go func() {
		list := a.mgr.Worktrees(path)
		fyne.Do(func() {
			delete(a.worktreeLoading, path)
			a.worktrees[path] = &list
			a.bumpWorktreeGen(path)
			if a.expandedPath == path {
				a.reflowExpanded()
			}
		})
	}()
}

func (a *App) openWorktreeIDE(worktree monitor.WorktreeInfo) {
	name := worktree.Branch
	if worktree.Detached || name == "" {
		name = "detached"
	}
	a.openInIDE(monitor.RepoState{Path: worktree.Path, Name: name, Branch: worktree.Branch})
}

func (a *App) openWorktreeFolder(worktree monitor.WorktreeInfo) {
	if err := actions.Run(config.ActionOpenFolder, "", worktree.Path); err != nil {
		dialog.ShowError(err, a.win)
	}
}

// branchSectionFor assembles the section state for one repository. It is the
// ONLY constructor: listUpdate and the height probe both call it, which is what
// keeps the measured height and the rendered row in agreement.
func (a *App) branchSectionFor(path string) branchSectionState {
	sec := branchSectionState{
		open:     a.branchOpen[path],
		loading:  a.branchLoading[path],
		gen:      a.branchGen[path],
		list:     a.branches[path],
		limit:    a.branchLimit[path],
		fetching: a.branchFetching[path],
	}
	// Only collect the per-branch maps when the section is actually open; a
	// closed section renders one line and needs neither.
	if !sec.open {
		return sec
	}
	for key, busy := range a.branchBusy {
		if key.path == path && busy {
			if sec.busy == nil {
				sec.busy = map[string]bool{}
			}
			sec.busy[key.branch] = true
		}
	}
	for key, msg := range a.branchErr {
		if key.path == path && msg != "" {
			if sec.errs == nil {
				sec.errs = map[string]string{}
			}
			sec.errs[key.branch] = msg
		}
	}
	return sec
}

// bumpBranchGen marks a repository's branch state as changed, so the row knows
// to rebuild its section and the height memo knows to re-measure.
func (a *App) bumpBranchGen(path string) { a.branchGen[path]++ }

// toggleBranches folds the section open or closed, loading the branches the
// first time it is opened. Nothing runs git until then — that is the whole point
// of the section starting closed.
func (a *App) toggleBranches(r monitor.RepoState) {
	open := !a.branchOpen[r.Path]
	a.branchOpen[r.Path] = open
	if open {
		bl := a.branches[r.Path]
		// A failed load is retried by opening the section again.
		if (bl == nil || bl.Err != "") && !a.branchLoading[r.Path] {
			a.fetchBranches(r)
		}
	}
	a.bumpBranchGen(r.Path)
	a.reflowExpanded()
}

// fetchBranches reads a repository's branches off the main thread.
func (a *App) fetchBranches(r monitor.RepoState) {
	path := r.Path
	a.branchLoading[path] = true
	a.bumpBranchGen(path)
	go func() {
		bl := a.mgr.Branches(path)
		fyne.Do(func() {
			delete(a.branchLoading, path)
			a.branches[path] = &bl
			a.bumpBranchGen(path)
			if a.expandedPath == path {
				a.reflowExpanded()
			}
		})
	}()
}

// startBranchSync brings one branch into line with its remote: fetch, then pull,
// push, or merge — whichever that branch actually needs. The verb is decided
// after the fetch, from the branch's real state, not from the counts the row was
// showing a minute ago.
func (a *App) startBranchSync(r monitor.RepoState, b monitor.BranchInfo) {
	key := branchKey{r.Path, b.Name}
	if a.branchBusy[key] {
		return
	}
	a.branchBusy[key] = true
	delete(a.branchErr, key) // retrying clears the previous complaint
	a.bumpBranchGen(r.Path)
	a.reflowExpanded()

	go func() {
		did, err := a.mgr.SyncBranch(r.Path, b.Name)
		fyne.Do(func() {
			delete(a.branchBusy, key)
			// Either way the fetch may have moved other branches, so the listing
			// is stale whatever happened to this one.
			delete(a.branches, r.Path)
			switch {
			case errors.Is(err, monitor.ErrBranchUpToDate):
				// Not a failure, and not silence either: the control was pressed
				// and has to answer.
				a.note(b.Name + " is already up to date")
			case err != nil:
				a.branchErr[key] = branchErrorMessage(err)
			default:
				a.note(syncNote(did, b.Name, b.Upstream))
				// The commit detail is stale too, but only if what moved was the
				// branch the repository is standing on.
				if b.Current {
					delete(a.details, r.Path)
				}
			}
			if a.expandedPath == r.Path && a.branchOpen[r.Path] {
				a.fetchBranches(r)
			}
			a.bumpBranchGen(r.Path)
			a.reflowExpanded()
			a.refresh()
		})
	}()
}

// startTrackBranch creates a local branch for a remote-only one.
func (a *App) startTrackBranch(r monitor.RepoState, b monitor.BranchInfo) {
	key := branchKey{r.Path, b.Name}
	if a.branchBusy[key] {
		return
	}
	a.branchBusy[key] = true
	delete(a.branchErr, key)
	a.bumpBranchGen(r.Path)
	a.reflowExpanded()

	go func() {
		err := a.mgr.TrackBranch(r.Path, b.Name)
		fyne.Do(func() {
			delete(a.branchBusy, key)
			if err != nil {
				a.branchErr[key] = branchErrorMessage(err)
			} else {
				delete(a.branches, r.Path) // a dim row becomes a real one
				if a.expandedPath == r.Path && a.branchOpen[r.Path] {
					a.fetchBranches(r)
				}
			}
			a.bumpBranchGen(r.Path)
			a.reflowExpanded()
		})
	}()
}

// startBranchWorktree opens the checkout that already holds a branch, or asks
// the monitor to create a safe linked worktree first. Git and filesystem work
// stays off the main thread; only the resulting editor launch touches the UI.
func (a *App) startBranchWorktree(r monitor.RepoState, b monitor.BranchInfo) {
	key := branchKey{r.Path, b.Name}
	if a.branchBusy[key] {
		return
	}
	a.branchBusy[key] = true
	delete(a.branchErr, key)
	a.bumpBranchGen(r.Path)
	a.reflowExpanded()

	go func() {
		worktree, err := a.mgr.EnsureBranchWorktree(r.Path, b)
		fyne.Do(func() {
			delete(a.branchBusy, key)
			if err != nil {
				a.branchErr[key] = branchErrorMessage(err)
			} else {
				delete(a.branches, r.Path)
				delete(a.worktrees, r.Path)
				if a.expandedPath == r.Path && a.branchOpen[r.Path] {
					a.fetchBranches(r)
				}
				if a.expandedPath == r.Path && a.worktreeOpen[r.Path] {
					a.fetchWorktrees(r)
				}
				a.openInIDE(monitor.RepoState{Path: worktree, Name: b.Name, Branch: b.Name})
			}
			a.bumpBranchGen(r.Path)
			a.reflowExpanded()
		})
	}()
}

// dismissBranchError clears one inline message.
func (a *App) dismissBranchError(r monitor.RepoState, branch string) {
	delete(a.branchErr, branchKey{r.Path, branch})
	a.bumpBranchGen(r.Path)
	a.reflowExpanded()
}

// reflowExpanded re-lays the list and glides the popover to its new height. It
// is the single path for every change that can resize the expanded row — the
// section opening, branches finishing loading, an error appearing or being
// dismissed — so those never fight each other over the window height.
func (a *App) reflowExpanded() {
	a.suppressAutoResize = true
	a.applyFilter()
	a.suppressAutoResize = false
	a.animatePopoverTo(a.desiredPopoverHeight())
}

// invalidateBranchesFor drops branch listings the monitor has overtaken. A
// background fetch or an automatic pull moves refs underneath us, and comparing
// the stamps captured when the listing was read against the fresh snapshot says
// exactly which repositories that happened to.
func (a *App) invalidateBranchesFor(snapshot []monitor.RepoState) {
	moved := map[string]bool{}
	for _, r := range snapshot {
		bl := a.branches[r.Path]
		if bl == nil {
			// Nothing loaded for this repository, but the search index may still
			// hold stale names — track the movement either way.
			if !r.LastFetch.IsZero() && a.lastSeenFetch[r.Path] != r.LastFetch {
				moved[r.Path] = true
			}
			continue
		}
		if r.LastFetch.After(bl.FetchedAt) || r.LastPull.After(bl.PulledAt) {
			moved[r.Path] = true
			delete(a.branches, r.Path)
			a.bumpBranchGen(r.Path)
			if a.expandedPath == r.Path && a.branchOpen[r.Path] && !a.branchLoading[r.Path] {
				a.fetchBranches(r)
			}
		}
	}
	for _, r := range snapshot {
		if a.lastSeenFetch == nil {
			a.lastSeenFetch = map[string]time.Time{}
		}
		a.lastSeenFetch[r.Path] = r.LastFetch
	}
	a.invalidateIndexFor(snapshot, moved)
}

// pruneBranchState drops everything belonging to repositories that have gone.
func (a *App) pruneBranchState(live map[string]bool) {
	for path := range a.worktrees {
		if !live[path] {
			delete(a.worktrees, path)
		}
	}
	for path := range a.worktreeOpen {
		if !live[path] {
			delete(a.worktreeOpen, path)
		}
	}
	for path := range a.worktreeLoading {
		if !live[path] {
			delete(a.worktreeLoading, path)
		}
	}
	for path := range a.worktreeGen {
		if !live[path] {
			delete(a.worktreeGen, path)
		}
	}
	for path := range a.branches {
		if !live[path] {
			delete(a.branches, path)
		}
	}
	for path := range a.branchOpen {
		if !live[path] {
			delete(a.branchOpen, path)
		}
	}
	for path := range a.branchLoading {
		if !live[path] {
			delete(a.branchLoading, path)
		}
	}
	for path := range a.branchGen {
		if !live[path] {
			delete(a.branchGen, path)
		}
	}
	for path := range a.branchLimit {
		if !live[path] {
			delete(a.branchLimit, path)
		}
	}
	for path := range a.branchFetching {
		if !live[path] {
			delete(a.branchFetching, path)
		}
	}
	for key := range a.branchBusy {
		if !live[key.path] {
			delete(a.branchBusy, key)
		}
	}
	// A dismissible error must not outlive the repository it belongs to.
	for key := range a.branchErr {
		if !live[key.path] {
			delete(a.branchErr, key)
		}
	}
}

// showMoreBranches reveals another page of a repository's branches.
func (a *App) showMoreBranches(r monitor.RepoState) {
	limit := a.branchLimit[r.Path]
	if limit <= 0 {
		limit = branchPageSize
	}
	a.branchLimit[r.Path] = limit + branchPageSize
	a.bumpBranchGen(r.Path)
	a.reflowExpanded()
}

// syncNote reports which of the three things the control turned out to do.
func syncNote(did monitor.BranchAction, branch, upstream string) string {
	switch did {
	case monitor.BranchPull:
		return "Pulled " + branch
	case monitor.BranchPush:
		return "Pushed " + branch
	case monitor.BranchMerge:
		return "Merged " + upstream + " into " + branch + " — push when you are ready"
	default:
		return branch + " is already up to date"
	}
}

// fetchNote is the sentence a finished fetch leaves in the status bar.
//
// A fetch is invisible by nature: it moves refs nobody is looking at, and on a
// repository that was already current it changes nothing at all. Without a line
// like this, pressing the button and pressing a dead button feel identical.
func fetchNote(repo string, moved int, err error) string {
	switch {
	case err != nil:
		return "Fetch failed: " + repo
	case moved == 1:
		return "Fetched " + repo + " · 1 branch updated"
	case moved > 1:
		return fmt.Sprintf("Fetched %s · %d branches updated", repo, moved)
	default:
		return "Fetched " + repo + " · already current"
	}
}

// note puts one short sentence in the status bar for a few seconds. It is the
// app's only way to answer an action that succeeded without changing anything
// visible.
func (a *App) note(msg string) {
	a.transient = msg
	a.transientUntil = time.Now().Add(transientHold)
	a.rearmExpiry()
	a.updateFooter()
}

// copyBranchName puts a branch name on the clipboard. It is the one action every
// branch offers: most branches are level with their upstream and so have nothing
// to pull, and a hover that reveals no control at all reads as "branches have no
// actions".
func (a *App) copyBranchName(name string) {
	if name == "" {
		return
	}
	if cb := fyne.CurrentApp().Clipboard(); cb != nil {
		cb.SetContent(name)
	}
	a.note("Copied " + name)
}

// fetchRepoNow fetches one repository because the user asked. Until this existed
// there was no way to make branch distances current on a root with autoFetch
// off, so no branch was ever behind and no branch ever offered a pull.
func (a *App) fetchRepoNow(r monitor.RepoState) {
	path := r.Path
	if a.branchFetching[path] {
		return
	}
	a.branchFetching[path] = true
	a.bumpBranchGen(path)
	a.reflowExpanded()

	go func() {
		moved, err := a.mgr.FetchRepo(path)
		fyne.Do(func() {
			delete(a.branchFetching, path)
			a.note(fetchNote(r.Name, moved, err))
			// The listing was read before the fetch moved the remote refs.
			delete(a.branches, path)
			if a.bindex != nil {
				a.bindex.invalidate(path)
			}
			if err == nil && a.expandedPath == path && a.branchOpen[path] {
				a.fetchBranches(r)
			}
			a.bumpBranchGen(path)
			a.reflowExpanded()
			a.refresh()
		})
	}()
}

// branchErrorMessage renders a refused branch operation. The UI never parses
// git's wording itself — the git layer has already classified it.
func branchErrorMessage(err error) string {
	return monitor.BranchErrorMessage(err)
}

// expandedHeightKey memoises the expanded row's measured height. applyFilter
// runs on every keystroke and probes that row each time; with a branch section
// open that is around twenty widgets and sixty text measurements per character
// typed. The key covers everything the probe's height depends on.
type expandedHeightKey struct {
	path             string
	detail           *monitor.Details // pointer identity: details are replaced, never mutated
	branchGen        uint64
	worktreeGen      uint64
	status           *rowStatus
	branchLoadedAt   time.Time
	worktreeLoadedAt time.Time
}
