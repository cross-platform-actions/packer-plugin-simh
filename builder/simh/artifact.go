package simh

import (
	"fmt"
	"os"
)

// Artifact represents the result of a SIMH build.
type Artifact struct {
	dir   string
	f     []string
	state map[string]interface{}
}

// BuilderId returns the unique ID for this builder.
func (a *Artifact) BuilderId() string {
	return BuilderId
}

// Files returns the list of files in the artifact.
func (a *Artifact) Files() []string {
	return a.f
}

// Id returns the artifact ID.
func (a *Artifact) Id() string {
	return "VM"
}

// String returns a human-readable description of the artifact.
func (a *Artifact) String() string {
	return fmt.Sprintf("VM files in directory: %s", a.dir)
}

// State returns artifact state by name.
func (a *Artifact) State(name string) interface{} {
	if a.state != nil {
		return a.state[name]
	}
	return nil
}

// Destroy removes the artifact files.
func (a *Artifact) Destroy() error {
	return os.RemoveAll(a.dir)
}
