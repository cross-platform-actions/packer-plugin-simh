package simh

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

func testStateBag(config *Config) *multistep.BasicStateBag {
	state := new(multistep.BasicStateBag)
	state.Put("config", config)
	state.Put("ui", &packer.MockUi{})
	state.Put("driver", &MockDriver{WaitResult: true})
	state.Put("hook", &packer.MockHook{})
	return state
}

func TestStepPrepareOutputDir_Creates(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output-test")

	config := &Config{
		OutputDirectory: dir,
	}
	state := testStateBag(config)

	step := &stepPrepareOutputDir{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected output directory to be created")
	}
}

func TestStepPrepareOutputDir_Force(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output-test")
	_ = os.MkdirAll(dir, 0755)
	_ = os.WriteFile(filepath.Join(dir, "old.txt"), []byte("old"), 0644)

	config := &Config{
		OutputDirectory: dir,
	}
	config.PackerForce = true

	state := testStateBag(config)

	step := &stepPrepareOutputDir{}
	action := step.Run(context.Background(), state)

	if action != multistep.ActionContinue {
		t.Fatal("expected ActionContinue")
	}

	// Old file should be gone.
	if _, err := os.Stat(filepath.Join(dir, "old.txt")); !os.IsNotExist(err) {
		t.Fatal("expected old file to be removed")
	}
}

func TestStepPrepareOutputDir_CleanupOnError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output-test")

	config := &Config{
		OutputDirectory: dir,
	}
	state := testStateBag(config)

	step := &stepPrepareOutputDir{}
	step.Run(context.Background(), state)

	// Simulate error.
	state.Put(multistep.StateHalted, true)
	step.Cleanup(state)

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatal("expected output directory to be removed on error")
	}
}

func TestStepPrepareOutputDir_NoCleanupOnDebug(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output-test")

	config := &Config{
		OutputDirectory: dir,
	}
	config.PackerDebug = true

	state := testStateBag(config)

	step := &stepPrepareOutputDir{}
	step.Run(context.Background(), state)

	state.Put(multistep.StateHalted, true)
	step.Cleanup(state)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected output directory to be preserved in debug mode")
	}
}

func TestStepPrepareOutputDir_NoCleanupOnSuccess(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "output-test")

	config := &Config{
		OutputDirectory: dir,
	}
	state := testStateBag(config)

	step := &stepPrepareOutputDir{}
	step.Run(context.Background(), state)

	// No StateHalted or StateCancelled — simulate success.
	step.Cleanup(state)

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected output directory to be preserved on success")
	}
}
