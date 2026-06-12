package simh

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type stepShutdown struct{}

func (s *stepShutdown) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	driver := state.Get("driver").(Driver)
	ui := state.Get("ui").(packer.Ui)

	ui.Say(fmt.Sprintf("Waiting for SIMH to exit (timeout: %s)...", config.ShutdownTimeout))

	cancelCh := make(chan struct{})
	go func() {
		select {
		case <-time.After(config.ShutdownTimeout):
			close(cancelCh)
		case <-ctx.Done():
			close(cancelCh)
		}
	}()

	if driver.WaitForShutdown(cancelCh) {
		log.Printf("[INFO] SIMH exited cleanly")
		ui.Say("SIMH process exited successfully.")
		return multistep.ActionContinue
	}

	// Check if it was a user cancellation vs timeout.
	select {
	case <-ctx.Done():
		ui.Say("Build cancelled. Stopping SIMH...")
		if err := driver.Stop(); err != nil {
			log.Printf("[WARN] Error stopping SIMH after cancellation: %s", err)
			ui.Error(fmt.Sprintf("Error stopping SIMH: %s", err))
		}
		return multistep.ActionHalt
	default:
		// Timeout.
		ui.Error("SIMH did not exit within timeout; process killed")
		if err := driver.Stop(); err != nil {
			log.Printf("[WARN] Error stopping SIMH after shutdown timeout: %s", err)
			ui.Error(fmt.Sprintf("Error stopping SIMH: %s", err))
		}
		err := fmt.Errorf("SIMH did not exit within %s timeout", config.ShutdownTimeout)
		state.Put("error", err)
		return multistep.ActionHalt
	}
}

func (s *stepShutdown) Cleanup(state multistep.StateBag) {}
