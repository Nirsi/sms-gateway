// Package simulator provides a virtual modem driver that logs SMS sends
// instead of talking to hardware. It's useful for development and tests.
//
// Registered as driver name "simulator".
package simulator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"

	"sms-gateway/internal/modem"
)

func init() {
	modem.Register("simulator", New)
}

// Driver is a virtual modem. It requires no hardware.
type Driver struct {
	mu     sync.Mutex
	msgRef int
}

// New is the registered factory.
func New(_ modem.Options) (modem.Modem, error) {
	return &Driver{}, nil
}

// Open is a no-op for the simulator.
func (d *Driver) Open(_ context.Context) error { return nil }

// Close is a no-op for the simulator.
func (d *Driver) Close() error { return nil }

// GetStatus returns a hardcoded "connected" status.
func (d *Driver) GetStatus(_ context.Context) (*modem.Status, error) {
	return &modem.Status{
		Connected:          true,
		NetworkRegistered:  true,
		SignalStrength:     31,
		SignalStrengthDesc: "excellent",
		Manufacturer:       "Simulator",
		Model:              "Virtual Modem",
	}, nil
}

// SendSMS logs the message and returns success immediately.
func (d *Driver) SendSMS(ctx context.Context, phoneNumber, message string) (*modem.SendResult, error) {
	if phoneNumber == "" {
		return &modem.SendResult{}, fmt.Errorf("phone number is required")
	}
	if message == "" {
		return &modem.SendResult{}, fmt.Errorf("message is required")
	}

	if err := ctx.Err(); err != nil {
		return &modem.SendResult{}, err
	}

	d.mu.Lock()
	d.msgRef++
	ref := d.msgRef
	d.mu.Unlock()

	log.Printf("[SIMULATOR] SMS to %s (%d chars)", phoneNumber, len(message))
	log.Printf("[SIMULATOR] preview: %s", previewMessage(message))

	return &modem.SendResult{
		Success:          true,
		MessageReference: fmt.Sprintf("%d", ref),
	}, nil
}

func previewMessage(message string) string {
	const maxPreview = 80
	if len(message) <= maxPreview {
		return message
	}
	return strings.TrimSpace(message[:maxPreview]) + "..."
}
