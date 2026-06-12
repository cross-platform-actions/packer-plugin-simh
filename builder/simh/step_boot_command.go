package simh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/hashicorp/packer-plugin-sdk/bootcommand"
	"github.com/hashicorp/packer-plugin-sdk/multistep"
	"github.com/hashicorp/packer-plugin-sdk/packer"
	"github.com/hashicorp/packer-plugin-sdk/template/interpolate"
)

type stepBootCommand struct {
	conn net.Conn
	// reader continuously drains the console for the whole build (and mirrors
	// it to the tee file, which it owns and closes); nil until the console
	// connection is established.
	reader *consoleReader
}

// consoleReader owns the simulated console connection for the lifetime of the
// build. A single goroutine continuously drains the connection, appending
// IAC-stripped bytes to a buffer (while boot steps still need it) and
// mirroring them to the tee file. It also OWNS writes: telnetDriver delegates
// to Write so that keystrokes always go to the live connection, even after a
// transparent reconnect.
//
// Expect matching is windowed: callers record an offset with pos() and waitFor
// only searches output that arrived at or after that offset, so text painted
// on earlier screens (repeated password prompts, menus rendered before a
// detour) can never satisfy a later step's expect — only fresh bytes can.
//
// Reading must never pause: SIMH only accepts one telnet client, so the tee
// file is the operator's only window into the console, and boot steps with
// empty expect patterns (timed sends) would otherwise leave the socket
// undrained — the tee freezes at the last expect match and the operator can't
// tell a healthy guest from a wedged one.
//
// If the connection drops (e.g. a post_boot_command like RESET -p ALL closes
// it), the reader dials a fresh connection and resumes, and subsequent writes
// follow the new connection automatically. All read errors are swallowed:
// losing the console stream is never by itself a reason to fail a build — a
// boot step waiting on an expect will time out (or fail fast if SIMH exits).
type consoleReader struct {
	addr         string
	tee          io.Writer
	vmDone       <-chan struct{} // closed when SIMH exits; stops reconnects and waits
	cancel       <-chan struct{} // closed when the build is cancelled; aborts writes/reconnects
	writeTimeout time.Duration   // how long Write waits for a (re)connection; 0 = fail fast

	stop     chan struct{}
	stopOnce sync.Once
	done     chan struct{} // closed when run() returns; nil for test-only readers

	mu        sync.Mutex
	buf       bytes.Buffer
	buffering bool     // retain bytes in buf for expect matching (boot phase only)
	conn      net.Conn // current connection; guarded by mu so writers follow reconnects
}

func newConsoleReader(conn net.Conn, addr string, tee io.Writer, vmDone, cancel <-chan struct{}) *consoleReader {
	r := &consoleReader{
		addr:         addr,
		tee:          tee,
		vmDone:       vmDone,
		cancel:       cancel,
		writeTimeout: 5 * time.Second,
		stop:         make(chan struct{}),
		done:         make(chan struct{}),
		buffering:    true,
		conn:         conn,
	}
	go r.run()
	return r
}

// run drains the connection until Stop is called. Ownership of the connection
// (including reconnection and closing) belongs to run.
func (r *consoleReader) run() {
	defer close(r.done)

	// Cancel reconnect attempts when stop is requested, SIMH exits, or the
	// build is cancelled.
	ctx, cancel := mergeDone(r.stop, r.vmDone, r.cancel)
	defer cancel()

	defer func() {
		r.mu.Lock()
		if r.conn != nil {
			_ = r.conn.Close()
			r.conn = nil
		}
		r.mu.Unlock()
		// The reader owns the tee file; close it once the goroutine (its
		// only writer) has stopped, so no write can race the close.
		if c, ok := r.tee.(io.Closer); ok {
			_ = c.Close()
		}
	}()

	readBuf := make([]byte, 4096)
	var pending []byte // trailing incomplete IAC sequence carried across reads

	for {
		select {
		case <-r.stop:
			return
		default:
		}

		r.mu.Lock()
		c := r.conn
		r.mu.Unlock()

		if c == nil {
			// Reconnect, retrying forever until stop/SIMH-exit cancels ctx.
			nc, err := dialWithBackoff(ctx, r.addr, 0)
			if err != nil {
				return
			}
			r.mu.Lock()
			r.conn = nc
			r.mu.Unlock()
			c = nc
		}

		// Cap read with a deadline so we periodically re-check stop.
		_ = c.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, err := c.Read(readBuf)
		if n > 0 {
			chunk := readBuf[:n]
			if len(pending) > 0 {
				chunk = append(pending, chunk...)
			}
			var cleaned []byte
			cleaned, pending = stripTelnetIAC(chunk)
			r.mu.Lock()
			if r.buffering {
				r.buf.Write(cleaned)
			}
			r.mu.Unlock()
			if r.tee != nil {
				// Best-effort: ignore write errors so the tee file
				// going away never breaks the build.
				_, _ = r.tee.Write(cleaned)
			}
		}
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			r.mu.Lock()
			if r.conn == c {
				_ = r.conn.Close()
				r.conn = nil
			}
			r.mu.Unlock()
			// A reconnect starts a fresh telnet stream, so drop any partial
			// IAC sequence carried from the now-dead connection — otherwise
			// it would corrupt the leading bytes of the new stream.
			pending = nil
		}
	}
}

// Write sends bytes to the current console connection. telnetDriver delegates
// here so that keystrokes always reach the live connection, even after the
// reader has transparently reconnected.
//
// Surviving a transient console drop is the whole point of the reader, so a
// send that races a reconnect is retried on the new connection rather than
// failing the build. The retry is bounded by writeTimeout (0 = fail fast, used
// by tests) and aborts early if the reader stops or SIMH exits.
func (r *consoleReader) Write(p []byte) (int, error) {
	deadline := time.Now().Add(r.writeTimeout)
	for {
		r.mu.Lock()
		c := r.conn
		r.mu.Unlock()

		if c != nil {
			n, err := c.Write(p)
			if err == nil {
				return n, nil
			}
			if n > 0 {
				// Partial write — retrying would duplicate bytes.
				return n, fmt.Errorf("error writing to console: %s", err)
			}
			// n == 0: the reader likely closed this connection on a drop;
			// wait for the reconnect and retry on the fresh connection.
		}

		if !time.Now().Before(deadline) {
			return 0, fmt.Errorf("console connection unavailable after %s", r.writeTimeout)
		}

		select {
		case <-r.stop:
			return 0, fmt.Errorf("console reader stopped")
		case <-r.vmDone:
			return 0, fmt.Errorf("SIMH exited before console write could complete")
		case <-r.cancel:
			return 0, fmt.Errorf("build cancelled before console write could complete")
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// Stop terminates the reader goroutine (closing the connection it holds) and
// blocks until it has actually exited, so callers can safely close the tee
// file and remove the output directory afterwards. It is idempotent.
func (r *consoleReader) Stop() {
	r.stopOnce.Do(func() { close(r.stop) })
	if r.done != nil {
		<-r.done
	}
}

// stopBuffering releases the expect-match buffer once boot steps are done. The
// reader keeps draining the console (and mirroring to the tee) for the rest of
// the build, but nothing reads the buffer after this, so retaining it would
// only grow memory without bound.
func (r *consoleReader) stopBuffering() {
	r.mu.Lock()
	r.buffering = false
	r.buf.Reset()
	r.mu.Unlock()
}

// pos returns the current length of the console buffer. Callers use it to mark
// a point in the output stream: a later waitFor with from=pos matches only
// output that arrived after the mark. The buffer only grows during the boot
// phase, so a recorded pos stays valid for that phase.
func (r *consoleReader) pos() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Len()
}

// containsFrom reports whether console output received at or after byte offset
// from contains the given substring.
func (r *consoleReader) containsFrom(s string, from int) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.buf.Bytes()
	from = clampOffset(from, len(b))
	return bytes.Contains(b[from:], []byte(s))
}

// tailSince returns up to the last n bytes of console output received at or
// after byte offset from. If nothing has arrived since from, it falls back to
// the overall tail so a timeout error always shows something useful.
func (r *consoleReader) tailSince(from, n int) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	b := r.buf.Bytes()
	from = clampOffset(from, len(b))
	s := b[from:]
	if len(s) == 0 {
		s = b
	}
	if len(s) > n {
		s = s[len(s)-n:]
	}
	return string(s)
}

// waitFor blocks until console output received at or after byte offset from
// contains expect, the timeout expires, the context is cancelled, or SIMH
// exits. Output that arrived before from is ignored, so an anchor string that
// already appeared on an earlier screen cannot satisfy the match — only fresh
// bytes can.
func (r *consoleReader) waitFor(ctx context.Context, expect string, from int, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for {
		if r.containsFrom(expect, from) {
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("timeout waiting for pattern %q (console output since last send: %q)",
				expect, r.tailSince(from, 500))
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("build cancelled while waiting for pattern %q", expect)
		case <-r.vmDone:
			// SIMH exited. Its final console output may still be buffered in
			// the socket and not yet drained by the reader goroutine, so the
			// awaited text could be in that last burst. Give the reader a
			// brief grace period to catch up before declaring failure.
			graceDeadline := time.Now().Add(2 * time.Second)
			for {
				if r.containsFrom(expect, from) {
					return nil
				}
				if time.Now().After(graceDeadline) {
					return fmt.Errorf("SIMH exited while waiting for pattern %q (console output since last send: %q)",
						expect, r.tailSince(from, 500))
				}
				select {
				case <-ctx.Done():
					return fmt.Errorf("build cancelled while waiting for pattern %q", expect)
				case <-time.After(50 * time.Millisecond):
				}
			}
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// mergeDone returns a context that is cancelled when any of the provided
// channels is closed (nil channels are ignored). Callers must invoke the
// returned cancel func to release the watcher goroutines.
func mergeDone(chans ...<-chan struct{}) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	for _, ch := range chans {
		if ch == nil {
			continue
		}
		go func(ch <-chan struct{}) {
			select {
			case <-ch:
				cancel()
			case <-ctx.Done():
			}
		}(ch)
	}
	return ctx, cancel
}

// dialWithBackoff dials addr over TCP, retrying with exponential backoff until
// it connects, ctx is cancelled, or overallTimeout elapses (0 = retry
// forever). It is used both for the initial console connection and for
// reconnects, so the backoff policy lives in one place.
func dialWithBackoff(ctx context.Context, addr string, overallTimeout time.Duration) (net.Conn, error) {
	var deadline time.Time
	if overallTimeout > 0 {
		deadline = time.Now().Add(overallTimeout)
	}
	dialer := net.Dialer{Timeout: 5 * time.Second}
	backoff := 500 * time.Millisecond
	for {
		// DialContext honours ctx, so a cancelled ctx (stop requested or SIMH
		// exited) aborts an in-flight dial promptly instead of blocking for
		// the full per-attempt timeout.
		conn, err := dialer.DialContext(ctx, "tcp", addr)
		if err == nil {
			// If ctx was cancelled at almost the same time the dial
			// succeeded, don't hand back a live connection nobody will use.
			if ctx.Err() != nil {
				_ = conn.Close()
				return nil, ctx.Err()
			}
			return conn, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !deadline.IsZero() && time.Now().After(deadline) {
			return nil, fmt.Errorf("could not connect to %s within %s: %s", addr, overallTimeout, err)
		}
		log.Printf("[DEBUG] Failed to connect to console at %s: %s, retrying...", addr, err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(backoff):
			backoff *= 2
			if backoff > 5*time.Second {
				backoff = 5 * time.Second
			}
		}
	}
}

// clampOffset clamps a byte offset to the range [0, length].
func clampOffset(from, length int) int {
	if from < 0 {
		return 0
	}
	if from > length {
		return length
	}
	return from
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
	conn, err := dialWithBackoff(ctx, addr, 30*time.Second)
	if err != nil {
		if ctx.Err() != nil {
			return multistep.ActionHalt
		}
		// dialWithBackoff already formats a descriptive "could not connect to
		// <addr> within <timeout>" message; surface it directly.
		state.Put("error", err)
		ui.Error(err.Error())
		return multistep.ActionHalt
	}

	s.conn = conn
	ui.Say(fmt.Sprintf("Connected to console at %s", addr))

	// Mirror everything we read from the simulated console to a tee file so
	// the operator can `tail -f` it during the build (SIMH itself can't
	// expose the telnet stream to a second client). Failure to open the
	// file is non-fatal — boot_steps still work without it.
	// The reader owns and closes this file; we only open it here. Assign it
	// to the io.Writer only on success so we never box a nil *os.File into a
	// (non-nil) interface, which would defeat the reader's tee != nil guard.
	var tee io.Writer
	teePath := filepath.Join(config.OutputDirectory, config.VMName+".console.tee.log")
	if f, err := os.OpenFile(teePath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0644); err == nil {
		tee = f
		ui.Say(fmt.Sprintf("Mirroring console to %s (tail -f to follow)", teePath))
	} else {
		log.Printf("[WARN] could not open console tee file %s: %s", teePath, err)
	}

	// Wire SIMH's process-exit signal so waits fail fast and reconnects stop
	// once SIMH is gone. Optional: a driver that doesn't expose Done() simply
	// loses fast-fail.
	var vmDone <-chan struct{}
	if raw, ok := state.GetOk("driver"); ok {
		if d, ok := raw.(interface{ Done() <-chan struct{} }); ok {
			vmDone = d.Done()
		}
	}

	// Start the console reader. It owns both reads and writes on conn
	// (including reconnects and closing) from here on.
	s.reader = newConsoleReader(conn, addr, tee, vmDone, ctx.Done())
	// Make the reader reachable from the builder's cleanup backstop, which
	// runs even when the SDK's abortStep skips this step's Cleanup.
	state.Put("console_reader", s.reader)

	// Populate template data.
	td := populateTemplateData(config, state)
	config.ctx.Data = &td

	// Create telnet driver. It writes through the reader so keystrokes follow
	// any reconnection the reader performs.
	driver := &telnetDriver{
		w:        s.reader,
		interval: config.BootKeyInterval,
	}

	// Process each boot step.
	//
	// mark is the buffer offset where the current step's expect window
	// begins: each step's expect matches only console output that
	// arrived after the previous step's send began. This makes
	// repeated anchor text (password prompts, re-painted menus) safe —
	// a stale occurrence from an earlier screen can't trigger the
	// match.
	mark := 0
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

		// Wait for expect pattern in output received since the
		// previous step's send began. Empty expect = timed send,
		// no waiting.
		if renderedExpect != "" {
			if err := s.reader.waitFor(ctx, renderedExpect, mark, config.BootStepTimeout); err != nil {
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

		// Capture the next step's window mark BEFORE executing the
		// send: if the guest echoes our typing and paints the next
		// prompt while we're still keying the final characters (the
		// inter-key interval gives it time to), that prompt must
		// still land inside the next step's expect window. The echo
		// of our own typing lands in the window too; that's
		// acceptable — anchors are screen labels the guest paints,
		// not text we type.
		mark = s.reader.pos()

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

	// Stop retaining console output for expect matching; the reader keeps
	// draining and mirroring to the tee file for the rest of the build —
	// that's where most "stuck after boot_steps" problems show up.
	s.reader.stopBuffering()

	// Ownership of conn already passed to the reader; clear our copy so
	// Cleanup doesn't double-close it.
	s.conn = nil

	return multistep.ActionContinue
}

func (s *stepBootCommand) Cleanup(state multistep.StateBag) {
	if s.reader != nil {
		// Stop blocks until the reader goroutine has exited; the reader closes
		// both its connection and the tee file it owns.
		s.reader.Stop()
		s.reader = nil
		s.conn = nil
		return
	}
	if s.conn != nil {
		_ = s.conn.Close()
		s.conn = nil
	}
}

// stripTelnetIAC removes complete telnet IAC (Interpret As Command) sequences
// from data. It returns the cleaned bytes plus any trailing bytes that form an
// incomplete IAC sequence; the caller must prepend that leftover to the next
// chunk so sequences split across read boundaries are not corrupted.
func stripTelnetIAC(data []byte) (cleaned, leftover []byte) {
	var result []byte
	i := 0
	for i < len(data) {
		if data[i] == 0xFF { // IAC
			if i+1 >= len(data) {
				// Incomplete: just the IAC byte so far.
				return result, append([]byte(nil), data[i:]...)
			}
			switch data[i+1] {
			case 0xFF: // Escaped 0xFF -> literal 0xFF
				result = append(result, 0xFF)
				i += 2
			case 0xFB, 0xFC, 0xFD, 0xFE: // WILL, WONT, DO, DONT (3-byte)
				if i+2 >= len(data) {
					// Option byte not yet read.
					return result, append([]byte(nil), data[i:]...)
				}
				i += 3
			default:
				// Other IAC command (2 bytes).
				i += 2
			}
		} else {
			result = append(result, data[i])
			i++
		}
	}
	return result, nil
}
