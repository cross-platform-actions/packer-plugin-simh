package simh

import (
	"context"
	"fmt"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestStepRun_Success(t *testing.T) {
	driver := &MockDriver{WaitResult: true}
	state := new(multistep.BasicStateBag)
	state.Put("driver", driver)
	state.Put("ui", &packer.MockUi{})
	state.Put("command_file_path", "/tmp/test.simh")

	step := &stepRun{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}
	if !driver.SimhCalled {
		t.Error("expected Simh() to be called")
	}
	if driver.SimhFile != "/tmp/test.simh" {
		t.Errorf("expected command file /tmp/test.simh, got %s", driver.SimhFile)
	}
}

func TestStepRun_Error(t *testing.T) {
	driver := &MockDriver{
		SimhErr:    fmt.Errorf("launch failed"),
		WaitResult: true,
	}
	state := new(multistep.BasicStateBag)
	state.Put("driver", driver)
	state.Put("ui", &packer.MockUi{})
	state.Put("command_file_path", "/tmp/test.simh")

	step := &stepRun{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionHalt {
		t.Fatal("expected ActionHalt")
	}

	if _, ok := state.GetOk("error"); !ok {
		t.Error("expected error in state")
	}
}

func TestStepRun_Cleanup(t *testing.T) {
	driver := &MockDriver{WaitResult: true}
	state := new(multistep.BasicStateBag)
	state.Put("driver", driver)
	state.Put("ui", &packer.MockUi{})

	step := &stepRun{}
	step.Cleanup(state)

	if !driver.StopCalled {
		t.Error("expected Stop() to be called during cleanup")
	}
}
