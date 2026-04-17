package atserial

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.bug.st/serial"
)

type blockingPort struct {
	readDelay time.Duration
	reads     int
	writes    [][]byte
}

func (p *blockingPort) SetMode(*serial.Mode) error { return nil }

func (p *blockingPort) Read(buf []byte) (int, error) {
	p.reads++
	time.Sleep(p.readDelay)
	return 0, nil
}

func (p *blockingPort) Write(buf []byte) (int, error) {
	cp := make([]byte, len(buf))
	copy(cp, buf)
	p.writes = append(p.writes, cp)
	return len(buf), nil
}

func (p *blockingPort) Drain() error { return nil }

func (p *blockingPort) ResetInputBuffer() error { return nil }

func (p *blockingPort) ResetOutputBuffer() error { return nil }

func (p *blockingPort) SetDTR(bool) error { return nil }

func (p *blockingPort) SetRTS(bool) error { return nil }

func (p *blockingPort) GetModemStatusBits() (*serial.ModemStatusBits, error) { return nil, nil }

func (p *blockingPort) SetReadTimeout(time.Duration) error { return nil }

func (p *blockingPort) Close() error { return nil }

func (p *blockingPort) Break(time.Duration) error { return nil }

func TestReadUntilReturnsSoonAfterContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	port := &blockingPort{readDelay: 20 * time.Millisecond}

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	resp, err := ReadUntil(ctx, port, []string{"OK"}, time.Second)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadUntil error = %v, want context canceled", err)
	}
	if resp != "" {
		t.Fatalf("ReadUntil response = %q, want empty", resp)
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("ReadUntil took %v after cancellation, want <= 80ms", elapsed)
	}
	if port.reads == 0 {
		t.Fatal("ReadUntil did not attempt any reads")
	}
}

func TestSendATReturnsContextErrorBeforePolling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	port := &blockingPort{}

	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	resp, err := SendAT(ctx, port, "AT", time.Second)
	if err == nil || err != context.Canceled {
		t.Fatalf("SendAT error = %v, want context canceled", err)
	}
	if resp != "" {
		t.Fatalf("SendAT response = %q, want empty", resp)
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("SendAT took %v after cancellation, want <= 80ms", elapsed)
	}
	if len(port.writes) != 1 || string(port.writes[0]) != "AT\r" {
		t.Fatalf("SendAT writes = %q, want [\"AT\\r\"]", port.writes)
	}
}

func TestWithPortReturnsContextErrorWhileWaitingForLock(t *testing.T) {
	tx := New("", 0)
	// Pre-acquire the semaphore so WithPort has to wait.
	tx.sem <- struct{}{}
	defer func() { <-tx.sem }()

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := tx.WithPort(ctx, func(serial.Port) error {
		t.Fatal("callback should not run")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("WithPort error = %v, want context canceled", err)
	}
	if elapsed := time.Since(start); elapsed > 80*time.Millisecond {
		t.Fatalf("WithPort took %v after cancellation, want <= 80ms", elapsed)
	}
}
