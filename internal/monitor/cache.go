package monitor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const cacheVersion = 1

// cacheDoc is the on-disk representation of the registry. Persisting last-known
// state lets the tray paint instantly on launch, before any git runs.
type cacheDoc struct {
	Version int         `json:"version"`
	Repos   []RepoState `json:"repos"`
}

// cachePath returns the state cache location: GIT_REPO_TRACKER_CACHE if set,
// otherwise under the OS cache dir (e.g. ~/Library/Caches/git-repo-tracker/
// state.json on macOS).
func cachePath() (string, error) {
	if p := strings.TrimSpace(os.Getenv("GIT_REPO_TRACKER_CACHE")); p != "" {
		return p, nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "git-repo-tracker", "state.json"), nil
}

// loadCache reads the persisted state. Any error (missing file, version skew,
// corrupt JSON) yields an empty registry rather than failing — the cache is
// disposable and will be rebuilt by the next refresh.
func loadCache() map[string]*RepoState {
	repos := map[string]*RepoState{}
	path, err := cachePath()
	if err != nil {
		return repos
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return repos
	}
	var doc cacheDoc
	if err := json.Unmarshal(data, &doc); err != nil || doc.Version != cacheVersion {
		return repos
	}
	for i := range doc.Repos {
		r := doc.Repos[i]
		if r.Path != "" {
			repos[r.Path] = &r
		}
	}
	return repos
}

// saveCache writes the registry atomically. It uses a unique temp file per call
// (os.CreateTemp) so concurrent saves — the local and remote refresh loops can
// both persist — never clobber a shared temp; each rename is atomic, so the final
// file is always one complete document.
func saveCache(repos []RepoState) error {
	path, err := cachePath()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cacheDoc{Version: cacheVersion, Repos: repos})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "state-*.json.tmp")
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
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return err
	}
	return nil
}
