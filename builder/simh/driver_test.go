package simh

import (
	"testing"
)

func TestMockDriver_Simh(t *testing.T) {
	d := &MockDriver{}
	err := d.Simh("/tmp/test.simh")

	if !d.SimhCalled {
		t.Error("expected SimhCalled=true")
	}
	if d.SimhFile != "/tmp/test.simh" {
		t.Errorf("expected /tmp/test.simh, got %s", d.SimhFile)
	}
	if err != nil {
		t.Errorf("expected nil error, got %s", err)
	}
}

func TestMockDriver_WaitForShutdown(t *testing.T) {
	d := &MockDriver{WaitResult: true}
	cancelCh := make(chan struct{})

	result := d.WaitForShutdown(cancelCh)
	if !result {
		t.Error("expected WaitResult=true")
	}
}

func TestMockDriver_Stop(t *testing.T) {
	d := &MockDriver{}
	err := d.Stop()

	if !d.StopCalled {
		t.Error("expected StopCalled=true")
	}
	if err != nil {
		t.Errorf("expected nil error, got %s", err)
	}
}

func TestMockDriver_Verify(t *testing.T) {
	d := &MockDriver{}
	err := d.Verify()

	if !d.VerifyCalled {
		t.Error("expected VerifyCalled=true")
	}
	if err != nil {
		t.Errorf("expected nil error, got %s", err)
	}
}

func TestMockDriver_Version(t *testing.T) {
	d := &MockDriver{VersionResult: "4.0"}
	ver, err := d.Version()

	if !d.VersionCalled {
		t.Error("expected VersionCalled=true")
	}
	if ver != "4.0" {
		t.Errorf("expected 4.0, got %s", ver)
	}
	if err != nil {
		t.Errorf("expected nil error, got %s", err)
	}
}
