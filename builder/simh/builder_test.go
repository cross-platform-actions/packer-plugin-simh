package simh

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuilderPrepare_MinimalConfig(t *testing.T) {
	b := &Builder{}
	_, _, err := b.Prepare(testConfig())
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}
}

func TestBuilderPrepare_MissingRequired(t *testing.T) {
	b := &Builder{}
	_, _, err := b.Prepare(map[string]interface{}{})
	if err == nil {
		t.Error("expected error for missing required fields")
	}
}

func TestCollectArtifactFiles(t *testing.T) {
	dir := t.TempDir()

	// Create test files.
	_ = os.WriteFile(filepath.Join(dir, "disk.dsk"), []byte("disk"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "test-vm.simh"), []byte("cmd"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "test-vm.console.log"), []byte("log"), 0644)
	_ = os.WriteFile(filepath.Join(dir, "rom.bin"), []byte("rom"), 0644)

	files, err := collectArtifactFiles(dir, "test-vm")
	if err != nil {
		t.Fatal(err)
	}

	// Should include disk.dsk and rom.bin, exclude .simh and .console.log.
	if len(files) != 2 {
		t.Errorf("expected 2 artifact files, got %d: %v", len(files), files)
	}

	fileNames := map[string]bool{}
	for _, f := range files {
		fileNames[filepath.Base(f)] = true
	}
	if !fileNames["disk.dsk"] {
		t.Error("expected disk.dsk in artifacts")
	}
	if !fileNames["rom.bin"] {
		t.Error("expected rom.bin in artifacts")
	}
	if fileNames["test-vm.simh"] {
		t.Error("expected test-vm.simh to be excluded")
	}
	if fileNames["test-vm.console.log"] {
		t.Error("expected test-vm.console.log to be excluded")
	}
}
