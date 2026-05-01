package simulator

import (
	"context"
	"errors"
	"testing"

	"sms-gateway/internal/modem"
)

func TestNewReturnsDriver(t *testing.T) {
	m, err := New(modem.Options{})
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if _, ok := m.(*Driver); !ok {
		t.Fatalf("New returned %T, want *Driver", m)
	}
}

func TestOpenCloseAreNoops(t *testing.T) {
	d := &Driver{}
	if err := d.Open(context.Background()); err != nil {
		t.Fatalf("Open returned error: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close returned error: %v", err)
	}
}

func TestGetStatus(t *testing.T) {
	d := &Driver{}
	status, err := d.GetStatus(context.Background())
	if err != nil {
		t.Fatalf("GetStatus returned error: %v", err)
	}
	if status == nil {
		t.Fatal("GetStatus returned nil status")
	}
	if !status.Connected || !status.NetworkRegistered {
		t.Fatalf("status connected=%t registered=%t, want both true", status.Connected, status.NetworkRegistered)
	}
	if status.Manufacturer != "Simulator" || status.Model != "Virtual Modem" {
		t.Fatalf("status manufacturer/model = %q/%q", status.Manufacturer, status.Model)
	}
}

func TestSendSMSValidation(t *testing.T) {
	d := &Driver{}
	if _, err := d.SendSMS(context.Background(), "", "hello"); err == nil {
		t.Fatal("SendSMS accepted empty phone number")
	}
	if _, err := d.SendSMS(context.Background(), "+420123456789", ""); err == nil {
		t.Fatal("SendSMS accepted empty message")
	}
}

func TestSendSMSReturnsContextError(t *testing.T) {
	d := &Driver{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := d.SendSMS(ctx, "+420123456789", "hello")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("SendSMS error = %v, want context canceled", err)
	}
}

func TestSendSMSSuccessIncrementsMessageReference(t *testing.T) {
	d := &Driver{}

	first, err := d.SendSMS(context.Background(), "+420123456789", "hello")
	if err != nil {
		t.Fatalf("first SendSMS returned error: %v", err)
	}
	second, err := d.SendSMS(context.Background(), "+420123456789", "world")
	if err != nil {
		t.Fatalf("second SendSMS returned error: %v", err)
	}

	if !first.Success || first.MessageReference != "1" {
		t.Fatalf("first result = %+v, want success ref 1", first)
	}
	if !second.Success || second.MessageReference != "2" {
		t.Fatalf("second result = %+v, want success ref 2", second)
	}
}

func TestPreviewMessage(t *testing.T) {
	short := "short message"
	if got := previewMessage(short); got != short {
		t.Fatalf("previewMessage(short) = %q, want %q", got, short)
	}

	long := "1234567890" + "1234567890" + "1234567890" + "1234567890" + "1234567890" + "1234567890" + "1234567890" + "1234567890" + " extra"
	if got := previewMessage(long); got != "12345678901234567890123456789012345678901234567890123456789012345678901234567890..." {
		t.Fatalf("previewMessage(long) = %q", got)
	}
}
