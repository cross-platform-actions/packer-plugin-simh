package simh

import (
	"context"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func TestStepShutdown_Success(t *testing.T) {
	driver := &MockDriver{WaitResult: true}
	config := &Config{
		ShutdownTimeout: 5 * time.Second,
	}

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("driver", driver)
	state.Put("ui", &packer.MockUi{})

	step := &stepShutdown{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}
}

func TestStepShutdown_Timeout(t *testing.T) {
	driver := &MockDriver{WaitResult: false}
	config := &Config{
		ShutdownTimeout: 100 * time.Millisecond,
	}

	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("driver", driver)
	state.Put("ui", &packer.MockUi{})

	step := &stepShutdown{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionHalt {
		t.Fatal("expected ActionHalt on timeout")
	}
	if _, ok := state.GetOk("error"); !ok {
		t.Error("expected error in state on timeout")
	}
	if !driver.StopCalled {
		t.Error("expected Stop() to be called on timeout")
	}
}
