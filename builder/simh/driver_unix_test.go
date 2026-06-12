//go:build !windows

package simh

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestSimhDriver_Stop_NeverStarted(t *testing.T) {
	d := &SimhDriver{SimhPath: "/bin/sleep"}

	// Stop must be a safe no-op, repeatedly, when nothing was started.
	for i := 0; i < 2; i++ {
		if err := d.Stop(); err != nil {
			t.Fatalf("expected nil error from Stop() with no process, got %s", err)
		}
	}
}

func TestSimhDriver_Stop_Idempotent(t *testing.T) {
	// Use sleep as a stand-in for SIMH; "30" doubles as both the command
	// file argument and sleep's duration. Stop() must kill it long before
	// the 30 seconds elapse.
	d := &SimhDriver{SimhPath: "/bin/sleep"}
	if err := d.Simh("30"); err != nil {
		t.Fatalf("error starting process: %s", err)
	}

	if err := d.Stop(); err != nil {
		t.Fatalf("expected nil error from first Stop(), got %s", err)
	}

	// Stop() must not return until the process has been reaped.
	select {
	case <-d.vmEndCh:
	default:
		t.Fatal("expected vmEndCh to be closed after Stop() returns")
	}

	// A second Stop() must be a no-op.
	if err := d.Stop(); err != nil {
		t.Fatalf("expected nil error from second Stop(), got %s", err)
	}
}

func TestKillProcessGroup(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	setProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		t.Fatalf("error starting process: %s", err)
	}

	if err := killProcessGroup(cmd.Process.Pid, syscall.SIGKILL); err != nil {
		t.Fatalf("error killing process group: %s", err)
	}

	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()

	select {
	case err := <-done:
		// SIGKILL makes Wait return a "signal: killed" error.
		if err == nil {
			t.Error("expected non-nil error from Wait() after SIGKILL")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process group was not killed within 5 seconds")
	}
}

func TestWatchdog_KillsChildWhenPipeCloses(t *testing.T) {
	// A stand-in for SIMH: a long sleep in its own process group.
	victim := exec.Command("/bin/sleep", "30")
	setProcessGroup(victim)
	if err := victim.Start(); err != nil {
		t.Fatalf("error starting victim process: %s", err)
	}
	defer func() { _ = victim.Process.Kill() }()

	d := &SimhDriver{}
	d.startWatchdog(victim.Process.Pid)
	if d.watchdogW == nil || d.watchdogCmd == nil {
		t.Fatal("expected watchdog to be running")
	}

	// Simulate the plugin process dying: the kernel would close every
	// descriptor it holds, including the pipe's write end.
	_ = d.watchdogW.Close()

	done := make(chan error, 1)
	go func() { done <- victim.Wait() }()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected non-nil error from Wait() after watchdog SIGKILL")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not kill the victim within 5 seconds")
	}

	// Reap the watchdog itself; it exits once cat sees EOF.
	_ = d.watchdogCmd.Wait()
}
