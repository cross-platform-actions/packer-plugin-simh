// Package simh implements a Packer builder plugin for the SIMH historical
// computer emulator.
package simh

//go:generate packer-sdc mapstructure-to-hcl2 -type Config,DiskAttachment,NetworkDevice

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/common"
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep/commonsteps"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/config"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

// DiskAttachment represents a disk or tape device attachment in the SIMH
// command file.
type DiskAttachment struct {
	// SIMH device-unit identifier, e.g. "RQ0", "RL0", "TQ0".
	Device string `mapstructure:"device" required:"true"`
	// Path to the image file. May contain runtime template variables.
	// Relative paths are resolved from output_directory.
	Path string `mapstructure:"path" required:"true"`
	// Additional SIMH ATTACH options, appended after the path.
	Options string `mapstructure:"options"`
	// SET <device> <value> commands to emit before the ATTACH.
	SetCommands []string `mapstructure:"set_commands"`
}

// NetworkDevice represents a structured network device configuration.
type NetworkDevice struct {
	// SIMH network device name, e.g. "XQ".
	Device string `mapstructure:"device" required:"true"`
	// MAC address. Generates SET <device> MAC=<mac>.
	MAC string `mapstructure:"mac"`
	// SIMH attach argument. May contain runtime template variables.
	AttachType string `mapstructure:"attach_type" required:"true"`
	// Additional SET <device> <value> commands.
	SetCommands []string `mapstructure:"set_commands"`
}

// Config is the configuration structure for the SIMH builder.
type Config struct {
	common.PackerConfig    `mapstructure:",squash"`
	commonsteps.ISOConfig  `mapstructure:",squash"`
	commonsteps.HTTPConfig `mapstructure:",squash"`
	communicator.Config    `mapstructure:",squash"`

	// Name or path of the SIMH simulator binary.
	SimhBinary string `mapstructure:"simh_binary" required:"true"`

	// Directory for all build outputs.
	OutputDirectory string `mapstructure:"output_directory"`
	// Base name for generated files.
	VMName string `mapstructure:"vm_name"`

	// Passed as SET CPU <cpu_type> if non-empty.
	CPUType string `mapstructure:"cpu_type"`
	// Passed as SET CPU <memory> if non-empty.
	Memory string `mapstructure:"memory"`
	// Additional SET CPU operands.
	CPUOptions []string `mapstructure:"cpu_options"`

	// List of disk/tape device attachments.
	DiskAttachments []DiskAttachment `mapstructure:"disk_attachments"`

	// Optional structured network device configuration.
	NetworkDevice *NetworkDevice `mapstructure:"network_device"`
	// Raw SIMH SCP commands for network setup.
	NetworkCommands []string `mapstructure:"network_commands"`

	// Ordered list of [expect, send, description] tuples.
	BootSteps [][]string `mapstructure:"boot_steps"`
	// Delay after SIMH starts before connecting to the console.
	BootWait time.Duration `mapstructure:"boot_wait"`
	// Minimum interval between keystrokes sent to the console.
	BootKeyInterval time.Duration `mapstructure:"boot_key_interval"`
	// Maximum time to wait for each individual expect pattern.
	BootStepTimeout time.Duration `mapstructure:"boot_step_timeout"`

	// Path to an external SIMH command file.
	CommandFile string `mapstructure:"command_file"`

	// Minimum port for the Telnet console.
	ConsolePortMin int `mapstructure:"console_port_min"`
	// Maximum port for the Telnet console.
	ConsolePortMax int `mapstructure:"console_port_max"`
	// Bind address for the Telnet console.
	ConsoleBindAddress string `mapstructure:"console_bind_address"`
	// Capture console output to a log file.
	ConsoleLog *bool `mapstructure:"console_log"`

	// Override host address for the SSH communicator.
	CommHost string `mapstructure:"comm_host"`
	// Minimum host port for SSH forwarding.
	HostPortMin int `mapstructure:"host_port_min"`
	// Maximum host port for SSH forwarding.
	HostPortMax int `mapstructure:"host_port_max"`
	// Skip host port selection entirely.
	SkipPortForward bool `mapstructure:"skip_port_forward"`

	// Override the IP for Packer's HTTP server reachable from guest.
	HTTPIP string `mapstructure:"http_ip"`

	// Additional raw SCP commands inserted before disk attachments.
	SimhArgs []string `mapstructure:"simh_args"`

	// Additional raw SCP commands inserted after the initial BOOT CPU and
	// before the auto-QUIT. Each command runs after the previous BOOT
	// returns control to SCP (i.e. on a HALT instruction when SIMHALT is in
	// effect). Useful for re-booting after the guest's first halt/reboot
	// without exiting the simulator.
	PostBootCommands []string `mapstructure:"post_boot_commands"`

	// Maximum time to wait for the SIMH process to exit.
	ShutdownTimeout time.Duration `mapstructure:"shutdown_timeout"`

	// Internal: whether ISO download was skipped.
	isoSkipped bool

	ctx interpolate.Context
}

// templateData holds runtime template variables available during Pass 2
// interpolation.
type templateData struct {
	HTTPIP       string
	HTTPPort     int
	Name         string
	OutputDir    string
	ISOPath      string
	SSHPublicKey string
	HostPort     int
	GuestPort    int
}

// Prepare processes the configuration and validates it.
func (c *Config) Prepare(raws ...interface{}) ([]string, []string, error) {
	err := config.Decode(c, &config.DecodeOpts{
		PluginType:         BuilderId,
		Interpolate:        true,
		InterpolateContext: &c.ctx,
		InterpolateFilter: &interpolate.RenderFilter{
			Exclude: []string{
				"boot_steps",
				"disk_attachments",
				"network_device",
				"network_commands",
				"simh_args",
				"post_boot_commands",
			},
		},
	}, raws...)
	if err != nil {
		return nil, nil, err
	}

	var errs *packer.MultiError
	var warnings []string

	// Validate communicator type.
	if c.Type == "winrm" {
		errs = packer.MultiErrorAppend(errs,
			errors.New("WinRM communicator is not supported by the SIMH builder"))
	}

	// Prepare ISO config (optional).
	if c.RawSingleISOUrl != "" || len(c.ISOUrls) > 0 {
		isoWarnings, isoErrs := c.ISOConfig.Prepare(&c.ctx)
		warnings = append(warnings, isoWarnings...)
		errs = packer.MultiErrorAppend(errs, isoErrs...)
	} else {
		c.isoSkipped = true
	}

	// Prepare HTTP config.
	httpErrs := c.HTTPConfig.Prepare(&c.ctx)
	errs = packer.MultiErrorAppend(errs, httpErrs...)

	// Prepare communicator config.
	commErrs := c.Config.Prepare(&c.ctx)
	errs = packer.MultiErrorAppend(errs, commErrs...)

	// Validate simh_binary is set.
	if c.SimhBinary == "" {
		errs = packer.MultiErrorAppend(errs,
			errors.New("simh_binary is required"))
	}

	// Set defaults.
	if c.OutputDirectory == "" {
		c.OutputDirectory = fmt.Sprintf("output-%s", c.PackerBuildName)
	}
	if c.VMName == "" {
		c.VMName = fmt.Sprintf("packer-%s", c.PackerBuildName)
	}

	// Convert output_directory to absolute path.
	absOutputDir, err := filepath.Abs(c.OutputDirectory)
	if err != nil {
		errs = packer.MultiErrorAppend(errs,
			fmt.Errorf("error resolving output_directory: %s", err))
	} else {
		c.OutputDirectory = absOutputDir
	}

	// Set console defaults.
	if c.ConsolePortMin == 0 {
		c.ConsolePortMin = 4000
	}
	if c.ConsolePortMax == 0 {
		c.ConsolePortMax = 4100
	}
	if c.ConsoleBindAddress == "" {
		c.ConsoleBindAddress = "127.0.0.1"
	}
	if c.ConsoleLog == nil {
		defaultTrue := true
		c.ConsoleLog = &defaultTrue
	}

	// Set communicator host/port defaults.
	if c.HostPortMin == 0 {
		c.HostPortMin = 2222
	}
	if c.HostPortMax == 0 {
		c.HostPortMax = 4444
	}

	// Set boot defaults.
	if c.BootWait == 0 {
		c.BootWait = 10 * time.Second
	}
	if c.BootStepTimeout == 0 {
		c.BootStepTimeout = 5 * time.Minute
	}

	// Set shutdown default.
	if c.ShutdownTimeout == 0 {
		c.ShutdownTimeout = 5 * time.Minute
	}

	// Validate disk attachments.
	for i, da := range c.DiskAttachments {
		if da.Device == "" {
			errs = packer.MultiErrorAppend(errs,
				fmt.Errorf("disk_attachments[%d]: device is required", i))
		}
		if da.Path == "" {
			errs = packer.MultiErrorAppend(errs,
				fmt.Errorf("disk_attachments[%d]: path is required", i))
		}
	}

	// Validate network_device.
	if c.NetworkDevice != nil {
		if c.NetworkDevice.Device == "" {
			errs = packer.MultiErrorAppend(errs,
				errors.New("network_device: device is required"))
		}
		if c.NetworkDevice.AttachType == "" {
			errs = packer.MultiErrorAppend(errs,
				errors.New("network_device: attach_type is required"))
		}
	}

	// Validate port ranges.
	if c.ConsolePortMin > c.ConsolePortMax {
		errs = packer.MultiErrorAppend(errs,
			errors.New("console_port_min must be less than or equal to console_port_max"))
	}
	if c.HostPortMin > c.HostPortMax {
		errs = packer.MultiErrorAppend(errs,
			errors.New("host_port_min must be less than or equal to host_port_max"))
	}

	// Check if output_directory exists.
	if _, err := os.Stat(c.OutputDirectory); err == nil {
		if !c.PackerForce {
			errs = packer.MultiErrorAppend(errs,
				fmt.Errorf("output_directory '%s' already exists. Use -force to overwrite", c.OutputDirectory))
		}
	}

	// Validate boot_steps.
	for i, step := range c.BootSteps {
		if len(step) != 3 {
			errs = packer.MultiErrorAppend(errs,
				fmt.Errorf("boot_steps[%d]: must be a 3-element array [expect, send, description], got %d elements", i, len(step)))
		}
	}

	// Validate command_file.
	if c.CommandFile != "" {
		if _, err := os.Stat(c.CommandFile); err != nil {
			errs = packer.MultiErrorAppend(errs,
				fmt.Errorf("command_file '%s' does not exist or is not readable: %s", c.CommandFile, err))
		}
		// post_boot_commands are appended after the external command_file's
		// contents; if that file issues QUIT (or otherwise exits) first, they
		// will never run.
		if len(c.PostBootCommands) > 0 {
			warnings = append(warnings,
				"post_boot_commands are appended after your command_file contents; if that file issues QUIT (or otherwise exits) before them, they will not run.")
		}
	}

	generatedVars := []string{"ID", "Host", "Port", "SSHPublicKey"}

	if errs != nil && len(errs.Errors) > 0 {
		return generatedVars, warnings, errs
	}

	return generatedVars, warnings, nil
}
