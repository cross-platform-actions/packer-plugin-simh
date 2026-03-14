package simh

import (
	"context"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestStepForwardPort_FindsPort(t *testing.T) {
	config := &Config{
		HostPortMin: 14200,
		HostPortMax: 14300,
	}
	config.Type = "ssh"

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})

	step := &stepForwardPort{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}

	port, ok := state.GetOk("commHostPort")
	if !ok {
		t.Fatal("expected commHostPort in state")
	}

	p := port.(int)
	if p < 14200 || p > 14300 {
		t.Errorf("expected port in range [14200, 14300], got %d", p)
	}
}

func TestStepForwardPort_SkipNone(t *testing.T) {
	config := &Config{}
	config.Type = "none"

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})

	step := &stepForwardPort{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}

	if _, ok := state.GetOk("commHostPort"); ok {
		t.Error("expected no commHostPort for none communicator")
	}
}

func TestStepForwardPort_SkipPortForward(t *testing.T) {
	config := &Config{
		SkipPortForward: true,
	}
	config.Type = "ssh"

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})

	step := &stepForwardPort{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}

	if _, ok := state.GetOk("commHostPort"); ok {
		t.Error("expected no commHostPort when skip_port_forward is true")
	}
}
