package simh

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

type stepBootCommand struct {
	conn net.Conn
}

func (s *stepBootCommand) Run(ctx context.Context, state multistep.StateBag) multistep.StepAction {
	config := state.Get("config").(*Config)
	ui := state.Get("ui").(packer.Ui)

	if len(config.BootSteps) == 0 {
		ui.Say("No boot steps defined, skipping boot command.")
		return multistep.ActionContinue
	}

	// Wait boot_wait before connecting.
	ui.Say(fmt.Sprintf("Waiting %s before connecting to console...", config.BootWait))
	select {
	case <-time.After(config.BootWait):
	case <-ctx.Done():
		return multistep.ActionHalt
	}

	consolePort := state.Get("console_port").(int)
	addr := fmt.Sprintf("%s:%d", config.ConsoleBindAddress, consolePort)

	// Connect with retry and exponential backoff.
	var conn net.Conn
	var err error
	retryDeadline := time.Now().Add(30 * time.Second)
	backoff := 500 * time.Millisecond

	for time.Now().Before(retryDeadline) {
		conn, err = net.DialTimeout("tcp", addr, 5*time.Second)
		if err == nil {
			break
		}
		log.Printf("[DEBUG] Failed to connect to console at %s: %s, retrying...", addr, err)
		select {
		case <-time.After(backoff):
			backoff = backoff * 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		case <-ctx.Done():
			return multistep.ActionHalt
		}
	}

	if err != nil {
		err := fmt.Errorf("failed to connect to console at %s after 30s: %s", addr, err)
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	s.conn = conn
	ui.Say(fmt.Sprintf("Connected to console at %s", addr))

	// Populate template data.
	td := populateTemplateData(config, state)
	config.ctx.Data = &td

	// Create telnet driver.
	driver := &telnetDriver{
		conn:     conn,
		interval: config.BootKeyInterval,
	}

	// Console output buffer.
	var consoleBuf bytes.Buffer

	// Process each boot step.
	for i, step := range config.BootSteps {
		if len(step) != 3 {
			err := fmt.Errorf("boot_steps[%d]: invalid format", i)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		expect := step[0]
		send := step[1]
		description := step[2]

		ui.Say(fmt.Sprintf("Boot step: %s", description))

		// Interpolate expect string.
		renderedExpect, err := interpolate.Render(expect, &config.ctx)
		if err != nil {
			err := fmt.Errorf("error interpolating expect pattern: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		// Wait for expect pattern.
		if renderedExpect != "" {
			if err := waitForExpect(ctx, conn, &consoleBuf, renderedExpect,
				config.BootStepTimeout, ui); err != nil {
				state.Put("error", err)
				ui.Error(err.Error())
				return multistep.ActionHalt
			}
		}

		// Interpolate send string.
		renderedSend, err := interpolate.Render(send, &config.ctx)
		if err != nil {
			err := fmt.Errorf("error interpolating send text: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		// Parse and execute send via bootcommand.
		seq, err := bootcommand.GenerateExpressionSequence(renderedSend)
		if err != nil {
			err := fmt.Errorf("error parsing boot command '%s': %s", renderedSend, err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		if err := seq.Do(ctx, driver); err != nil {
			err := fmt.Errorf("error executing boot command: %s", err)
			state.Put("error", err)
			ui.Error(err.Error())
			return multistep.ActionHalt
		}

		if config.PackerDebug {
			ui.Say(fmt.Sprintf("  Matched: %q, Sent: %q", renderedExpect, renderedSend))
		}
	}

	ui.Say("Boot steps completed successfully.")
	return multistep.ActionContinue
}

func (s *stepBootCommand) Cleanup(state multistep.StateBag) {
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// waitForExpect reads from conn and accumulates into buf until the expect
// substring is found or the timeout expires.
func waitForExpect(ctx context.Context, conn net.Conn, buf *bytes.Buffer,
	expect string, timeout time.Duration, ui packer.Ui) error {

	deadline := time.Now().Add(timeout)
	readBuf := make([]byte, 4096)

	for {
		// Check if pattern already in buffer.
		if strings.Contains(buf.String(), expect) {
			return nil
		}

		// Check context cancellation.
		select {
		case <-ctx.Done():
			return fmt.Errorf("build cancelled while waiting for pattern %q", expect)
		default:
		}

		// Check timeout.
		if time.Now().After(deadline) {
			lastBytes := buf.String()
			if len(lastBytes) > 500 {
				lastBytes = lastBytes[len(lastBytes)-500:]
			}
			return fmt.Errorf("timeout waiting for pattern %q (last console output: %q)", expect, lastBytes)
		}

		// Set read deadline to avoid blocking indefinitely.
		readDeadline := time.Now().Add(1 * time.Second)
		if readDeadline.After(deadline) {
			readDeadline = deadline
		}
		conn.SetReadDeadline(readDeadline)

		n, err := conn.Read(readBuf)
		if n > 0 {
			// Strip telnet IAC sequences.
			cleaned := stripTelnetIAC(readBuf[:n])
			buf.Write(cleaned)
		}

		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			return fmt.Errorf("error reading from console: %s", err)
		}
	}
}

// stripTelnetIAC removes telnet IAC (Interpret As Command) sequences from
// the data and responds with WONT/DONT to all negotiation requests.
func stripTelnetIAC(data []byte) []byte {
	var result []byte
	i := 0
	for i < len(data) {
		if data[i] == 0xFF { // IAC
			if i+1 >= len(data) {
				break
			}
			switch data[i+1] {
			case 0xFF: // Escaped 0xFF
				result = append(result, 0xFF)
				i += 2
			case 0xFB, 0xFC, 0xFD, 0xFE: // WILL, WONT, DO, DONT
				if i+2 < len(data) {
					i += 3
				} else {
					i = len(data)
				}
			default:
				// Other IAC command (2 bytes).
				i += 2
			}
		} else {
			result = append(result, data[i])
			i++
		}
	}
	return result
}
