package simh

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type stepPrepareOutputDir struct{}

func (s *stepPrepareOutputDir) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)

	dir := config.OutputDirectory

	if config.PackerForce {
		if _, err := os.Stat(dir); err == nil {
			ui.Say(fmt.Sprintf("Removing existing output directory: %s", dir))
			if err := os.RemoveAll(dir); err != nil {
				err := fmt.Errorf("error removing output directory: %s", err)
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
		}
	}

	ui.Say(fmt.Sprintf("Creating output directory: %s", dir))
	if err := os.MkdirAll(dir, 0755); err != nil {
		err := fmt.Errorf("error creating output directory: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepPrepareOutputDir) Cleanup(state multistep.StateBag) {
	config := state.Get("config").(*Config)

	_, halted := state.GetOk(multistep.StateHalted)
	_, cancelled := state.GetOk(multistep.StateCancelled)
	failed := halted || cancelled

	// Honor PACKER_SIMH_KEEP_OUTPUT=1 in addition to -debug so failures can
	// be inspected even when running non-interactively.
	keepForInspection := config.PackerDebug || os.Getenv("PACKER_SIMH_KEEP_OUTPUT") == "1"

	if failed && keepForInspection {
		log.Printf("[INFO] Leaving output directory for inspection: %s", config.OutputDirectory)
	} else if failed {
		log.Printf("[INFO] Removing output directory: %s", config.OutputDirectory)
		_ = os.RemoveAll(config.OutputDirectory)
	}
}
