package simh

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// newTestConsoleReader builds a consoleReader with no connection and no
// reader goroutine; tests inject console bytes directly via inject.
func newTestConsoleReader() *consoleReader {
	return &consoleReader{stop: make(chan struct{})}
}

// inject appends bytes to the reader's buffer the same way the reader
// goroutine would.
func (r *consoleReader) inject(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf.WriteString(s)
}

func TestConsoleReader_WriteFollowsCurrentConn(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	r := &consoleReader{conn: client}

	if _, err := r.Write([]byte("hi")); err != nil {
		t.Fatalf("write error: %s", err)
	}
	if got := readWithDeadline(t, server); got != "hi" {
		t.Errorf("expected %q on first conn, got %q", "hi", got)
	}

	// Simulate a reconnect: the reader swaps to a fresh connection. Writes
	// must follow the new connection, not the old one.
	client2, server2 := mockConn()
	defer func() { _ = client2.Close() }()
	defer func() { _ = server2.Close() }()
	r.mu.Lock()
	r.conn = client2
	r.mu.Unlock()

	if _, err := r.Write([]byte("yo")); err != nil {
		t.Fatalf("write error after reconnect: %s", err)
	}
	if got := readWithDeadline(t, server2); got != "yo" {
		t.Errorf("expected %q on reconnected conn, got %q", "yo", got)
	}
}

func TestConsoleReader_WriteWithoutConn(t *testing.T) {
	r := &consoleReader{} // writeTimeout 0 -> fail fast
	if _, err := r.Write([]byte("x")); err == nil {
		t.Error("expected error when writing with no established connection")
	}
}

func TestConsoleReader_WriteRetriesUntilReconnect(t *testing.T) {
	client, server := mockConn()
	defer func() { _ = client.Close() }()
	defer func() { _ = server.Close() }()

	// conn starts nil (a reconnect is "in progress"); the write must wait for
	// the reconnection rather than fail.
	r := &consoleReader{writeTimeout: 2 * time.Second, stop: make(chan struct{})}

	go func() {
		time.Sleep(150 * time.Millisecond)
		r.mu.Lock()
		r.conn = client
		r.mu.Unlock()
	}()

	if _, err := r.Write([]byte("late")); err != nil {
		t.Fatalf("expected Write to succeed once the reader reconnects, got %s", err)
	}
	if got := readWithDeadline(t, server); got != "late" {
		t.Errorf("expected %q, got %q", "late", got)
	}
}

func TestConsoleReader_WriteAbortsWhenVMExits(t *testing.T) {
	vmDone := make(chan struct{})
	close(vmDone) // SIMH already exited
	r := &consoleReader{writeTimeout: 5 * time.Second, stop: make(chan struct{}), vmDone: vmDone}

	if _, err := r.Write([]byte("x")); err == nil {
		t.Error("expected Write to abort promptly when SIMH has exited")
	}
}

func readWithDeadline(t *testing.T, c net.Conn) string {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(1 * time.Second))
	buf := make([]byte, 32)
	n, err := c.Read(buf)
	if err != nil {
		t.Fatalf("read error: %s", err)
	}
	return string(buf[:n])
}

func TestConsoleReader_Pos(t *testing.T) {
	r := newTestConsoleReader()

	if got := r.pos(); got != 0 {
		t.Errorf("expected pos 0 on empty buffer, got %d", got)
	}

	r.inject("hello")
	if got := r.pos(); got != 5 {
		t.Errorf("expected pos 5, got %d", got)
	}
}

func TestConsoleReader_WaitFor_FromZero_MatchesExistingContent(t *testing.T) {
	r := newTestConsoleReader()
	r.inject("login: ")

	err := r.waitFor(context.Background(), "login:", 0, 1*time.Second)
	if err != nil {
		t.Errorf("expected match against existing content with from=0, got: %s", err)
	}
}

func TestConsoleReader_WaitFor_RepeatedPrompt_IgnoresStaleOccurrence(t *testing.T) {
	r := newTestConsoleReader()

	// The buffer already contains a first occurrence of the prompt
	// (e.g. root's password prompt). A waitFor whose window starts
	// past it must not match until a second occurrence arrives.
	r.inject("New password: ")
	from := r.pos()

	done := make(chan error, 1)
	go func() {
		done <- r.waitFor(context.Background(), "New password:", from, 3*time.Second)
	}()

	// Must still be blocked: only stale text is in the window.
	select {
	case err := <-done:
		t.Fatalf("waitFor returned early on stale occurrence (err=%v)", err)
	case <-time.After(300 * time.Millisecond):
	}

	// A fresh occurrence arrives — now it must match.
	r.inject("Retype guard\r\nNew password: ")

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("expected match on fresh occurrence, got: %s", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitFor did not match the fresh occurrence")
	}
}

func TestConsoleReader_WaitFor_FromBeyondBufferIsSafe(t *testing.T) {
	r := newTestConsoleReader()
	r.inject("abc")

	// from beyond the current buffer length must not panic and must
	// not match existing content.
	err := r.waitFor(context.Background(), "abc", 100, 300*time.Millisecond)
	if err == nil {
		t.Error("expected timeout with from beyond buffer, got match")
	}
}

func TestConsoleReader_WaitFor_TimeoutIncludesOutputSinceFrom(t *testing.T) {
	r := newTestConsoleReader()
	r.inject("old screen contents")
	from := r.pos()
	r.inject("fresh window output")

	err := r.waitFor(context.Background(), "never appears", from, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "fresh window output") {
		t.Errorf("timeout error should include output since from, got: %s", err)
	}
	if strings.Contains(err.Error(), "old screen contents") {
		t.Errorf("timeout error should not include pre-window output when fresh output exists, got: %s", err)
	}
}

func TestConsoleReader_WaitFor_TimeoutFallsBackToOverallTail(t *testing.T) {
	r := newTestConsoleReader()
	r.inject("only old output")
	from := r.pos()

	// Nothing arrived since from — the error should fall back to the
	// overall tail so the operator still sees something.
	err := r.waitFor(context.Background(), "never appears", from, 300*time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if !strings.Contains(err.Error(), "only old output") {
		t.Errorf("timeout error should fall back to overall tail, got: %s", err)
	}
}

func TestConsoleReader_WaitFor_ContextCancelled(t *testing.T) {
	r := newTestConsoleReader()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.waitFor(ctx, "never appears", 0, 10*time.Second)
	}()

	cancel()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "cancelled") {
			t.Errorf("expected cancellation error, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("waitFor did not return after context cancellation")
	}
}

func TestConsoleReader_TailSince(t *testing.T) {
	r := newTestConsoleReader()
	r.inject("0123456789")

	if got := r.tailSince(4, 100); got != "456789" {
		t.Errorf("expected %q, got %q", "456789", got)
	}
	// Truncated to the last n bytes of the window.
	if got := r.tailSince(0, 3); got != "789" {
		t.Errorf("expected %q, got %q", "789", got)
	}
	// Nothing since from: fall back to the whole buffer.
	if got := r.tailSince(10, 100); got != "0123456789" {
		t.Errorf("expected fallback to overall tail, got %q", got)
	}
	// Out-of-range from values are clamped.
	if got := r.tailSince(-5, 100); got != "0123456789" {
		t.Errorf("expected clamp of negative from, got %q", got)
	}
	if got := r.tailSince(99, 100); got != "0123456789" {
		t.Errorf("expected clamp of from beyond buffer, got %q", got)
	}
}
