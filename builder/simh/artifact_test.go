package simh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestArtifact_BuilderId(t *testing.T) {
	a := &Artifact{}
	if a.BuilderId() != "simh.simh" {
		t.Errorf("expected BuilderId=simh.simh, got %s", a.BuilderId())
	}
}

func TestArtifact_Id(t *testing.T) {
	a := &Artifact{}
	if a.Id() != "VM" {
		t.Errorf("expected Id=VM, got %s", a.Id())
	}
}

func TestArtifact_String(t *testing.T) {
	a := &Artifact{dir: "/tmp/output"}
	expected := "VM files in directory: /tmp/output"
	if a.String() != expected {
		t.Errorf("expected %q, got %q", expected, a.String())
	}
}

func TestArtifact_Files(t *testing.T) {
	files := []string{"/tmp/output/disk.dsk", "/tmp/output/rom.bin"}
	a := &Artifact{f: files}
	if len(a.Files()) != 2 {
		t.Errorf("expected 2 files, got %d", len(a.Files()))
	}
}

func TestArtifact_State(t *testing.T) {
	a := &Artifact{
		state: map[string]interface{}{
			"simulator": "vax",
		},
	}
	if a.State("simulator") != "vax" {
		t.Errorf("expected state simulator=vax, got %v", a.State("simulator"))
	}
	if a.State("nonexistent") != nil {
		t.Error("expected nil for nonexistent state key")
	}
}

func TestArtifact_StateNilMap(t *testing.T) {
	a := &Artifact{}
	if a.State("anything") != nil {
		t.Error("expected nil for nil state map")
	}
}

func TestArtifact_Destroy(t *testing.T) {
	dir := t.TempDir()
	testFile := filepath.Join(dir, "test.txt")
	_ = os.WriteFile(testFile, []byte("test"), 0644)

	a := &Artifact{dir: dir}
	if err := a.Destroy(); err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("expected directory to be removed")
	}
}
