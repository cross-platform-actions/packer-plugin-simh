package simh

import (
	"fmt"
	"io"
	"time"
	"unicode/utf8"

	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
)

// telnetDriver implements the bootcommand.BCDriver interface, sending
// keystrokes to the simulated console. It writes through an io.Writer (the
// consoleReader) rather than holding a net.Conn directly, so keystrokes always
// reach the live connection even after the reader transparently reconnects.
type telnetDriver struct {
	w        io.Writer
	interval time.Duration
	ctrlHeld bool
	altHeld  bool
}

// Flush is a no-op — the telnet connection is unbuffered.
func (d *telnetDriver) Flush() error {
	return nil
}

// SendKey sends a single key to the telnet connection.
func (d *telnetDriver) SendKey(key rune, action bootcommand.KeyAction) error {
	if action == bootcommand.KeyOn || action == bootcommand.KeyOff {
		return nil
	}

	var buf []byte

	if d.ctrlHeld {
		// Convert to control character: Ctrl+A = 0x01, Ctrl+Z = 0x1A
		if key >= 'a' && key <= 'z' {
			buf = []byte{byte(key - 0x60)}
		} else if key >= 'A' && key <= 'Z' {
			buf = []byte{byte(key - 0x40)}
		} else {
			// For other characters with Ctrl, just send the character.
			buf = make([]byte, utf8.RuneLen(key))
			utf8.EncodeRune(buf, key)
		}
	} else if d.altHeld {
		// Alt/Meta sends ESC prefix + char.
		charBuf := make([]byte, utf8.RuneLen(key))
		utf8.EncodeRune(charBuf, key)
		buf = append([]byte{0x1b}, charBuf...)
	} else {
		buf = make([]byte, utf8.RuneLen(key))
		utf8.EncodeRune(buf, key)
	}

	if _, err := d.w.Write(buf); err != nil {
		return fmt.Errorf("error sending key to console: %s", err)
	}

	if d.interval > 0 {
		time.Sleep(d.interval)
	}

	return nil
}

// SendSpecial sends a special key to the telnet connection.
func (d *telnetDriver) SendSpecial(special string, action bootcommand.KeyAction) error {
	// Handle modifier key state tracking.
	switch special {
	case "leftCtrl":
		switch action {
		case bootcommand.KeyOn:
			d.ctrlHeld = true
			return nil
		case bootcommand.KeyOff:
			d.ctrlHeld = false
			return nil
		}
	case "leftAlt":
		switch action {
		case bootcommand.KeyOn:
			d.altHeld = true
			return nil
		case bootcommand.KeyOff:
			d.altHeld = false
			return nil
		}
	case "rightCtrl", "leftShift", "rightShift", "leftSuper", "rightSuper", "menu":
		// Not meaningful for text terminals.
		return nil
	}

	// For KeyOn/KeyOff of non-modifier keys, no-op.
	if action == bootcommand.KeyOn || action == bootcommand.KeyOff {
		return nil
	}

	// Map special key names to terminal escape sequences.
	var seq []byte
	switch special {
	case "enter", "return":
		seq = []byte{'\r'}
	case "tab":
		seq = []byte{'\t'}
	case "bs":
		seq = []byte{0x08}
	case "del":
		seq = []byte{0x7f}
	case "esc":
		seq = []byte{0x1b}
	case "spacebar":
		seq = []byte{0x20}
	case "up":
		seq = []byte{0x1b, '[', 'A'}
	case "down":
		seq = []byte{0x1b, '[', 'B'}
	case "right":
		seq = []byte{0x1b, '[', 'C'}
	case "left":
		seq = []byte{0x1b, '[', 'D'}
	case "insert":
		seq = []byte{0x1b, '[', '2', '~'}
	case "home":
		seq = []byte{0x1b, '[', 'H'}
	case "end":
		seq = []byte{0x1b, '[', 'F'}
	case "pageUp":
		seq = []byte{0x1b, '[', '5', '~'}
	case "pageDown":
		seq = []byte{0x1b, '[', '6', '~'}
	case "f1":
		seq = []byte{0x1b, 'O', 'P'}
	case "f2":
		seq = []byte{0x1b, 'O', 'Q'}
	case "f3":
		seq = []byte{0x1b, 'O', 'R'}
	case "f4":
		seq = []byte{0x1b, 'O', 'S'}
	case "f5":
		seq = []byte{0x1b, '[', '1', '5', '~'}
	case "f6":
		seq = []byte{0x1b, '[', '1', '7', '~'}
	case "f7":
		seq = []byte{0x1b, '[', '1', '8', '~'}
	case "f8":
		seq = []byte{0x1b, '[', '1', '9', '~'}
	case "f9":
		seq = []byte{0x1b, '[', '2', '0', '~'}
	case "f10":
		seq = []byte{0x1b, '[', '2', '1', '~'}
	case "f11":
		seq = []byte{0x1b, '[', '2', '3', '~'}
	case "f12":
		seq = []byte{0x1b, '[', '2', '4', '~'}
	case "leftCtrl":
		// KeyPress for Ctrl alone — no-op (handled by modifier tracking).
		return nil
	case "leftAlt":
		// KeyPress for Alt alone — no-op.
		return nil
	default:
		// Unknown special key — log and ignore.
		return nil
	}

	if _, err := d.w.Write(seq); err != nil {
		return fmt.Errorf("error sending special key to console: %s", err)
	}

	if d.interval > 0 {
		time.Sleep(d.interval)
	}

	return nil
}
