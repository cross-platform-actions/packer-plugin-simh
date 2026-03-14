package simh

import (
	"context"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestStepConfigureConsole_FindsPort(t *testing.T) {
	config := &Config{
		ConsolePortMin:     14000,
		ConsolePortMax:     14100,
		ConsoleBindAddress: "127.0.0.1",
	}
	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})

	step := &stepConfigureConsole{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}

	port, ok := state.GetOk("console_port")
	if !ok {
		t.Fatal("expected console_port in state")
	}

	p := port.(int)
	if p < 14000 || p > 14100 {
		t.Errorf("expected port in range [14000, 14100], got %d", p)
	}
}
