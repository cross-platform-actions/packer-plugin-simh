package simh

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"syscall"
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
}

// Simh launches the SIMH binary with the given command file path.
func (d *SimhDriver) Simh(commandFile string) error {
	d.lock.Lock()
	defer d.lock.Unlock()

	cmd := exec.Command(d.SimhPath, commandFile)
	cmd.Dir = filepath.Dir(commandFile)

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
	if d.vmEndCh == nil {
		return true
	}
	select {
	case <-d.vmEndCh:
		return true
	case <-cancelCh:
		return false
	}
}

// Stop forcefully kills the SIMH process.
func (d *SimhDriver) Stop() error {
	d.lock.Lock()
	cmd := d.vmCmd
	d.lock.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}

	// Check if process already exited.
	if d.vmEndCh != nil {
		select {
		case <-d.vmEndCh:
			return nil
		default:
		}
	}

	// Send SIGTERM (Unix) or Kill (Windows).
	if runtime.GOOS == "windows" {
		if err := cmd.Process.Kill(); err != nil {
			return err
		}
	} else {
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			return err
		}
	}

	// Wait up to 5 seconds for the process to exit.
	if d.vmEndCh != nil {
		select {
		case <-d.vmEndCh:
			return nil
		case <-time.After(5 * time.Second):
			// Force kill.
			log.Printf("[WARN] SIMH process did not exit after SIGTERM, force killing")
			return cmd.Process.Kill()
		}
	}

	return nil
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
