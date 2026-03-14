package simh

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/hashicorp/hcl/v2/hcldec"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
)

const BuilderId = "simh.simh"

// Builder implements packer.Builder for SIMH.
type Builder struct {
	config Config
	runner multistep.Runner
}

var _ packer.Builder = &Builder{}

// ConfigSpec returns the HCL2 object spec for the builder configuration.
func (b *Builder) ConfigSpec() hcldec.ObjectSpec {
	return b.config.FlatMapstructure().HCL2Spec()
}

// Prepare processes the template and validates configuration.
func (b *Builder) Prepare(raws ...interface{}) ([]string, []string, error) {
	generatedVars, warnings, errs := b.config.Prepare(raws...)
	if errs != nil {
		return nil, warnings, errs
	}
	return generatedVars, warnings, nil
}

// Run executes the SIMH build.
func (b *Builder) Run(ctx context.Context, ui packer.Ui, hook packer.Hook) (packer.Artifact, error) {
	// Resolve the SIMH binary.
	simhPath, err := exec.LookPath(b.config.SimhBinary)
	if err != nil {
		return nil, fmt.Errorf("error finding SIMH binary '%s': %s", b.config.SimhBinary, err)
	}
	log.Printf("[INFO] SIMH binary resolved to: %s", simhPath)

	// Create the driver.
	driver := &SimhDriver{
		SimhPath: simhPath,
	}

	// Verify the driver.
	if err := driver.Verify(); err != nil {
		return nil, fmt.Errorf("SIMH driver verification failed: %s", err)
	}

	// Log version (informational only).
	ver, _ := driver.Version()
	log.Printf("[INFO] SIMH version: %s", ver)

	// Set up the state bag.
	state := new(multistep.BasicStateBag)
	state.Put("config", &b.config)
	state.Put("debug", b.config.PackerDebug)
	state.Put("driver", driver)
	state.Put("hook", hook)
	state.Put("ui", ui)

	// Build the step sequence.
	steps := []multistep.Step{}

	// Step 1 — Download ISO (conditional).
	if !b.config.isoSkipped {
		steps = append(steps, &commonsteps.StepDownload{
			Checksum:    b.config.ISOChecksum,
			Description: "ISO",
			Extension:   b.config.TargetExtension,
			ResultKey:   "iso_path",
			TargetPath:  b.config.TargetPath,
			Url:         b.config.ISOUrls,
		})
	}

	// Step 2 — Prepare output directory.
	steps = append(steps, new(stepPrepareOutputDir))

	// Step 3 — HTTP server.
	steps = append(steps, commonsteps.HTTPServerFromHTTPConfig(&b.config.HTTPConfig))

	// Step 4 — Discover HTTP IP.
	steps = append(steps, new(stepDiscoverHTTPIP))

	// Step 5 — SSH key generation (conditional).
	if b.config.Type == "ssh" {
		steps = append(steps, &communicator.StepSSHKeyGen{
			CommConf:            &b.config.Config,
			SSHTemporaryKeyPair: b.config.SSHTemporaryKeyPair,
		})
	}

	// Step 6 — Forward port (conditional).
	if b.config.Type != "none" && !b.config.SkipPortForward {
		steps = append(steps, new(stepForwardPort))
	}

	// Step 7 — Configure console.
	steps = append(steps, new(stepConfigureConsole))

	// Step 8 — Create command file.
	steps = append(steps, new(stepCreateCommandFile))

	// Step 9 — Run SIMH.
	steps = append(steps, new(stepRun))

	// Step 10 — Boot command (conditional).
	if len(b.config.BootSteps) > 0 {
		steps = append(steps, new(stepBootCommand))
	}

	// Step 11 — Connect communicator.
	steps = append(steps, &communicator.StepConnect{
		Config:    &b.config.Config,
		Host:      commHost(b.config.CommHost),
		SSHConfig: b.config.SSHConfigFunc(),
		SSHPort:   commPort(&b.config.Config),
	})

	// Step 12 — Provision.
	steps = append(steps, new(commonsteps.StepProvision))

	// Step 13 — Cleanup temp keys (conditional).
	if b.config.Type == "ssh" {
		steps = append(steps, new(commonsteps.StepCleanupTempKeys))
	}

	// Step 14 — Shutdown.
	steps = append(steps, new(stepShutdown))

	// Run the steps.
	b.runner = commonsteps.NewRunner(steps, b.config.PackerConfig, ui)
	b.runner.Run(ctx, state)

	// Check for errors.
	if rawErr, ok := state.GetOk("error"); ok {
		return nil, rawErr.(error)
	}

	// Check if cancelled or halted.
	if _, ok := state.GetOk(multistep.StateCancelled); ok {
		return nil, fmt.Errorf("build was cancelled")
	}
	if _, ok := state.GetOk(multistep.StateHalted); ok {
		return nil, fmt.Errorf("build was halted")
	}

	// Collect artifact files.
	artifactFiles, err := collectArtifactFiles(b.config.OutputDirectory, b.config.VMName)
	if err != nil {
		return nil, fmt.Errorf("error collecting artifact files: %s", err)
	}

	// Build artifact state.
	artifactState := map[string]interface{}{
		"simulator": b.config.SimhBinary,
	}

	if generatedData, ok := state.GetOk("generated_data"); ok {
		artifactState["generated_data"] = generatedData
	}

	if diskPaths, ok := state.GetOk("disk_paths"); ok {
		artifactState["diskPaths"] = diskPaths
	}

	artifact := &Artifact{
		dir:   b.config.OutputDirectory,
		f:     artifactFiles,
		state: artifactState,
	}

	return artifact, nil
}

// collectArtifactFiles walks the output directory and returns all regular
// files, excluding the command file and console log.
func collectArtifactFiles(outputDir, vmName string) ([]string, error) {
	excludeNames := map[string]bool{
		vmName + ".simh":        true,
		vmName + ".console.log": true,
	}

	var files []string
	err := filepath.Walk(outputDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// Get relative name from output dir for exclusion check.
		rel, err := filepath.Rel(outputDir, path)
		if err != nil {
			return err
		}
		if excludeNames[rel] {
			return nil
		}
		// Exclude files with path separators in the excluded name check
		// (only top-level files are excluded).
		if !strings.Contains(rel, string(filepath.Separator)) && excludeNames[rel] {
			return nil
		}
		files = append(files, path)
		return nil
	})
	return files, err
}
