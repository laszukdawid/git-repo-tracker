// Package config defines the persisted application configuration and the
// operations used to read and mutate it. The on-disk format is YAML so it can
// be hand-edited, and the same struct is written back when settings change
// through the UI. Config holds user intent only (which directories to scan and
// how); the discovered repositories and their status live in a separate cache.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// Click actions understood by the UI when a repo row is activated.
const (
	ActionOpenFolder = "open-folder"
	ActionTerminal   = "terminal"
	ActionEditor     = "editor"
	ActionCustom     = "custom"
)

// Defaults applied when the config omits a value or carries a nonsensical one.
const (
	defaultFetchMinutes = 30
	defaultLocalSeconds = 30
	defaultDepth        = 5
	defaultProjectsDir  = "~/projects"
)

// defaultIgnore lists directory names that are pruned during discovery. They
// never contain repos we care about but can hold thousands of files.
var defaultIgnore = []string{"node_modules", "vendor", ".Trash", ".cache", "Library"}

// Root is a directory tree scanned for git repositories.
type Root struct {
	Path      string `yaml:"path"`
	Depth     int    `yaml:"depth"`     // max sub-directory depth; 0 means unlimited
	AutoFetch bool   `yaml:"autoFetch"` // run `git fetch` for repos under this root
}

// Config is the root document. Operations are guarded by a mutex because the UI
// and the monitor can touch it from different goroutines.
type Config struct {
	Roots                []Root   `yaml:"roots"`
	Ignore               []string `yaml:"ignore"`
	FetchIntervalMinutes int      `yaml:"fetchIntervalMinutes"`
	LocalRefreshSeconds  int      `yaml:"localRefreshSeconds"`
	ClickAction          string   `yaml:"clickAction"`
	CustomCommand        string   `yaml:"customCommand"`
	LaunchAtLogin        bool     `yaml:"launchAtLogin"`

	path string
	mu   sync.Mutex
}

// DefaultPath resolves the config location, honouring an override env var and
// otherwise using the OS-conventional config dir (on macOS that is
// ~/Library/Application Support/git-repo-tracker/config.yaml).
func DefaultPath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("GIT_REPO_TRACKER_CONFIG")); p != "" {
		return p, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-repo-tracker", "config.yaml"), nil
}

// Load reads the config at path. When the file is absent a starter config is
// written (seeded with ~/projects if it exists) so a first launch shows
// something useful instead of an empty tray.
func Load(path string) (*Config, error) {
	c := &Config{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			c.seedDefaults()
			c.normalize()
			_ = c.save() // best effort; a read-only config dir is non-fatal
			return c, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, c); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	c.normalize()
	return c, nil
}

// seedDefaults populates a brand-new config with a sensible starting root so the
// app is usable before the user has touched the file.
func (c *Config) seedDefaults() {
	if dir := ExpandPath(defaultProjectsDir); dir != "" {
		if info, err := os.Stat(dir); err == nil && info.IsDir() {
			c.Roots = []Root{{Path: defaultProjectsDir, Depth: defaultDepth, AutoFetch: true}}
		}
	}
}

// normalize fills in defaults for omitted or invalid values so the rest of the
// app can rely on them without re-checking.
func (c *Config) normalize() {
	if c.FetchIntervalMinutes <= 0 {
		c.FetchIntervalMinutes = defaultFetchMinutes
	}
	if c.LocalRefreshSeconds <= 0 {
		c.LocalRefreshSeconds = defaultLocalSeconds
	}
	if c.Ignore == nil {
		c.Ignore = append([]string(nil), defaultIgnore...)
	}
	if strings.TrimSpace(c.ClickAction) == "" {
		c.ClickAction = ActionOpenFolder
	}
	for i := range c.Roots {
		if c.Roots[i].Depth < 0 {
			c.Roots[i].Depth = 0
		}
	}
}

// Path returns the file backing this config.
func (c *Config) Path() string { return c.path }

// RootList returns a copy of the configured roots so callers can range safely.
func (c *Config) RootList() []Root {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Root, len(c.Roots))
	copy(out, c.Roots)
	return out
}

// IgnoreDirs returns a copy of the ignore list.
func (c *Config) IgnoreDirs() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.Ignore))
	copy(out, c.Ignore)
	return out
}

// FetchInterval is how often to run `git fetch` for each repo.
func (c *Config) FetchInterval() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Duration(c.FetchIntervalMinutes) * time.Minute
}

// LocalRefresh is how often to recompute cheap local status.
func (c *Config) LocalRefresh() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.Duration(c.LocalRefreshSeconds) * time.Second
}

// Click returns the configured click action and its custom command.
func (c *Config) Click() (action, custom string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.ClickAction, c.CustomCommand
}

// LaunchAtLoginEnabled reports the persisted launch-at-login preference.
func (c *Config) LaunchAtLoginEnabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.LaunchAtLogin
}

// Save applies a batch of settings atomically: it persists in one write and, on
// failure, rolls the in-memory values back so they never diverge from disk.
// Non-positive intervals keep their current value.
func (c *Config) Save(roots []Root, fetchMinutes, localSeconds int, action, custom string) error {
	return c.update(func() {
		c.Roots = append([]Root(nil), roots...)
		if fetchMinutes > 0 {
			c.FetchIntervalMinutes = fetchMinutes
		}
		if localSeconds > 0 {
			c.LocalRefreshSeconds = localSeconds
		}
		c.ClickAction = action
		c.CustomCommand = custom
	})
}

// SetRoots replaces the root list and persists.
func (c *Config) SetRoots(roots []Root) error {
	return c.update(func() { c.Roots = append([]Root(nil), roots...) })
}

// SetClickAction updates the click behaviour and persists.
func (c *Config) SetClickAction(action, custom string) error {
	return c.update(func() { c.ClickAction = action; c.CustomCommand = custom })
}

// SetLaunchAtLogin updates the launch-at-login preference and persists it.
func (c *Config) SetLaunchAtLogin(v bool) error {
	return c.update(func() { c.LaunchAtLogin = v })
}

// update applies mutate under the lock and persists atomically. If the write
// fails the in-memory values are rolled back, so memory never diverges from disk.
func (c *Config) update(mutate func()) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	prev := c.snapshot()
	mutate()
	if err := c.writeLocked(); err != nil {
		c.restore(prev)
		return err
	}
	return nil
}

// persisted is a copy of the on-disk fields, used for rollback.
type persisted struct {
	roots                []Root
	ignore               []string
	fetchIntervalMinutes int
	localRefreshSeconds  int
	clickAction          string
	customCommand        string
	launchAtLogin        bool
}

func (c *Config) snapshot() persisted {
	return persisted{
		roots:                append([]Root(nil), c.Roots...),
		ignore:               append([]string(nil), c.Ignore...),
		fetchIntervalMinutes: c.FetchIntervalMinutes,
		localRefreshSeconds:  c.LocalRefreshSeconds,
		clickAction:          c.ClickAction,
		customCommand:        c.CustomCommand,
		launchAtLogin:        c.LaunchAtLogin,
	}
}

func (c *Config) restore(p persisted) {
	c.Roots, c.Ignore = p.roots, p.ignore
	c.FetchIntervalMinutes, c.LocalRefreshSeconds = p.fetchIntervalMinutes, p.localRefreshSeconds
	c.ClickAction, c.CustomCommand = p.clickAction, p.customCommand
	c.LaunchAtLogin = p.launchAtLogin
}

// Reload re-reads the backing file and replaces the in-memory values in place,
// so hand-edits take effect without restarting. The Config pointer is unchanged,
// which keeps it race-free for holders that read through the mutexed accessors.
func (c *Config) Reload() error {
	fresh, err := Load(c.path)
	if err != nil {
		return err
	}
	c.mu.Lock()
	c.Roots = fresh.Roots
	c.Ignore = fresh.Ignore
	c.FetchIntervalMinutes = fresh.FetchIntervalMinutes
	c.LocalRefreshSeconds = fresh.LocalRefreshSeconds
	c.ClickAction = fresh.ClickAction
	c.CustomCommand = fresh.CustomCommand
	c.LaunchAtLogin = fresh.LaunchAtLogin
	c.mu.Unlock()
	return nil
}

// save locks and persists; used during Load. Runtime mutations go through update.
func (c *Config) save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writeLocked()
}

// writeLocked marshals and atomically writes the config (unique temp file +
// rename) to avoid leaving a truncated config behind or clobbering a shared temp.
// The caller must hold c.mu.
func (c *Config) writeLocked() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".config-*.yaml.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, c.path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}

// ExpandPath resolves a leading ~ to the user's home directory and returns an
// absolute, cleaned path. An empty input yields an empty string.
func ExpandPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return filepath.Clean(p)
}
