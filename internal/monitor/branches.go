package monitor

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/laszukdawid/git-repo-tracker/internal/git"
)

// defaultRemote is the remote whose branches are offered for creating local
// counterparts. Matching DefaultBranch(), which already assumes origin.
const defaultRemote = "origin"

// BranchInfo is one branch as the UI sees it. Like RepoState flattens git.Status,
// this flattens git.Branch so no frontend has to import the git package.
type BranchInfo struct {
	Name       string    `json:"name"`
	RemoteOnly bool      `json:"remoteOnly,omitempty"`
	Current    bool      `json:"current,omitempty"`
	Upstream   string    `json:"upstream,omitempty"` // display form, e.g. "origin/foo"
	Ahead      int       `json:"ahead"`
	Behind     int       `json:"behind"`
	Gone       bool      `json:"gone,omitempty"`
	Worktree   string    `json:"worktree,omitempty"` // set when checked out elsewhere
	Time       time.Time `json:"time"`
	Subject    string    `json:"subject,omitempty"`
	Hash       string    `json:"hash,omitempty"`
}

// Updatable reports whether this branch is KNOWN to be behind — that is, behind
// as of the last fetch. It is not the same question as "can this be pulled":
// UpdateBranch fetches first, so a branch reporting false here may still move.
func (b BranchInfo) Updatable() bool { return b.Behind > 0 && !b.Gone && !b.RemoteOnly }

// BranchList is one repository's branch listing.
//
// It is deliberately NOT part of RepoState. RepoState is persisted to
// state.json for every discovered repository and painted before any git runs; a
// sixty-entry branch listing is both large and stale the moment anyone commits,
// so it is fetched on demand and never written to disk.
type BranchList struct {
	Path     string       `json:"path"`
	Branches []BranchInfo `json:"branches"`
	LoadedAt time.Time    `json:"loadedAt"`
	Err      string       `json:"err,omitempty"`

	// Stamps taken from the repository when the listing was read, so a consumer
	// can tell the listing has been overtaken by a background fetch or pull
	// without diffing refs.
	FetchedAt time.Time `json:"-"`
	PulledAt  time.Time `json:"-"`
}

// WorktreeInfo is one linked checkout as the UI sees it. The primary checkout
// is deliberately excluded: its status is already represented by RepoState.
type WorktreeInfo struct {
	Path      string `json:"path"`
	Head      string `json:"head,omitempty"`
	Branch    string `json:"branch,omitempty"`
	Detached  bool   `json:"detached,omitempty"`
	Bare      bool   `json:"bare,omitempty"`
	Locked    string `json:"locked,omitempty"`
	Prunable  string `json:"prunable,omitempty"`
	Upstream  string `json:"upstream,omitempty"`
	Ahead     int    `json:"ahead"`
	Behind    int    `json:"behind"`
	Staged    int    `json:"staged"`
	Modified  int    `json:"modified"`
	Deleted   int    `json:"deleted"`
	Untracked int    `json:"untracked"`
	Conflicts int    `json:"conflicts"`
	Operation string `json:"operation,omitempty"`
	Dirty     bool   `json:"dirty"`
	Err       string `json:"err,omitempty"`
}

// WorktreeList is loaded on demand and never persisted. Linked checkouts can
// be numerous and git status can be expensive, so repository discovery remains
// canonical and this detail is only gathered for an expanded repository.
type WorktreeList struct {
	Path      string         `json:"path"`
	Worktrees []WorktreeInfo `json:"worktrees"`
	LoadedAt  time.Time      `json:"loadedAt"`
	Err       string         `json:"err,omitempty"`
}

// GitCommand is one completed Git subprocess recorded by the shared execution
// layer. The alias keeps UI/backend consumers on the monitor boundary.
type GitCommand = git.CommandTrace

func (m *Manager) GitCommands() []GitCommand { return git.CommandTraces() }

func (m *Manager) ClearGitCommands() { git.ClearCommandTraces() }

func (m *Manager) SetOnGitCommand(fn func()) { git.SetCommandTraceNotify(fn) }

func (m *Manager) ConfigPath(path string) (string, error) {
	return git.ConfigPath(m.ctx, path)
}

// Branches reads a repository's branches. It blocks on git, so callers should
// run it off the UI thread. Nothing is cached here — the Manager stays stateless
// for branches, exactly like Details(), so there is only one place (the UI) that
// has to decide when a listing has gone stale.
func (m *Manager) Branches(path string) BranchList {
	bl := BranchList{Path: path, LoadedAt: time.Now()}
	m.mu.RLock()
	if r, ok := m.repos[path]; ok {
		bl.FetchedAt, bl.PulledAt = r.LastFetch, r.LastPull
	}
	m.mu.RUnlock()

	bs, err := git.Branches(m.ctx, path, defaultRemote)
	if err != nil {
		bl.Err = err.Error()
		return bl
	}
	bl.Branches = make([]BranchInfo, 0, len(bs))
	for _, b := range bs {
		bl.Branches = append(bl.Branches, BranchInfo{
			Name:       b.Name,
			RemoteOnly: b.Kind == git.BranchRemoteOnly,
			Current:    b.Current,
			Upstream:   b.UpstreamName,
			Ahead:      b.Ahead,
			Behind:     b.Behind,
			Gone:       b.Gone,
			Worktree:   b.WorktreePath,
			Time:       b.CommitTime,
			Subject:    b.Subject,
			Hash:       shortHash(b.ObjectID),
		})
	}
	return bl
}

// Worktrees reads the linked checkouts belonging to a repository and gathers
// each checkout's local status. It blocks on git, so callers must run it off the
// UI thread. The per-repository lock prevents these reads from racing a fetch,
// pull, push, or worktree creation for the same repository.
func (m *Manager) Worktrees(path string) WorktreeList {
	wl := WorktreeList{Path: path, LoadedAt: time.Now()}
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	worktrees, err := git.Worktrees(m.ctx, path)
	if err != nil {
		wl.Err = err.Error()
		return wl
	}

	primary := cleanAbsolutePath(path)
	for _, worktree := range worktrees {
		if cleanAbsolutePath(worktree.Path) == primary {
			continue
		}
		wl.Worktrees = append(wl.Worktrees, WorktreeInfo{
			Path:     worktree.Path,
			Head:     shortHash(worktree.Head),
			Branch:   worktree.Branch,
			Detached: worktree.Detached,
			Bare:     worktree.Bare,
			Locked:   worktree.Locked,
			Prunable: worktree.Prunable,
		})
	}
	if len(wl.Worktrees) == 0 {
		return wl
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := min(refreshWorkers, len(wl.Worktrees))
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				item := &wl.Worktrees[i]
				if item.Bare || item.Prunable != "" {
					continue
				}
				status, statusErr := git.GetStatus(m.ctx, item.Path)
				if statusErr != nil {
					item.Err = statusErr.Error()
					continue
				}
				item.Branch = status.Branch
				item.Detached = status.Detached
				item.Upstream = status.Upstream
				item.Ahead = status.Ahead
				item.Behind = status.Behind
				item.Staged = status.Staged
				item.Modified = status.Modified
				item.Deleted = status.Deleted
				item.Untracked = status.Untracked
				item.Conflicts = status.Conflicts
				item.Operation = status.Operation
				item.Dirty = status.Dirty()
			}
		}()
	}
	for i := range wl.Worktrees {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return wl
}

func cleanAbsolutePath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, resolveErr := filepath.EvalSymlinks(abs); resolveErr == nil {
		abs = resolved
	}
	return filepath.Clean(abs)
}

// ErrBranchUpToDate reports that a branch had nothing to do. It is not a
// failure — the UI says so and moves on — but the caller has to be able to tell
// it apart from work that actually happened.
var ErrBranchUpToDate = errors.New("branch is already up to date")

// BranchAction is what bringing a branch into line with its remote requires.
// It is the same question an IDE answers when it decides between "update" and
// "push": one control, and the branch's actual state picks the verb.
type BranchAction int

const (
	BranchNothing BranchAction = iota // level with its upstream
	BranchPull                        // behind only — fast-forward
	BranchPush                        // ahead only — publish
	BranchMerge                       // both — integrate the upstream, then push
	BranchBlocked                     // both, and checked out nowhere to merge in
)

// PlanBranch decides what a branch needs, from a listing the UI already has.
//
// The same function decides which control to draw and, after a fetch, what to
// run — so the control never promises something different from what happens.
func PlanBranch(b BranchInfo) BranchAction {
	switch {
	case b.RemoteOnly, b.Gone, b.Upstream == "":
		return BranchNothing
	case b.Behind > 0 && b.Ahead > 0:
		if b.Worktree == "" {
			return BranchBlocked
		}
		return BranchMerge
	case b.Behind > 0:
		return BranchPull
	case b.Ahead > 0:
		return BranchPush
	default:
		return BranchNothing
	}
}

// SyncBranch brings one branch into line with its remote, whatever that takes.
//
// It fetches first, then looks at where the branch actually stands and does the
// matching thing: fast-forward when it is only behind, push when it is only
// ahead, merge the upstream in when it is both. "diverged — cannot fast-forward"
// used to be the answer to all three — an error message for a state the app
// refused to handle, on a branch that in two of those cases needed no
// fast-forwarding at all.
//
// It reports which of them it did, so the status bar can say so.
func (m *Manager) SyncBranch(path, branch string) (BranchAction, error) {
	if _, err := m.FetchRepo(path); err != nil {
		return BranchNothing, err
	}
	// Re-read after the fetch: the counts the user clicked on are older than the
	// commits that just arrived.
	b, err := git.LookupBranch(m.ctx, path, branch)
	if err != nil {
		return BranchNothing, err
	}
	if !b.HasUpstream() || b.Gone {
		// FastForwardBranch reports the real reason (no upstream / upstream gone)
		// in the same classified form as every other refusal.
		return BranchNothing, git.FastForwardBranch(m.ctx, path, b)
	}

	switch {
	case b.Behind > 0 && b.Ahead > 0:
		if b.WorktreePath == "" {
			return BranchBlocked, &git.BranchError{
				Kind: git.BranchErrNotCheckedOut, Branch: branch}
		}
		return BranchMerge, m.mergeBranch(path, b)
	case b.Behind > 0:
		return BranchPull, m.PullBranch(path, branch)
	case b.Ahead > 0:
		return BranchPush, m.PushBranch(path, branch)
	default:
		return BranchNothing, ErrBranchUpToDate
	}
}

// mergeBranch integrates a diverged branch's upstream, in whichever checkout
// holds it. A merge needs a working tree — that is why a diverged branch checked
// out nowhere cannot be settled from here at all.
func (m *Manager) mergeBranch(path string, b git.Branch) error {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	name := filepath.Base(path)
	op := m.act.begin(ActivityPulling, 1)
	op.start(name + " · " + b.Name)
	defer op.end()
	defer op.finish(name + " · " + b.Name)

	if err := git.MergeUpstream(m.ctx, b.WorktreePath, b); err != nil {
		m.act.post(ActivityEvent{Repo: name + " · " + b.Name, Err: err.Error(), At: time.Now()})
		return err
	}
	if b.Current {
		m.update(path, func(r *RepoState) { r.LastPull = time.Now() })
	}
	m.refreshOne(path)
	m.notify()
	return nil
}

// PushBranch publishes one branch to its upstream. It is never forced: a push
// the remote will not fast-forward is reported, not worked around.
func (m *Manager) PushBranch(path, branch string) error {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	b, err := git.LookupBranch(m.ctx, path, branch)
	if err != nil {
		return err
	}

	name := filepath.Base(path)
	op := m.act.begin(ActivityPushing, 1)
	op.start(name + " · " + branch)
	defer op.end()
	defer op.finish(name + " · " + branch)

	if err := git.PushBranch(m.ctx, path, b); err != nil {
		m.act.post(ActivityEvent{Repo: name + " · " + branch, Err: err.Error(), At: time.Now()})
		return err
	}
	m.refreshOne(path)
	m.act.post(ActivityEvent{Repo: name + " · " + branch, At: time.Now()})
	m.notify()
	return nil
}

// PullBranch fast-forwards one branch of a repository.
//
// It takes the same per-repository lock as Pull — keyed by path, not by branch —
// so a branch fetch can never run alongside a repo-wide pull in the same working
// copy, which is what makes ref-lock contention a rarity rather than routine.
func (m *Manager) PullBranch(path, branch string) error {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	// Re-resolve rather than trusting what the caller passed back: the listing
	// the user clicked may be minutes old, and the upstream may not be the one
	// its name suggests.
	b, err := git.LookupBranch(m.ctx, path, branch)
	if err != nil {
		return err
	}

	name := filepath.Base(path)
	op := m.act.begin(ActivityPulling, 1)
	op.start(name + " · " + branch)
	defer op.end()
	defer op.finish(name + " · " + branch)

	if err := git.FastForwardBranch(m.ctx, path, b); err != nil {
		m.act.post(ActivityEvent{Repo: name + " · " + branch, Err: err.Error(), At: time.Now()})
		return err
	}

	now := time.Now()
	if b.Current {
		// HEAD moved, so the repository's own status is stale.
		m.update(path, func(r *RepoState) { r.LastPull = now })
		m.refreshOne(path)
	}
	m.act.post(ActivityEvent{Repo: name + " · " + branch, At: now})
	m.notify()
	return nil
}

// TrackBranch creates a local branch tracking the remote one of the same name.
// Nothing is checked out; the working tree does not change.
func (m *Manager) TrackBranch(path, branch string) error {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	if err := git.CreateLocalBranch(m.ctx, path, branch,
		"refs/remotes/"+defaultRemote+"/"+branch); err != nil {
		return err
	}
	m.notify()
	return nil
}

// EnsureBranchWorktree returns the checkout that already holds a branch, or
// creates one beside the repository under .worktrees/<repo>/<branch>. The
// repository lock serialises this with fetch/pull/push so refs cannot change
// halfway through creation.
func (m *Manager) EnsureBranchWorktree(path string, info BranchInfo) (string, error) {
	lockValue, _ := m.pullLocks.LoadOrStore(path, &sync.Mutex{})
	lock := lockValue.(*sync.Mutex)
	lock.Lock()
	defer lock.Unlock()

	b, err := git.LookupBranch(m.ctx, path, info.Name)
	if err == nil {
		if b.WorktreePath != "" {
			return b.WorktreePath, nil
		}
	} else {
		var branchErr *git.BranchError
		if !info.RemoteOnly || !errors.As(err, &branchErr) || branchErr.Kind != git.BranchErrMissing {
			return "", err
		}
		remoteRef := "refs/remotes/" + defaultRemote + "/" + info.Name
		if info.Upstream != "" {
			remoteRef = "refs/remotes/" + info.Upstream
		}
		b = git.Branch{
			Name: info.Name, Kind: git.BranchRemoteOnly, RemoteRef: remoteRef,
		}
	}

	target, err := branchWorktreePath(path, info.Name)
	if err != nil {
		return "", err
	}
	if err := git.CreateBranchWorktree(m.ctx, path, target, b); err != nil {
		return "", err
	}
	m.notify()
	return target, nil
}

func branchWorktreePath(repoPath, branch string) (string, error) {
	if strings.TrimSpace(branch) == "" {
		return "", fmt.Errorf("branch name is empty")
	}
	repo, err := filepath.Abs(filepath.Clean(repoPath))
	if err != nil {
		return "", err
	}
	root := filepath.Join(filepath.Dir(repo), ".worktrees", filepath.Base(repo))
	target := filepath.Join(root, filepath.FromSlash(branch))
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("branch %q does not map to a safe worktree path", branch)
	}
	return target, nil
}

func shortHash(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// BranchErrorMessage renders a refused branch operation as one short sentence.
// The classification is done in the git layer; this is the seam that keeps
// frontends from having to import it or match git's wording themselves.
func BranchErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var be *git.BranchError
	if errors.As(err, &be) {
		return be.Message()
	}
	return err.Error()
}
