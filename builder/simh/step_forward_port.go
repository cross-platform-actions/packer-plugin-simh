package simh

import (
	"context"
	"fmt"
	"net"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type stepForwardPort struct{}

func (s *stepForwardPort) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)

	if config.Type == "none" || config.SkipPortForward {
		return multistep.ActionContinue
	}

	for port := config.HostPortMin; port <= config.HostPortMax; port++ {
		l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err != nil {
			continue
		}
		_ = l.Close()

		ui.Say(fmt.Sprintf("Selected host port %d for communicator forwarding", port))
		state.Put("commHostPort", port)
		return multistep.ActionContinue
	}

	err := fmt.Errorf("no available port found in range [%d, %d]", config.HostPortMin, config.HostPortMax)
	state.Put("error", err)
	ui.Error(err.Error())
	return multistep.ActionHalt
}

func (s *stepForwardPort) Cleanup(state multistep.StateBag) {}
