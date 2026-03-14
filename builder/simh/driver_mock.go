package simh

// MockDriver implements the Driver interface for unit tests.
type MockDriver struct {
	SimhCalled bool
	SimhFile   string
	SimhErr    error

	WaitCalled bool
	WaitResult bool

	StopCalled bool
	StopErr    error

	VerifyCalled bool
	VerifyErr    error

	VersionCalled bool
	VersionResult string
	VersionErr    error
}

func (d *MockDriver) Simh(commandFile string) error {
	d.SimhCalled = true
	d.SimhFile = commandFile
	return d.SimhErr
}

func (d *MockDriver) WaitForShutdown(cancelCh <-chan struct{}) bool {
	d.WaitCalled = true
	if d.WaitResult {
		return true
	}
	<-cancelCh
	return false
}

func (d *MockDriver) Stop() error {
	d.StopCalled = true
	return d.StopErr
}

func (d *MockDriver) Verify() error {
	d.VerifyCalled = true
	return d.VerifyErr
}

func (d *MockDriver) Version() (string, error) {
	d.VersionCalled = true
	if d.VersionResult == "" {
		return "unknown", d.VersionErr
	}
	return d.VersionResult, d.VersionErr
}
