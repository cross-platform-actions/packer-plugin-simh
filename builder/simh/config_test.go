package simh

import (
	"os"
	"path/filepath"
	"testing"
)

func testConfig() map[string]interface{} {
	return map[string]interface{}{
		"simh_binary":  "vax",
		"communicator": "none",
	}
}

func TestConfigPrepare_Defaults(t *testing.T) {
	c := new(Config)
	_, warns, err := c.Prepare(testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(warns) > 0 {
		t.Logf("warnings: %v", warns)
	}

	if c.OutputDirectory == "" {
		t.Error("expected output_directory to have a default")
	}
	if c.VMName == "" {
		t.Error("expected vm_name to have a default")
	}
	if c.ConsolePortMin != 4000 {
		t.Errorf("expected console_port_min=4000, got %d", c.ConsolePortMin)
	}
	if c.ConsolePortMax != 4100 {
		t.Errorf("expected console_port_max=4100, got %d", c.ConsolePortMax)
	}
	if c.ConsoleBindAddress != "127.0.0.1" {
		t.Errorf("expected console_bind_address=127.0.0.1, got %s", c.ConsoleBindAddress)
	}
	if c.ConsoleLog == nil || !*c.ConsoleLog {
		t.Error("expected console_log to default to true")
	}
	if c.HostPortMin != 2222 {
		t.Errorf("expected host_port_min=2222, got %d", c.HostPortMin)
	}
	if c.HostPortMax != 4444 {
		t.Errorf("expected host_port_max=4444, got %d", c.HostPortMax)
	}
}

func TestConfigPrepare_MissingSimhBinary(t *testing.T) {
	raw := testConfig()
	delete(raw, "simh_binary")

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for missing simh_binary")
	}
}

func TestConfigPrepare_WinRMUnsupported(t *testing.T) {
	raw := testConfig()
	raw["communicator"] = "winrm"
	raw["winrm_username"] = "admin"

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for winrm communicator")
	}
}

func TestConfigPrepare_InvalidPortRanges(t *testing.T) {
	raw := testConfig()
	raw["console_port_min"] = 5000
	raw["console_port_max"] = 4000

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for invalid console port range")
	}
}

func TestConfigPrepare_InvalidHostPortRange(t *testing.T) {
	raw := testConfig()
	raw["host_port_min"] = 5000
	raw["host_port_max"] = 4000

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for invalid host port range")
	}
}

func TestConfigPrepare_DiskAttachmentMissingDevice(t *testing.T) {
	raw := testConfig()
	raw["disk_attachments"] = []map[string]interface{}{
		{
			"path": "/tmp/disk.dsk",
		},
	}

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for missing disk device")
	}
}

func TestConfigPrepare_DiskAttachmentMissingPath(t *testing.T) {
	raw := testConfig()
	raw["disk_attachments"] = []map[string]interface{}{
		{
			"device": "RQ0",
		},
	}

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for missing disk path")
	}
}

func TestConfigPrepare_NetworkDeviceMissingDevice(t *testing.T) {
	raw := testConfig()
	raw["network_device"] = map[string]interface{}{
		"attach_type": "nat:tcp=2222:10.0.2.15:22",
	}

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for missing network device")
	}
}

func TestConfigPrepare_NetworkDeviceMissingAttachType(t *testing.T) {
	raw := testConfig()
	raw["network_device"] = map[string]interface{}{
		"device": "XQ",
	}

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for missing attach_type")
	}
}

func TestConfigPrepare_BootStepsWrongLength(t *testing.T) {
	raw := testConfig()
	raw["boot_steps"] = [][]string{
		{"expect", "send"},
	}

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for boot_steps with wrong element count")
	}
}

func TestConfigPrepare_BootStepsValid(t *testing.T) {
	raw := testConfig()
	raw["boot_steps"] = [][]string{
		{"", "BOOT CPU<enter>", "Boot"},
		{">>>", "BOOT DUA0<enter>", "Boot from DUA0"},
	}

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if len(c.BootSteps) != 2 {
		t.Errorf("expected 2 boot_steps, got %d", len(c.BootSteps))
	}
}

func TestConfigPrepare_OutputDirExists(t *testing.T) {
	dir := t.TempDir()

	raw := testConfig()
	raw["output_directory"] = dir

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error when output_directory exists without force")
	}
}

func TestConfigPrepare_OutputDirExistsWithForce(t *testing.T) {
	dir := t.TempDir()

	raw := testConfig()
	raw["output_directory"] = dir
	raw["packer_force"] = true

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error with force: %s", err)
	}
}

func TestConfigPrepare_CommandFileNotExists(t *testing.T) {
	raw := testConfig()
	raw["command_file"] = "/nonexistent/file.simh"

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err == nil {
		t.Error("expected error for nonexistent command_file")
	}
}

func TestConfigPrepare_CommandFileExists(t *testing.T) {
	tmpFile := filepath.Join(t.TempDir(), "test.simh")
	if err := os.WriteFile(tmpFile, []byte("BOOT CPU\n"), 0644); err != nil {
		t.Fatal(err)
	}

	raw := testConfig()
	raw["command_file"] = tmpFile

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestConfigPrepare_ISOOptional(t *testing.T) {
	raw := testConfig()
	// No iso_url — should succeed.

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !c.isoSkipped {
		t.Error("expected isoSkipped to be true when no ISO configured")
	}
}

func TestConfigPrepare_OutputDirAbsolute(t *testing.T) {
	raw := testConfig()
	raw["output_directory"] = "relative-dir"

	c := new(Config)
	_, _, err := c.Prepare(raw)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
	if !filepath.IsAbs(c.OutputDirectory) {
		t.Errorf("expected output_directory to be absolute, got %s", c.OutputDirectory)
	}
}

func TestConfigPrepare_GeneratedVars(t *testing.T) {
	c := new(Config)
	generatedVars, _, err := c.Prepare(testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	expected := map[string]bool{
		"ID": true, "Host": true, "Port": true, "SSHPublicKey": true,
	}
	for _, v := range generatedVars {
		if !expected[v] {
			t.Errorf("unexpected generated var: %s", v)
		}
		delete(expected, v)
	}
	for v := range expected {
		t.Errorf("missing generated var: %s", v)
	}
}
