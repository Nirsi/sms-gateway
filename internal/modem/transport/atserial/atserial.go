// Package atserial provides a reusable AT-over-serial transport for modem
// drivers. It handles port open/close, a short per-read poll loop, and the
// request/response primitives (SendAT, ReadUntil) needed by AT-based modems.
//
// Drivers typically embed or hold a *ATSerial and implement vendor-specific
// command sequences on top of it.
package atserial

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.bug.st/serial"
)

// idleReadSleep is the delay between poll iterations when the port returns
// zero bytes without blocking. Keeps ReadUntil from pinning a CPU on serial
// drivers that don't honor SetReadTimeout.
const idleReadSleep = 10 * time.Millisecond

// ATSerial is a serial-port-backed AT command transport. It serializes
// access with an internal channel semaphore so concurrent callers cannot
// interleave operations on the same physical port.
type ATSerial struct {
	portName string
	baudRate int
	sem      chan struct{}
}

// New returns an ATSerial bound to the given port name and baud rate.
func New(portName string, baudRate int) *ATSerial {
	return &ATSerial{
		portName: portName,
		baudRate: baudRate,
		sem:      make(chan struct{}, 1),
	}
}

// PortName returns the configured serial port name.
func (t *ATSerial) PortName() string { return t.portName }

// BaudRate returns the configured baud rate.
func (t *ATSerial) BaudRate() int { return t.baudRate }

// WithPort opens the serial port, invokes fn with it, and closes it when fn
// returns. Concurrent calls are serialized by an internal semaphore. The
// context is honored while waiting for the slot and then propagated into the
// callback.
func (t *ATSerial) WithPort(ctx context.Context, fn func(serial.Port) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case t.sem <- struct{}{}:
	}
	defer func() { <-t.sem }()

	if err := ctx.Err(); err != nil {
		return err
	}

	port, err := t.open()
	if err != nil {
		return err
	}
	defer port.Close()
	if err := ctx.Err(); err != nil {
		return err
	}

	return fn(port)
}

// open opens the serial port with 8N1 at the configured baud, and sets a
// short read timeout so polling loops don't block indefinitely.
func (t *ATSerial) open() (serial.Port, error) {
	mode := &serial.Mode{
		BaudRate: t.baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
	port, err := serial.Open(t.portName, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port %s: %w", t.portName, err)
	}
	if err := port.SetReadTimeout(200 * time.Millisecond); err != nil {
		port.Close()
		return nil, fmt.Errorf("failed to set read timeout: %w", err)
	}
	return port, nil
}

// ReadUntil reads from port in a polling loop until the accumulated response
// contains one of the terminal markers or the deadline is reached. It returns
// whatever has been read so far regardless. If port.Read returns an error
// other than a zero-byte timeout, that error is surfaced. On zero-byte reads
// the loop sleeps briefly to avoid busy-looping when the underlying driver
// does not honor SetReadTimeout.
func ReadUntil(ctx context.Context, port serial.Port, markers []string, deadline time.Duration) (string, error) {
	buf := make([]byte, 4096)
	var response strings.Builder
	cutoff := time.Now().Add(deadline)

	for time.Now().Before(cutoff) {
		if err := ctx.Err(); err != nil {
			return response.String(), err
		}
		n, err := port.Read(buf)
		if n > 0 {
			response.Write(buf[:n])
			resp := response.String()
			for _, marker := range markers {
				if strings.Contains(resp, marker) {
					return resp, nil
				}
			}
		}
		if err != nil {
			return response.String(), fmt.Errorf("serial read error: %w", err)
		}
		if n == 0 {
			// Port returned without data; pause so we don't spin if
			// SetReadTimeout isn't honored by the driver.
			select {
			case <-ctx.Done():
				return response.String(), ctx.Err()
			case <-time.After(idleReadSleep):
			}
		}
	}
	return response.String(), nil
}

// SendAT writes cmd (followed by CR) to the port and waits up to timeout for
// a terminal "OK" or "ERROR" marker in the response.
func SendAT(ctx context.Context, port serial.Port, cmd string, timeout time.Duration) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if _, err := port.Write([]byte(cmd + "\r")); err != nil {
		return "", fmt.Errorf("failed to write command %q: %w", cmd, err)
	}
	// Brief pause lets the modem start emitting bytes before we poll.
	timer := time.NewTimer(100 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-timer.C:
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return ReadUntil(ctx, port, []string{"OK", "ERROR"}, timeout)
}

// ParseSimpleResponse extracts the first non-empty, non-echo, non-OK line
// from an AT response — useful for things like AT+CGMI, AT+CGMM.
func ParseSimpleResponse(resp string) string {
	for _, line := range strings.Split(resp, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "OK" || strings.HasPrefix(line, "AT") {
			continue
		}
		return line
	}
	return ""
}
