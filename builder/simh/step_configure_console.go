package simh

import (
	"context"
	"fmt"
	"net"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type stepConfigureConsole struct{}

func (s *stepConfigureConsole) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)

	for port := config.ConsolePortMin; port <= config.ConsolePortMax; port++ {
		addr := fmt.Sprintf("%s:%d", config.ConsoleBindAddress, port)
		l, err := net.Listen("tcp", addr)
		if err != nil {
			continue
		}
		_ = l.Close()

		ui.Say(fmt.Sprintf("SIMH console will be available at telnet://%s:%d", config.ConsoleBindAddress, port))
		state.Put("console_port", port)
		return multistep.ActionContinue
	}

	err := fmt.Errorf("no available console port found in range [%d, %d]",
		config.ConsolePortMin, config.ConsolePortMax)
	state.Put("error", err)
	ui.Error(err.Error())
	return multistep.ActionHalt
}

func (s *stepConfigureConsole) Cleanup(state multistep.StateBag) {}
