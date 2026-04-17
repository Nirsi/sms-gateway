// Package modem defines the abstract modem interface used by the SMS gateway
// and a driver registry that concrete modem implementations can register with.
//
// Concrete drivers live in subpackages under internal/modem/drivers/. They
// register themselves in their init() via Register, and are selected at runtime
// by name (see Open and the -driver flag).
package modem

import "context"

// Status represents the current state of the modem. It is also used directly
// as a JSON response body by the HTTP layer, so changes here are observable
// to API clients.
type Status struct {
	Connected          bool   `json:"connected"`
	NetworkRegistered  bool   `json:"network_registered"`
	SignalStrength     int    `json:"signal_strength"`
	SignalStrengthDesc string `json:"signal_strength_desc"`
	Manufacturer       string `json:"manufacturer,omitempty"`
	Model              string `json:"model,omitempty"`
}

// SendResult represents the result of an SMS send operation. It is also used
// directly as a JSON field on queue jobs.
type SendResult struct {
	Success          bool   `json:"success"`
	MessageReference string `json:"message_reference,omitempty"`
	Error            string `json:"error,omitempty"`
}

// Modem is the abstract interface every driver must implement.
//
// Lifecycle:
//
//	Open is called once at startup. Drivers may use it to establish a
//	persistent connection, warm up the device, or perform a startup ping.
//	Drivers that open/close the serial port per call can simply return nil.
//
//	Close is called once at shutdown. Drivers should release any held
//	resources. It must be safe to call even if Open failed.
//
// Operations (GetStatus, SendSMS) may be called concurrently from multiple
// goroutines; drivers are responsible for serializing access to shared
// hardware resources. The provided context is honored for cancellation and
// timeouts where possible.
type Modem interface {
	Open(ctx context.Context) error
	Close() error
	GetStatus(ctx context.Context) (*Status, error)
	SendSMS(ctx context.Context, phoneNumber, message string) (*SendResult, error)
}
