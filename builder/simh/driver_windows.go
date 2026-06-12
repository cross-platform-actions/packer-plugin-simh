//go:build windows

package simh

import (
	"fmt"
	"os/exec"
	"time"
)

// setProcessGroup is a no-op on Windows. There are no Unix-style process
// groups; stopProcess kills the SIMH process directly via cmd.Process.Kill().
func setProcessGroup(cmd *exec.Cmd) {}

// stopProcess terminates SIMH on Windows. Windows has no Unix-style process
// groups or signals, so kill the process directly and wait for it to be
// reaped before returning (its listening sockets are only released then).
func (d *SimhDriver) stopProcess(cmd *exec.Cmd, endCh <-chan struct{}) error {
	if err := cmd.Process.Kill(); err != nil {
		return err
	}
	if endCh != nil {
		select {
		case <-endCh:
		case <-time.After(5 * time.Second):
			return fmt.Errorf("SIMH process did not exit within 5s of being killed")
		}
	}
	return nil
}

// startWatchdog is a no-op on Windows. The pipe-based parent-death watchdog
// relies on /bin/sh, cat and Unix process groups.
func (d *SimhDriver) startWatchdog(pid int) {}

// stopWatchdog is a no-op on Windows; no watchdog is ever started.
func (d *SimhDriver) stopWatchdog() {}
