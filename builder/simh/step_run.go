package simh

import (
	"context"
	"fmt"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type stepRun struct{}

func (s *stepRun) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	driver := state.Get("driver").(Driver)
	ui := state.Get("ui").(packer.Ui)
	cmdFilePath := state.Get("command_file_path").(string)

	ui.Say("Starting SIMH...")
	if err := driver.Simh(cmdFilePath); err != nil {
		err := fmt.Errorf("error starting SIMH: %s", err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	return multistep.ActionContinue
}

func (s *stepRun) Cleanup(state multistep.StateBag) {
	driver := state.Get("driver").(Driver)
	if err := driver.Stop(); err != nil {
		ui := state.Get("ui").(packer.Ui)
		ui.Error(fmt.Sprintf("Error stopping SIMH: %s", err))
	}
}
