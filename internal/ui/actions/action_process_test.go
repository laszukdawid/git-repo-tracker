package actions

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestStartAndReapWaitsForChild(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=TestActionHelperProcess")
	cmd.Env = append(os.Environ(), "GRT_ACTION_HELPER=1")
	done := make(chan error, 1)
	if err := startAndReap(cmd, func(err error) { done <- err }); err != nil {
		t.Fatalf("startAndReap: %v", err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("wait: %v", err)
		}
		if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
			t.Fatal("child process was not reaped")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for child process to be reaped")
	}
}

func TestActionHelperProcess(t *testing.T) {
	if os.Getenv("GRT_ACTION_HELPER") != "1" {
		return
	}
	os.Exit(0)
}
