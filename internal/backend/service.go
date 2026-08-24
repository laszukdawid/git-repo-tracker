// Package backend exposes the repo-tracking core independently of any UI.
// Frontends such as Fyne, a GNOME Shell extension bridge, or a headless CLI should
// depend on this package instead of wiring config and monitor directly.
package backend

import (
	"fmt"

	"github.com/laszukdawid/git-repo-tracker/internal/config"
	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

// Service owns the shared configuration and monitor daemon used by frontends.
type Service struct {
	cfg *config.Config
	mgr *monitor.Manager
}

// LoadDefaultConfig loads the configured default config path, honoring
// GIT_REPO_TRACKER_CONFIG.
func LoadDefaultConfig() (*config.Config, error) {
	path, err := config.DefaultPath()
	if err != nil {
		return nil, fmt.Errorf("resolve config path: %w", err)
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return cfg, nil
}

// New constructs a backend service around an already-loaded config.
func New(cfg *config.Config, onChange func(), logf func(string, ...any)) *Service {
	return &Service{cfg: cfg, mgr: monitor.New(cfg, onChange, logf)}
}

// Config returns the live config object backing this service.
func (s *Service) Config() *config.Config { return s.cfg }

// Start begins background discovery/status loops.
func (s *Service) Start() { s.mgr.Start() }

// Stop terminates background loops and persists final state.
func (s *Service) Stop() { s.mgr.Stop() }

// Refresh requests a non-blocking discovery + fetch pass.
func (s *Service) Refresh() { s.mgr.Refresh() }

// SetOnActivity installs the callback fired when background progress changes.
// Frontends that narrate what the daemon is doing use this instead of onChange,
// which is debounced for a much heavier full rebuild.
func (s *Service) SetOnActivity(fn func()) { s.mgr.SetOnActivity(fn) }

// Activity reports what the monitor is doing right now.
func (s *Service) Activity() monitor.Activity { return s.mgr.Activity() }

// DrainActivityEvents removes and returns completions queued since the last call.
func (s *Service) DrainActivityEvents() []monitor.ActivityEvent {
	return s.mgr.DrainActivityEvents()
}

// RefreshNow runs a blocking discovery + status pass. Use withFetch sparingly:
// it can hit the network for every auto-fetch root.
func (s *Service) RefreshNow(withFetch bool) { s.mgr.RefreshNow(withFetch) }

// Snapshot returns the current repository snapshot.
func (s *Service) Snapshot() []monitor.RepoState { return s.mgr.Snapshot() }

// Counts returns total and behind repo counts.
func (s *Service) Counts() (total, behind int) { return s.mgr.Counts() }

// Pull fast-forwards one repository.
func (s *Service) Pull(path string) error { return s.mgr.Pull(path) }

// SetKeepFresh opts a repository into or out of automatic fast-forward pulls.
func (s *Service) SetKeepFresh(path string, enabled bool) error {
	return s.mgr.SetKeepFresh(path, enabled)
}

// UpdateAll fetches every tracked remote, then fast-forwards every repository
// that is behind it. Manual updates ignore the background auto-fetch setting.
func (s *Service) UpdateAll() []monitor.UpdateResult { return s.mgr.UpdateAll() }

// Details returns expandable detail data for one repository.
func (s *Service) Details(path string) monitor.Details { return s.mgr.Details(path) }

// Branches lists a repository's local branches plus the remote-only ones. It
// blocks on git and is not cached, so callers should fetch it lazily.
func (s *Service) Branches(path string) monitor.BranchList { return s.mgr.Branches(path) }

// Worktrees lists linked checkouts and their local status. It blocks on git and
// is not cached, so callers should fetch it lazily.
func (s *Service) Worktrees(path string) monitor.WorktreeList { return s.mgr.Worktrees(path) }

// GitCommands returns the current process's in-memory Git command history.
func (s *Service) GitCommands() []monitor.GitCommand { return s.mgr.GitCommands() }

// ClearGitCommands removes the current process's Git command history.
func (s *Service) ClearGitCommands() { s.mgr.ClearGitCommands() }

// SetOnGitCommand installs a callback for live console updates.
func (s *Service) SetOnGitCommand(fn func()) { s.mgr.SetOnGitCommand(fn) }

// ConfigPath resolves the repository-local Git config file.
func (s *Service) ConfigPath(path string) (string, error) { return s.mgr.ConfigPath(path) }

// FetchRepo fetches one repository on request, regardless of autoFetch.
// It reports how many remote-tracking refs the fetch moved.
func (s *Service) FetchRepo(path string) (int, error) { return s.mgr.FetchRepo(path) }

// PullBranch fast-forwards one branch without touching the working tree.
func (s *Service) PullBranch(path, branch string) error { return s.mgr.PullBranch(path, branch) }

// SyncBranch fetches, then does whatever the branch actually needs: fast-forward,
// push, or merge its upstream in. It reports which.
func (s *Service) SyncBranch(path, branch string) (monitor.BranchAction, error) {
	return s.mgr.SyncBranch(path, branch)
}

// PushBranch publishes one branch to its upstream, never forced.
func (s *Service) PushBranch(path, branch string) error {
	return s.mgr.PushBranch(path, branch)
}

// TrackBranch creates a local branch tracking the remote one of the same name.
func (s *Service) TrackBranch(path, branch string) error { return s.mgr.TrackBranch(path, branch) }

// EnsureBranchWorktree reuses the checkout holding a branch or creates a linked
// worktree for it beside the repository.
func (s *Service) EnsureBranchWorktree(path string, branch monitor.BranchInfo) (string, error) {
	return s.mgr.EnsureBranchWorktree(path, branch)
}
