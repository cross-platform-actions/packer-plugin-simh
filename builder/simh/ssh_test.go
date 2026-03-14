package simh

import (
	"testing"

	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

func TestCommHost_Override(t *testing.T) {
	fn := commHost("192.168.1.100")
	state := new(multistep.BasicStateBag)

	host, err := fn(state)
	if err != nil {
		t.Fatal(err)
	}
	if host != "192.168.1.100" {
		t.Errorf("expected 192.168.1.100, got %s", host)
	}
}

func TestCommHost_Default(t *testing.T) {
	fn := commHost("")
	state := new(multistep.BasicStateBag)

	host, err := fn(state)
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Errorf("expected 127.0.0.1, got %s", host)
	}
}

func TestCommPort_FromState(t *testing.T) {
	commConfig := &communicator.Config{}
	fn := commPort(commConfig)

	state := new(multistep.BasicStateBag)
	state.Put("commHostPort", 2345)

	port, err := fn(state)
	if err != nil {
		t.Fatal(err)
	}
	if port != 2345 {
		t.Errorf("expected 2345, got %d", port)
	}
}

func TestCommPort_Default(t *testing.T) {
	commConfig := &communicator.Config{}
	commConfig.Type = "ssh"
	commConfig.SSHPort = 22
	fn := commPort(commConfig)

	state := new(multistep.BasicStateBag)

	port, err := fn(state)
	if err != nil {
		t.Fatal(err)
	}
	if port != 22 {
		t.Errorf("expected 22, got %d", port)
	}
}
