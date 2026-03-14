package simh

import (
	"context"
	"strings"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

type stepDiscoverHTTPIP struct{}

func (s *stepDiscoverHTTPIP) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)

	var httpIP string

	if config.HTTPIP != "" {
		httpIP = config.HTTPIP
	} else if config.NetworkDevice != nil &&
		strings.HasPrefix(strings.ToLower(config.NetworkDevice.AttachType), "nat:") {
		httpIP = "10.0.2.2"
	} else {
		httpIP = "127.0.0.1"
	}

	ui.Say("HTTP IP for guest: " + httpIP)
	state.Put("http_ip", httpIP)
	return multistep.ActionContinue
}

func (s *stepDiscoverHTTPIP) Cleanup(state multistep.StateBag) {}
