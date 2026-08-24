package ui

import (
	"sync"
	"testing"

	"github.com/laszukdawid/git-repo-tracker/internal/monitor"
)

func TestBranchIndexBasics(t *testing.T) {
	x := newBranchIndex()
	if x.get("/a") != nil {
		t.Error("an unread repository should have no entry")
	}

	// claim reserves a repository so two passes cannot read it at once.
	if !x.claim("/a") {
		t.Fatal("the first claim should succeed")
	}
	if x.claim("/a") {
		t.Error("a repository already being read should not be claimed twice")
	}

	x.set("/a", []string{"main", "release/24.4"})
	if !x.matches("/a", "release") {
		t.Error("an indexed branch should match")
	}
	if x.matches("/a", "nope") {
		t.Error("a term matching nothing should not match")
	}
	// Once set, it can be claimed again — that is how a re-index works.
	if !x.claim("/a") {
		t.Error("a repository should be claimable again after being read")
	}

	// A repository with no branches is still indexed, and must not look unread.
	x.set("/empty", []string{})
	if x.get("/empty") == nil {
		t.Error("a branchless repository should still count as indexed")
	}

	x.invalidate("/a")
	if x.get("/a") != nil {
		t.Error("invalidate should drop the entry")
	}

	x.set("/live", []string{"main"})
	x.set("/gone", []string{"main"})
	x.prune(map[string]bool{"/live": true})
	if x.get("/gone") != nil {
		t.Error("a vanished repository should be pruned from the index")
	}
	if x.get("/live") == nil {
		t.Error("a live repository was pruned")
	}
}

// The index is written from background workers and read on the main thread, so
// it has to hold up under -race.
func TestBranchIndexConcurrentUse(t *testing.T) {
	x := newBranchIndex()
	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			path := string(rune('a' + i%8))
			if x.claim(path) {
				x.set(path, []string{"main", "feature"})
			}
			_ = x.matches(path, "main")
			_ = x.get(path)
		}(i)
	}
	wg.Wait()
}

func TestBranchIndexErrorRemainsRetryable(t *testing.T) {
	x := newBranchIndex()
	if !x.claim("/repo") {
		t.Fatal("initial index claim failed")
	}
	x.record("/repo", monitor.BranchList{Err: "refs temporarily locked"})
	if got := x.get("/repo"); got != nil {
		t.Fatalf("failed branch read was cached as %v, want unread", got)
	}
	if !x.claim("/repo") {
		t.Fatal("failed branch read could not be retried")
	}
}
