package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"sms-gateway/internal/modem"
)

type stubModem struct {
	open func(context.Context) error
}

func (m stubModem) Open(ctx context.Context) error {
	if m.open != nil {
		return m.open(ctx)
	}
	return nil
}

func (stubModem) Close() error { return nil }

func (stubModem) GetStatus(context.Context) (*modem.Status, error) {
	return &modem.Status{}, nil
}

func (stubModem) SendSMS(context.Context, string, string) (*modem.SendResult, error) {
	return &modem.SendResult{}, nil
}

func TestOpenModemReturnsDriverError(t *testing.T) {
	expected := errors.New("boom")
	err := openModem(stubModem{open: func(context.Context) error {
		return expected
	}}, time.Second)
	if !errors.Is(err, expected) {
		t.Fatalf("openModem error = %v, want %v", err, expected)
	}
}

func TestOpenModemCancelsContextAfterTimeout(t *testing.T) {
	err := openModem(stubModem{open: func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	}}, 10*time.Millisecond)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("openModem error = %v, want context deadline exceeded", err)
	}
}
