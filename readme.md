<p align="center">
  <img src="logo.svg" alt="packer-plugin-simh logo" width="400">
</p>

# Packer Plugin SIMH

A [HashiCorp Packer](https://www.packer.io) builder plugin for the
[SIMH](https://opensimh.org) historical computer emulator. Build automated
machine images for classic computers — VAX, PDP-11, Altair, IBM 7094, and
dozens more.

## Overview

Unlike QEMU-based builders that communicate with the VM through VNC, this
plugin drives SIMH through **command files** for device configuration and a
**telnet connection** to the simulated console for interactive boot sequences.

Key features:

- **Command file generation** — automatically builds SIMH configuration from
  structured HCL fields (CPU, memory, disks, network)
- **Console interaction** — uses expect/send `boot_steps` to drive the
  simulated console over telnet, with full Packer boot command syntax support
  (`<enter>`, `<wait5>`, `<leftCtrl>`, etc.)
- **Two-pass template interpolation** — runtime variables like `{{ .ISOPath }}`
  and `{{ .HostPort }}` are resolved at execution time
- **SSH communicator support** — connect to the guest OS for provisioning via
  SSH (or use `communicator = "none"` for non-networked builds)
- **Optional ISO download** — use `iso_url` to download install media, or
  provide pre-existing disk images directly

## Installation

### From GitHub Releases

```bash
packer plugins install github.com/jacob-carlborg/simh
```

### Manual Installation

Download the appropriate binary from the
[releases page](https://github.com/jacob-carlborg/packer-plugin-simh/releases)
and place it in the Packer plugins directory:

```bash
~/.packer.d/plugins/packer-plugin-simh
```

## Usage

### Minimal Example (no network, no ISO)

```hcl
source "simh" "pdp11-test" {
  simh_binary = "pdp11"

  output_directory = "output-pdp11"
  vm_name          = "pdp11-test"

  memory = "256K"

  disk_attachments {
    device       = "RL0"
    path         = "/path/to/existing/rt11.dsk"
    set_commands = ["RL02"]
  }

  communicator     = "none"
  shutdown_timeout = "2m"
}

build {
  sources = ["source.simh.pdp11-test"]
}
```

### Full Example (VAX with ISO, SSH, boot steps)

```hcl
source "simh" "vax-vms" {
  simh_binary = "vax"

  iso_url      = "https://example.com/vms-install.iso"
  iso_checksum = "sha256:abcdef1234567890..."

  output_directory = "output-vax-vms"
  vm_name          = "vax-vms"

  cpu_type    = "MicroVAX"
  memory      = "128M"
  cpu_options = ["CONHALT"]

  disk_attachments {
    device       = "RQ0"
    path         = "{{ .OutputDir }}/system.dsk"
    set_commands = ["RD54"]
  }

  disk_attachments {
    device  = "RQ3"
    path    = "{{ .ISOPath }}"
    options = "-r"
  }

  network_device {
    device      = "XQ"
    mac         = "08-00-2B-AA-BB-CC"
    attach_type = "nat:tcp={{ .HostPort }}:10.0.2.15:{{ .GuestPort }}"
  }

  boot_wait = "10s"

  boot_steps = [
    [">>>",       "BOOT DUA0<enter>",             "Boot from DUA0"],
    ["Username:", "SYSTEM<enter>",                 "Log in as SYSTEM"],
    ["Password:", "MANAGER<enter>",                "Enter password"],
    ["$",         "@SYS$SYSTEM:SHUTDOWN<enter>",   "Initiate shutdown"],
    ["SHUTDOWN",  "YES<enter>",                    "Confirm shutdown"],
  ]

  communicator = "ssh"
  ssh_username = "system"
  ssh_password = "manager"
  ssh_port     = 22

  host_port_min = 2222
  host_port_max = 4444

  shutdown_timeout = "10m"

  http_directory = "http"
}

build {
  sources = ["source.simh.vax-vms"]

  provisioner "shell" {
    inline = ["SHOW SYSTEM"]
  }
}
```

## Configuration Reference

### Required

| Field | Type | Description |
|-------|------|-------------|
| `simh_binary` | `string` | Name or path of the SIMH simulator binary (e.g. `"vax"`, `"pdp11"`). Resolved via `$PATH`. |

### Output

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `output_directory` | `string` | `"output-<BuildName>"` | Directory for build outputs. |
| `vm_name` | `string` | `"packer-<BuildName>"` | Base name for generated files. |

### CPU & Memory

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `cpu_type` | `string` | `""` | SIMH CPU type (e.g. `"MicroVAX"`). |
| `memory` | `string` | `""` | Memory size (e.g. `"128M"`). |
| `cpu_options` | `[]string` | `[]` | Additional `SET CPU` operands. |

### Disk Attachments

```hcl
disk_attachments {
  device       = "RQ0"                         # Required: SIMH device name
  path         = "{{ .OutputDir }}/system.dsk"  # Required: image path (supports templates)
  set_commands = ["RD54"]                       # Optional: SET commands before ATTACH
  options      = "-r"                           # Optional: ATTACH options
}
```

### Network

```hcl
network_device {
  device      = "XQ"                                                    # Required
  mac         = "08-00-2B-AA-BB-CC"                                     # Optional
  attach_type = "nat:tcp={{ .HostPort }}:10.0.2.15:{{ .GuestPort }}"    # Required
  set_commands = []                                                     # Optional
}
```

`network_commands` — raw SIMH SCP commands for additional network setup.

### Boot Steps

Ordered list of `[expect, send, description]` tuples that interact with
the simulated console over telnet:

```hcl
boot_steps = [
  [">>>",       "BOOT DUA0<enter>",  "Boot from DUA0"],
  ["Username:", "SYSTEM<enter>",     "Log in"],
]
```

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `boot_wait` | `duration` | `"10s"` | Delay before connecting to console. |
| `boot_key_interval` | `duration` | `"0s"` | Minimum interval between keystrokes. |
| `boot_step_timeout` | `duration` | `"5m"` | Timeout per expect pattern. |

### Console

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `console_port_min` | `int` | `4000` | Minimum telnet console port. |
| `console_port_max` | `int` | `4100` | Maximum telnet console port. |
| `console_bind_address` | `string` | `"127.0.0.1"` | Console bind address. |
| `console_log` | `bool` | `true` | Enable console output logging. |

### Communicator

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `comm_host` | `string` | `""` | Override communicator host. |
| `host_port_min` | `int` | `2222` | Minimum host port for SSH. |
| `host_port_max` | `int` | `4444` | Maximum host port for SSH. |
| `skip_port_forward` | `bool` | `false` | Skip host port selection. |

Plus all standard [Packer communicator fields](https://developer.hashicorp.com/packer/docs/communicators/ssh)
(`ssh_username`, `ssh_password`, `ssh_port`, etc.).

### Other

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `command_file` | `string` | `""` | External SIMH command file (overrides generated file). |
| `http_ip` | `string` | `""` | Override HTTP server IP for guest. |
| `simh_args` | `[]string` | `[]` | Extra SCP commands before disk attachments. |
| `shutdown_timeout` | `duration` | `"5m"` | Max time to wait for SIMH to exit. |

### Runtime Template Variables

Available in `boot_steps`, `disk_attachments`, `network_device`,
`network_commands`, and `simh_args`:

| Variable | Description |
|----------|-------------|
| `{{ .HTTPIP }}` | IP to reach Packer's HTTP server |
| `{{ .HTTPPort }}` | HTTP server port |
| `{{ .Name }}` | `vm_name` value |
| `{{ .OutputDir }}` | Absolute output directory path |
| `{{ .ISOPath }}` | Path to downloaded ISO |
| `{{ .SSHPublicKey }}` | Generated SSH public key |
| `{{ .HostPort }}` | Selected host port for communicator |
| `{{ .GuestPort }}` | Guest communicator port |

## Building from Source

### Prerequisites

- [Go](https://go.dev/dl/) 1.21+
- [Packer](https://www.packer.io/downloads) (for testing)

### Build

```bash
go build -o packer-plugin-simh .
```

### Run Tests

```bash
go test -v ./...
```

### Generate HCL2 Spec

After modifying `Config` structs:

```bash
go install github.com/hashicorp/packer-plugin-sdk/cmd/packer-sdc@latest
go generate ./...
```

## Contributing

Contributions are welcome. Please follow these guidelines:

1. **Fork** the repository and create your branch from `main`.
2. **Write tests** for any new functionality.
3. **Run the full test suite** before submitting:
   ```bash
   go test -race ./...
   ```
4. **Format your code**:
   ```bash
   gofmt -w .
   ```
5. **Open a pull request** with a clear description of the change.

### Code Structure

```
packer-plugin-simh/
├── main.go                           # Plugin entry point
├── version/version.go                # Plugin version
├── builder/simh/
│   ├── builder.go                    # Builder struct, Run(), step sequence
│   ├── config.go                     # Config struct, Prepare(), validation
│   ├── config.hcl2spec.go            # Generated HCL2 spec
│   ├── artifact.go                   # Build artifact
│   ├── driver.go                     # SIMH process management
│   ├── driver_mock.go                # Mock driver for tests
│   ├── console_driver.go             # Telnet BCDriver implementation
│   ├── ssh.go                        # Communicator helpers
│   ├── step_prepare_output_dir.go    # Output directory management
│   ├── step_create_command_file.go   # SIMH command file generation
│   ├── step_configure_console.go     # Console port selection
│   ├── step_discover_http_ip.go      # HTTP IP discovery
│   ├── step_forward_port.go          # Host port selection
│   ├── step_run.go                   # SIMH process launch
│   ├── step_boot_command.go          # Console interaction (boot steps)
│   ├── step_shutdown.go              # Shutdown handling
│   └── *_test.go                     # Test files
├── .github/workflows/
│   ├── ci.yml                        # CI pipeline
│   └── release.yml                   # Release automation
└── .goreleaser.yml                   # GoReleaser config
```

## Creating a Release

Releases are automated via GitHub Actions and GoReleaser.

1. **Tag the release**:
   ```bash
   git tag v0.1.0
   git push origin v0.1.0
   ```

2. The `release.yml` workflow will automatically:
   - Build binaries for Linux, macOS, and Windows (amd64 + arm64)
   - Create a GitHub release with the binaries
   - Generate a changelog from commit messages

3. **Version format**: Use [semantic versioning](https://semver.org/) with a
   `v` prefix (e.g. `v0.1.0`, `v1.0.0`).

4. **Pre-release versions**: To create a pre-release, update
   `version/version.go` to set `VersionPrerelease` to a non-empty string
   (e.g. `"rc1"`) and tag accordingly (e.g. `v0.2.0-rc1`).

## License

This project is open source. See the repository for license details.
