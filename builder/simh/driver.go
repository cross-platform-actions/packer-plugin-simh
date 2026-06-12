package simh

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Driver is the interface for interacting with the SIMH process.
type Driver interface {
	// Simh launches the SIMH binary with the given command file path.
	Simh(commandFile string) error

	// WaitForShutdown blocks until the SIMH process exits or the cancel
	// channel is closed. Returns true if the process exited on its own.
	WaitForShutdown(cancelCh <-chan struct{}) bool

	// Stop forcefully kills the SIMH process.
	Stop() error

	// Verify validates that the SIMH binary exists and is executable.
	Verify() error

	// Version returns the SIMH simulator version string.
	Version() (string, error)
}

// SimhDriver manages the SIMH process.
type SimhDriver struct {
	SimhPath string
	vmCmd    *exec.Cmd
	vmEndCh  <-chan struct{}
	exitCode int
	lock     sync.Mutex

	// Parent-death watchdog (Unix only, see startWatchdog in
	// driver_unix.go). watchdogW is the write end of a pipe that the
	// plugin holds open for the driver's lifetime; the watchdog process
	// reads the other end and SIGKILLs SIMH's process group when it sees
	// EOF, i.e. when this plugin process dies for any reason.
	watchdogCmd *exec.Cmd
	watchdogW   *os.File
}

// Simh launches the SIMH binary with the given command file path.
func (d *SimhDriver) Simh(commandFile string) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	cmd := exec.Command(d.SimhPath, commandFile)
	cmd.Dir = filepath.Dir(commandFile)

	// Run SIMH in its own process group (Unix; no-op on Windows). Without
	// this, SIMH shares the terminal's foreground process group with
	// Packer, so a user Ctrl-C delivers SIGINT directly to SIMH too. SIMH
	// traps SIGINT to mean "stop simulation and read the next command-file
	// line" rather than exit, so it would keep running while Packer aborts
	// — exactly the orphan we are trying to prevent. With its own group,
	// SIMH only receives signals this driver sends deliberately, which is
	// safe because Stop() signals the whole group itself and the watchdog
	// started below covers abnormal plugin death.
	setProcessGroup(cmd)

	// Pipe stdout and stderr through log readers.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("error getting stdout pipe: %s", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("error getting stderr pipe: %s", err)
	}

	log.Printf("[INFO] Starting SIMH: %s %s", d.SimhPath, commandFile)
	log.Printf("[INFO] Working directory: %s", cmd.Dir)

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("error starting SIMH: %s", err)
	}

	d.vmCmd = cmd

	// Start the parent-death watchdog (Unix only; no-op on Windows). If
	// the plugin process itself dies without ever running Stop() — e.g.
	// it is SIGKILLed, panics, or the SDK's abortStep skips step Cleanups
	// — nothing in-process can clean up SIMH. The watchdog handles that
	// case from outside the plugin process (see startWatchdog). A failure
	// to start it is logged but non-fatal: the build still works, it just
	// loses this last line of defense.
	d.startWatchdog(cmd.Process.Pid)

	// Forward stdout/stderr to log.
	go logReader("simh stdout", stdout)
	go logReader("simh stderr", stderr)

	// Wait for process exit in a goroutine.
	endCh := make(chan struct{})
	d.vmEndCh = endCh

	go func() {
		defer close(endCh)
		err := cmd.Wait()
		d.lock.Lock()
		defer d.lock.Unlock()

		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				d.exitCode = exitErr.ExitCode()
			} else {
				d.exitCode = -1
			}
		} else {
			d.exitCode = 0
		}
		log.Printf("[INFO] SIMH process exited with code %d", d.exitCode)
	}()

	// Wait up to 2 seconds for early exit.
	select {
	case <-endCh:
		d.lock.Lock()
		code := d.exitCode
		d.lock.Unlock()
		if code != 0 {
			return fmt.Errorf("SIMH exited with non-zero code %d within 2 seconds of starting", code)
		}
		// Exit code 0 within 2 seconds is success.
		return nil
	case <-time.After(2 * time.Second):
		// Process is still running — normal async operation.
		return nil
	}
}

// WaitForShutdown blocks until the SIMH process exits or the cancel channel
// is closed.
func (d *SimhDriver) WaitForShutdown(cancelCh <-chan struct{}) bool {
	d.lock.Lock()
	endCh := d.vmEndCh
	d.lock.Unlock()

	if endCh == nil {
		return true
	}
	select {
	case <-endCh:
		return true
	case <-cancelCh:
		return false
	}
}

// Done returns a channel that is closed when the SIMH process exits. It
// returns nil if SIMH was never started.
func (d *SimhDriver) Done() <-chan struct{} {
	d.lock.Lock()
	defer d.lock.Unlock()
	return d.vmEndCh
}

// Stop terminates the SIMH process, escalating from a graceful SIGINT to
// SIGKILL, and does not return until the process has actually been reaped.
// Blocking until the process is gone matters: SIMH holds listening sockets
// (the telnet console, NAT port redirects) that are only released when it
// exits, and callers may immediately reuse those ports or delete the output
// directory. Stop is idempotent — it returns nil without doing anything if
// SIMH was never started or has already exited.
func (d *SimhDriver) Stop() error {
	d.lock.Lock()
	cmd := d.vmCmd
	endCh := d.vmEndCh
	d.lock.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Check if process already exited.
	if endCh != nil {
		select {
		case <-endCh:
			// SIMH is gone, so the watchdog has nothing left to guard.
			d.stopWatchdog()
			return nil
		default:
		}
	}

	// Platform-specific termination: signal escalation against the process
	// group on Unix, a direct Kill on Windows (see driver_unix.go /
	// driver_windows.go).
	return d.stopProcess(cmd, endCh)
}

// Verify validates that the SIMH binary exists and is executable.
func (d *SimhDriver) Verify() error {
	// Currently a no-op — binary path is already validated by exec.LookPath.
	return nil
}

// Version returns the SIMH simulator version string.
func (d *SimhDriver) Version() (string, error) {
	cmd := exec.Command(d.SimhPath, "--help")
	output, _ := cmd.CombinedOutput()

	re := regexp.MustCompile(`(?i)(?:sim(?:h|ulator)\s+)?(?:version\s+)?(\d+\.\d+[\w.-]*)`)
	matches := re.FindSubmatch(output)
	if matches != nil {
		return string(matches[1]), nil
	}

	return "unknown", nil
}

// logReader reads from an io.Reader line by line and logs each line.
func logReader(name string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		log.Printf("[%s] %s", name, scanner.Text())
	}
}
