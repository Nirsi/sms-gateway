package modem

import (
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"
)

// SimulatorModem is a virtual modem that simulates SMS sending by logging
// messages to the console. It requires no hardware and is useful for
// development and testing.
type SimulatorModem struct {
	mu     sync.Mutex
	msgRef int
}

// NewSimulator creates a new SimulatorModem instance.
func NewSimulator() *SimulatorModem {
	return &SimulatorModem{}
}

// GetStatus returns a hardcoded "connected" status for the simulated modem.
func (m *SimulatorModem) GetStatus() (*Status, error) {
	return &Status{
		Connected:          true,
		NetworkRegistered:  true,
		SignalStrength:     31,
		SignalStrengthDesc: "excellent",
		Manufacturer:       "Simulator",
		Model:              "Virtual Modem",
	}, nil
}

// SendSMS simulates sending an SMS by printing the message to the console.
// It introduces a small random delay (500ms–1s) to mimic real modem latency.
func (m *SimulatorModem) SendSMS(phoneNumber, message string) (*SendResult, error) {
	if phoneNumber == "" {
		return &SendResult{}, fmt.Errorf("phone number is required")
	}
	if message == "" {
		return &SendResult{}, fmt.Errorf("message is required")
	}

	// Simulate modem latency
	delay := 500 + rand.Intn(501) // 500–1000 ms
	time.Sleep(time.Duration(delay) * time.Millisecond)

	m.mu.Lock()
	m.msgRef++
	ref := m.msgRef
	m.mu.Unlock()

	log.Printf("[SIMULATOR] SMS to %s:", phoneNumber)
	log.Printf("[SIMULATOR] %s", message)

	return &SendResult{
		Success:          true,
		MessageReference: fmt.Sprintf("%d", ref),
	}, nil
}
