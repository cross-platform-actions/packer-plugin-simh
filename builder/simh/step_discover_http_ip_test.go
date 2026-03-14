package simh

import (
	"context"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestStepDiscoverHTTPIP_Override(t *testing.T) {
	config := &Config{
		HTTPIP: "192.168.1.100",
	}
	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})

	step := &stepDiscoverHTTPIP{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}
	if state.Get("http_ip") != "192.168.1.100" {
		t.Errorf("expected 192.168.1.100, got %s", state.Get("http_ip"))
	}
}

func TestStepDiscoverHTTPIP_NAT(t *testing.T) {
	config := &Config{
		NetworkDevice: &NetworkDevice{
			Device:     "XQ",
			AttachType: "nat:tcp=2222:10.0.2.15:22",
		},
	}
	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})

	step := &stepDiscoverHTTPIP{}
	step.Run(context.Background(), state)

	if state.Get("http_ip") != "10.0.2.2" {
		t.Errorf("expected 10.0.2.2, got %s", state.Get("http_ip"))
	}
}

func TestStepDiscoverHTTPIP_Fallback(t *testing.T) {
	config := &Config{}
	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})

	step := &stepDiscoverHTTPIP{}
	step.Run(context.Background(), state)

	if state.Get("http_ip") != "127.0.0.1" {
		t.Errorf("expected 127.0.0.1, got %s", state.Get("http_ip"))
	}
}
