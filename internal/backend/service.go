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

// RefreshNow runs a blocking discovery + status pass. Use withFetch sparingly:
// it can hit the network for every auto-fetch root.
func (s *Service) RefreshNow(withFetch bool) { s.mgr.RefreshNow(withFetch) }

// Snapshot returns the current repository snapshot.
func (s *Service) Snapshot() []monitor.RepoState { return s.mgr.Snapshot() }

// Counts returns total and behind repo counts.
func (s *Service) Counts() (total, behind int) { return s.mgr.Counts() }

// Pull fast-forwards one repository.
func (s *Service) Pull(path string) error { return s.mgr.Pull(path) }

// UpdateAll fetches every tracked remote, then fast-forwards every repository
// that is behind it. Manual updates ignore the background auto-fetch setting.
func (s *Service) UpdateAll() []monitor.UpdateResult { return s.mgr.UpdateAll() }

// Details returns expandable detail data for one repository.
func (s *Service) Details(path string) monitor.Details { return s.mgr.Details(path) }
