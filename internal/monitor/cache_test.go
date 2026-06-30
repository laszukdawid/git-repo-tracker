package monitor

import (
	"path/filepath"
	"sync"
	"testing"
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
	if err := saveCache([]RepoState{{Path: "/x", Name: "x", Behind: 3}}); err != nil {
		t.Fatal(err)
	}
	got := loadCache()
	r, ok := got["/x"]
	if !ok || r.Name != "x" || r.Behind != 3 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}
