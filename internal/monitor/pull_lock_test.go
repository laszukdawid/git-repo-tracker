package monitor

import (
	"runtime"
	"testing"
	"time"
)

func TestPullLockSerializesAndReleasesEntries(t *testing.T) {
	m := &Manager{pullLocks: make(map[string]*pullLock)}
	releaseFirst := m.lockPull("/work/repo")

	acquired := make(chan func(), 1)
	go func() {
		acquired <- m.lockPull("/work/repo")
	}()

	deadline := time.Now().Add(time.Second)
	for {
		m.pullLocksMu.Lock()
		refs := m.pullLocks["/work/repo"].refs
		m.pullLocksMu.Unlock()
		if refs == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second pull did not register as a waiter")
		}
		runtime.Gosched()
	}

	select {
	case release := <-acquired:
		release()
		t.Fatal("second pull acquired the lock before the first released it")
	default:
	}

	releaseFirst()
	select {
	case releaseSecond := <-acquired:
		releaseSecond()
	case <-time.After(time.Second):
		t.Fatal("second pull did not acquire the released lock")
	}

	m.pullLocksMu.Lock()
	remaining := len(m.pullLocks)
	m.pullLocksMu.Unlock()
	if remaining != 0 {
		t.Fatalf("pull lock entries after final release = %d, want 0", remaining)
	}
}
