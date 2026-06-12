package simh

import (
	"net"
	"testing"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
)

// mockConn creates a pair of connected net.Conn for testing.
func mockConn() (client net.Conn, server net.Conn) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	defer func() { _ = l.Close() }()

	doneCh := make(chan net.Conn, 1)
	go func() {
		conn, err := l.Accept()
		if err != nil {
			panic(err)
		}
		doneCh <- conn
	}()

	client, err = net.Dial("tcp", l.Addr().String())
	if err != nil {
		panic(err)
	}
	server = <-doneCh
	return
}

func TestTelnetDriver_SendKey(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	driver := &telnetDriver{w: client}

	err := driver.SendKey('A', bootcommand.KeyPress)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 10)
	_ = server.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := server.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || buf[0] != 'A' {
		t.Errorf("expected 'A', got %q", buf[:n])
	}
}

func TestTelnetDriver_SendKeyCtrl(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	driver := &telnetDriver{w: client, ctrlHeld: true}

	err := driver.SendKey('c', bootcommand.KeyPress)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 10)
	_ = server.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := server.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	// Ctrl+C = 0x03
	if n != 1 || buf[0] != 0x03 {
		t.Errorf("expected Ctrl+C (0x03), got %v", buf[:n])
	}
}

func TestTelnetDriver_SendKeyAlt(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	driver := &telnetDriver{w: client, altHeld: true}

	err := driver.SendKey('x', bootcommand.KeyPress)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 10)
	_ = server.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := server.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	// Alt+x = ESC + 'x'
	if n != 2 || buf[0] != 0x1b || buf[1] != 'x' {
		t.Errorf("expected ESC+x, got %v", buf[:n])
	}
}

func TestTelnetDriver_SendSpecial_Enter(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	driver := &telnetDriver{w: client}

	err := driver.SendSpecial("enter", bootcommand.KeyPress)
	if err != nil {
		t.Fatal(err)
	}

	buf := make([]byte, 10)
	_ = server.SetReadDeadline(time.Now().Add(1 * time.Second))
	n, err := server.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || buf[0] != '\r' {
		t.Errorf("expected CR, got %v", buf[:n])
	}
}

func TestTelnetDriver_SendSpecial_ArrowKeys(t *testing.T) {
	tests := []struct {
		key      string
		expected []byte
	}{
		{"up", []byte{0x1b, '[', 'A'}},
		{"down", []byte{0x1b, '[', 'B'}},
		{"right", []byte{0x1b, '[', 'C'}},
		{"left", []byte{0x1b, '[', 'D'}},
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			client, server := mockConn()
			defer func() { _ = client.Close() }()
			defer func() { _ = server.Close() }()

			driver := &telnetDriver{w: client}
			err := driver.SendSpecial(tt.key, bootcommand.KeyPress)
			if err != nil {
				t.Fatal(err)
			}

			buf := make([]byte, 10)
			_ = server.SetReadDeadline(time.Now().Add(1 * time.Second))
			n, err := server.Read(buf)
			if err != nil {
				t.Fatal(err)
			}

			if n != len(tt.expected) {
				t.Errorf("expected %d bytes, got %d", len(tt.expected), n)
			}
			for i, b := range tt.expected {
				if i < n && buf[i] != b {
					t.Errorf("byte %d: expected %02x, got %02x", i, b, buf[i])
				}
			}
		})
	}
}

func TestTelnetDriver_SendKey_KeyOnOff_NoOp(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	driver := &telnetDriver{w: client}

	// KeyOn and KeyOff should be no-ops.
	if err := driver.SendKey('a', bootcommand.KeyOn); err != nil {
		t.Fatal(err)
	}
	if err := driver.SendKey('a', bootcommand.KeyOff); err != nil {
		t.Fatal(err)
	}

	// Nothing should have been sent.
	_ = server.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 10)
	n, _ := server.Read(buf)
	if n != 0 {
		t.Errorf("expected nothing sent for KeyOn/KeyOff, got %d bytes", n)
	}
}

func TestTelnetDriver_ModifierTracking(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	driver := &telnetDriver{w: client}

	// Enable Ctrl.
	_ = driver.SendSpecial("leftCtrl", bootcommand.KeyOn)
	if !driver.ctrlHeld {
		t.Error("expected ctrlHeld=true after KeyOn")
	}

	// Disable Ctrl.
	_ = driver.SendSpecial("leftCtrl", bootcommand.KeyOff)
	if driver.ctrlHeld {
		t.Error("expected ctrlHeld=false after KeyOff")
	}

	// Enable Alt.
	_ = driver.SendSpecial("leftAlt", bootcommand.KeyOn)
	if !driver.altHeld {
		t.Error("expected altHeld=true after KeyOn")
	}

	// Disable Alt.
	_ = driver.SendSpecial("leftAlt", bootcommand.KeyOff)
	if driver.altHeld {
		t.Error("expected altHeld=false after KeyOff")
	}
}

func TestTelnetDriver_UnsupportedModifiers(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	driver := &telnetDriver{w: client}

	unsupported := []string{"rightCtrl", "leftShift", "rightShift", "leftSuper", "rightSuper", "menu"}
	for _, mod := range unsupported {
		if err := driver.SendSpecial(mod, bootcommand.KeyPress); err != nil {
			t.Errorf("unexpected error for %s: %s", mod, err)
		}
	}

	// Nothing should have been sent.
	_ = server.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	buf := make([]byte, 100)
	n, _ := server.Read(buf)
	if n != 0 {
		t.Errorf("expected nothing sent for unsupported modifiers, got %d bytes", n)
	}
}

func TestStripTelnetIAC(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected []byte
	}{
		{
			"no IAC",
			[]byte("Hello World"),
			[]byte("Hello World"),
		},
		{
			"escaped 0xFF",
			[]byte{0xFF, 0xFF},
			[]byte{0xFF},
		},
		{
			"WILL option",
			[]byte{0xFF, 0xFB, 0x01, 'A'},
			[]byte{'A'},
		},
		{
			"DO option",
			[]byte{0xFF, 0xFD, 0x03, 'B'},
			[]byte{'B'},
		},
		{
			"mixed",
			[]byte{'H', 0xFF, 0xFB, 0x01, 'e', 'l', 0xFF, 0xFD, 0x03, 'l', 'o'},
			[]byte("Hello"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, leftover := stripTelnetIAC(tt.input)
			if len(leftover) != 0 {
				t.Errorf("expected no leftover for complete input, got %v", leftover)
			}
			if len(result) != len(tt.expected) {
				t.Errorf("expected %d bytes, got %d", len(tt.expected), len(result))
				return
			}
			for i := range result {
				if result[i] != tt.expected[i] {
					t.Errorf("byte %d: expected %02x, got %02x", i, tt.expected[i], result[i])
				}
			}
		})
	}
}

// TestStripTelnetIAC_SplitAcrossReads verifies that an IAC sequence split
// across read boundaries is not corrupted: the trailing partial bytes are
// returned as leftover to be prepended to the next chunk.
func TestStripTelnetIAC_SplitAcrossReads(t *testing.T) {
	// A 3-byte IAC WILL <opt> sequence arriving as two chunks: the first
	// ends with the bare IAC byte, the second carries WILL <opt> then data.
	c1, lo1 := stripTelnetIAC([]byte{'H', 'i', 0xFF})
	if string(c1) != "Hi" {
		t.Errorf("chunk1 cleaned: expected %q, got %q", "Hi", c1)
	}
	if len(lo1) != 1 || lo1[0] != 0xFF {
		t.Errorf("chunk1 leftover: expected [0xFF], got %v", lo1)
	}

	// Caller prepends leftover to the next chunk.
	c2, lo2 := stripTelnetIAC(append(append([]byte(nil), lo1...), 0xFB, 0x01, 'O', 'K'))
	if len(lo2) != 0 {
		t.Errorf("chunk2 leftover: expected none, got %v", lo2)
	}
	if string(c2) != "OK" {
		t.Errorf("chunk2 cleaned: expected %q, got %q", "OK", c2)
	}

	// Split in the middle of the 3-byte sequence (after IAC WILL, before opt).
	c3, lo3 := stripTelnetIAC([]byte{'X', 0xFF, 0xFB})
	if string(c3) != "X" {
		t.Errorf("chunk3 cleaned: expected %q, got %q", "X", c3)
	}
	if len(lo3) != 2 || lo3[0] != 0xFF || lo3[1] != 0xFB {
		t.Errorf("chunk3 leftover: expected [0xFF 0xFB], got %v", lo3)
	}
	c4, lo4 := stripTelnetIAC(append(append([]byte(nil), lo3...), 0x01, 'Y'))
	if len(lo4) != 0 {
		t.Errorf("chunk4 leftover: expected none, got %v", lo4)
	}
	if string(c4) != "Y" {
		t.Errorf("chunk4 cleaned: expected %q, got %q", "Y", c4)
	}
}
