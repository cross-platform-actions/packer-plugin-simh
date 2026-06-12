//go:build !windows

package simh

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// setProcessGroup arranges for cmd to be started in its own process group
// (the group leader's PGID equals its PID). Must be called before cmd.Start().
//
// This serves two purposes:
//   - It detaches SIMH from the terminal's foreground process group, so a
//     Ctrl-C in the user's terminal no longer delivers SIGINT straight to
//     SIMH. SIMH traps SIGINT as "stop simulation, read next command" and
//     would survive Packer's death otherwise.
//   - It gives Stop() and the watchdog a single handle — the negative PID —
//     with which to signal SIMH and every child it may have spawned.
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killProcessGroup sends sig to every process in the process group led by
// pid. Kill with a negative PID targets the whole group, which is why
// setProcessGroup must have made the process a group leader first.
func killProcessGroup(pid int, sig syscall.Signal) error {
	return syscall.Kill(-pid, sig)
}

// stopProcess terminates SIMH on Unix by escalating signals against the whole
// process group (not just the SIMH PID), so any children SIMH may have spawned
// die with it. It blocks until the process is reaped or the escalation is
// exhausted, because SIMH holds listening sockets (telnet console, NAT port
// redirects) that are only released when it exits.
func (d *SimhDriver) stopProcess(cmd *exec.Cmd, endCh <-chan struct{}) error {
	pid := cmd.Process.Pid
	stages := []struct {
		sig  syscall.Signal
		name string
		wait time.Duration
	}{
		// SIMH traps SIGINT: the simulation stops ("Simulation stopped")
		// and the SCP continues with the next line of the command file,
		// which always ends in QUIT (see step_create_command_file.go).
		// This is the graceful exit path — console logs are flushed and
		// attached devices are detached cleanly.
		{syscall.SIGINT, "SIGINT", 3 * time.Second},
		{syscall.SIGTERM, "SIGTERM", 5 * time.Second},
		{syscall.SIGKILL, "SIGKILL", 5 * time.Second},
	}

	for _, stage := range stages {
		log.Printf("[INFO] Sending %s to SIMH process group %d", stage.name, pid)
		if err := killProcessGroup(pid, stage.sig); err != nil {
			// ESRCH just means the group is already gone; the wait on
			// endCh below will confirm that almost immediately.
			log.Printf("[WARN] Error sending %s to SIMH process group %d: %s", stage.name, pid, err)
		}

		select {
		case <-endCh:
			d.stopWatchdog()
			return nil
		case <-time.After(stage.wait):
			log.Printf("[WARN] SIMH process group %d still alive %s after %s", pid, stage.wait, stage.name)
		}
	}

	// Even SIGKILL did not get the process reaped within the cap. Do not
	// pretend it is gone — its ports may still be held, and leave the
	// watchdog armed since SIMH is confirmed still alive.
	return fmt.Errorf("SIMH process group %d did not exit within 5s of SIGKILL", pid)
}

// startWatchdog spawns a tiny shell process that SIGKILLs SIMH's process
// group when this plugin process dies for any reason — including SIGKILL,
// which no in-process handler (signal handler, defer, Cleanup) can ever
// intercept. macOS has no equivalent of Linux's PR_SET_PDEATHSIG, so the
// watchdog relies on a kernel guarantee instead: when a process dies, the
// kernel closes all of its file descriptors. The plugin holds the write end
// of a pipe for the driver's lifetime; the watchdog's stdin is the read end.
// As long as the plugin lives, `cat` blocks on stdin. The moment the plugin
// dies, cat sees EOF and the watchdog kills SIMH's group.
//
// Deliberately NOT placed in SIMH's process group (no setProcessGroup),
// otherwise the watchdog would SIGKILL itself mid-cleanup and, worse, die
// together with SIMH whenever Stop() signals the group.
//
// Errors are non-fatal: a build without the watchdog still works, it just
// loses the protection against abnormal plugin death.
//
// The caller (Simh) holds d.lock, so the driver fields are set directly.
func (d *SimhDriver) startWatchdog(pid int) {
	r, w, err := os.Pipe()
	if err != nil {
		log.Printf("[WARN] Failed to create watchdog pipe, SIMH will not be cleaned up if the plugin dies abnormally: %s", err)
		return
	}

	// The kill target is a negative PID (the process group). Do not use a
	// "--" end-of-options marker: bash's kill builtin accepts it, but
	// dash's (Linux's /bin/sh) rejects it with "Illegal number: -", which
	// silently defeats the watchdog. Without "--" both shells treat the
	// leading-dash argument as the group PID.
	watchdog := exec.Command("/bin/sh", "-c",
		fmt.Sprintf("cat >/dev/null; kill -KILL -%d 2>/dev/null", pid))
	watchdog.Stdin = r

	if err := watchdog.Start(); err != nil {
		log.Printf("[WARN] Failed to start watchdog process, SIMH will not be cleaned up if the plugin dies abnormally: %s", err)
		_ = r.Close()
		_ = w.Close()
		return
	}

	// The watchdog now owns its own duplicate of the read end; close ours
	// so that w is the only descriptor keeping the pipe open.
	_ = r.Close()

	log.Printf("[INFO] Started SIMH watchdog (pid %d) for process group %d", watchdog.Process.Pid, pid)
	d.watchdogCmd = watchdog
	d.watchdogW = w
}

// stopWatchdog dismisses the watchdog once SIMH is confirmed dead. Closing
// the write end of the pipe makes cat see EOF; the trailing kill of the
// already-dead group fails harmlessly (its stderr is discarded) and the
// watchdog exits. Reaping happens in a goroutine so Stop() never blocks on
// it. Safe to call multiple times and when no watchdog was ever started.
func (d *SimhDriver) stopWatchdog() {
	d.lock.Lock()
	w := d.watchdogW
	cmd := d.watchdogCmd
	d.watchdogW = nil
	d.watchdogCmd = nil
	d.lock.Unlock()

	if w != nil {
		_ = w.Close()
	}
	if cmd != nil {
		go func() { _ = cmd.Wait() }()
	}
}
