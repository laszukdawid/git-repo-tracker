package monitor

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestConcurrentSaveCache ensures many simultaneous writes (as the local and
// remote refresh loops can produce) never corrupt the cache. Run with -race.
func TestConcurrentSaveCache(t *testing.T) {
	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))
	repos := []RepoState{{Path: "/a", Name: "a"}, {Path: "/b", Name: "b"}}

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := saveCache(repos); err != nil {
				t.Errorf("saveCache: %v", err)
			}
		}()
	}
	wg.Wait()

	got := loadCache()
	if len(got) != len(repos) {
		t.Fatalf("loaded %d repos, want %d (cache may be corrupt)", len(got), len(repos))
	}
}

func TestCacheRoundTrip(t *testing.T) {
	t.Setenv("GIT_REPO_TRACKER_CACHE", filepath.Join(t.TempDir(), "state.json"))
	pulled := time.Date(2026, 8, 21, 14, 20, 0, 0, time.UTC)
	auto := time.Date(2026, 8, 22, 9, 5, 0, 0, time.UTC)
	if err := saveCache([]RepoState{{
		Path: "/x", Name: "x", Behind: 3, LastPull: pulled, LastAutoPull: auto,
	}}); err != nil {
		t.Fatal(err)
	}
	got := loadCache()
	r, ok := got["/x"]
	if !ok || r.Name != "x" || r.Behind != 3 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !r.LastPull.Equal(pulled) || !r.LastAutoPull.Equal(auto) {
		t.Errorf("pull stamps lost: LastPull=%v LastAutoPull=%v", r.LastPull, r.LastAutoPull)
	}
}

// A cache written before LastPull existed must still load, with zero stamps
// meaning "never pulled". This is the property that lets the version stay at 1
// when fields are added — see the comment on cacheVersion.
func TestLoadCacheAcceptsDocumentWithoutPullStamps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	t.Setenv("GIT_REPO_TRACKER_CACHE", path)
	old := `{"version":1,"repos":[{"path":"/x","name":"x","behind":2,` +
		`"lastLocal":"2026-08-20T10:00:00Z","lastFetch":"2026-08-20T10:00:00Z"}]}`
	if err := os.WriteFile(path, []byte(old), 0o600); err != nil {
		t.Fatal(err)
	}

	got := loadCache()
	r, ok := got["/x"]
	if !ok {
		t.Fatalf("pre-LastPull document was discarded: %+v", got)
	}
	if r.Behind != 2 {
		t.Errorf("Behind = %d, want 2", r.Behind)
	}
	if !r.LastPull.IsZero() || !r.LastAutoPull.IsZero() {
		t.Errorf("missing stamps should be zero, got %v / %v", r.LastPull, r.LastAutoPull)
	}
}
