package simh

import (
	"github.com/hashicorp/packer-plugin-sdk/communicator"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
)

// commHost returns a function that provides the host address for the
// communicator.
func commHost(host string) func(multistep.StateBag) (string, error) {
	return func(state multistep.StateBag) (string, error) {
		if host != "" {
			return host, nil
		}
		return "127.0.0.1", nil
	}
}

// commPort returns a function that provides the port for the communicator.
func commPort(commConfig *communicator.Config) func(multistep.StateBag) (int, error) {
	return func(state multistep.StateBag) (int, error) {
		if port, ok := state.GetOk("commHostPort"); ok {
			return port.(int), nil
		}
		return commConfig.Port(), nil
	}
}
