package modem

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.bug.st/serial"
)

// Status represents the current state of the modem.
type Status struct {
	Connected          bool   `json:"connected"`
	NetworkRegistered  bool   `json:"network_registered"`
	SignalStrength     int    `json:"signal_strength"`
	SignalStrengthDesc string `json:"signal_strength_desc"`
	Manufacturer       string `json:"manufacturer,omitempty"`
	Model              string `json:"model,omitempty"`
}

// SendResult represents the result of an SMS send operation.
type SendResult struct {
	Success          bool   `json:"success"`
	MessageReference string `json:"message_reference,omitempty"`
	Error            string `json:"error,omitempty"`
}

// Modem defines the interface for modem operations.
type Modem interface {
	GetStatus() (*Status, error)
	SendSMS(phoneNumber, message string) (*SendResult, error)
}

// GSMModem manages serial port communication with a real GSM modem.
type GSMModem struct {
	portName string
	baudRate int
	mu       sync.Mutex
}

// New creates a new GSMModem instance.
func New(portName string, baudRate int) *GSMModem {
	return &GSMModem{
		portName: portName,
		baudRate: baudRate,
	}
}

// openPort opens the serial port with the configured settings.
func (m *GSMModem) openPort() (serial.Port, error) {
	mode := &serial.Mode{
		BaudRate: m.baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}

	port, err := serial.Open(m.portName, mode)
	if err != nil {
		return nil, fmt.Errorf("failed to open serial port %s: %w", m.portName, err)
	}

	// Use a short read timeout so we can poll without blocking for ages.
	if err := port.SetReadTimeout(200 * time.Millisecond); err != nil {
		port.Close()
		return nil, fmt.Errorf("failed to set read timeout: %w", err)
	}

	return port, nil
}

// readUntil reads from the port in a polling loop until the accumulated
// response contains one of the terminal markers or the deadline is reached.
func (m *GSMModem) readUntil(port serial.Port, markers []string, deadline time.Duration) string {
	buf := make([]byte, 4096)
	var response strings.Builder
	cutoff := time.Now().Add(deadline)

	for time.Now().Before(cutoff) {
		n, _ := port.Read(buf)
		if n > 0 {
			response.Write(buf[:n])
			resp := response.String()
			for _, marker := range markers {
				if strings.Contains(resp, marker) {
					return resp
				}
			}
		}
	}

	return response.String()
}

// sendAT sends an AT command and waits for a complete response (OK or ERROR).
func (m *GSMModem) sendAT(port serial.Port, cmd string) (string, error) {
	_, err := port.Write([]byte(cmd + "\r"))
	if err != nil {
		return "", fmt.Errorf("failed to write command %q: %w", cmd, err)
	}

	// Give the modem a moment to start processing before we poll.
	time.Sleep(100 * time.Millisecond)

	resp := m.readUntil(port, []string{"OK", "ERROR"}, 3*time.Second)
	return resp, nil
}

// GetStatus checks the modem status including connectivity, network registration, and signal.
func (m *GSMModem) GetStatus() (*Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	status := &Status{}

	port, err := m.openPort()
	if err != nil {
		return status, fmt.Errorf("modem not accessible: %w", err)
	}
	defer port.Close()

	// Test basic communication
	resp, err := m.sendAT(port, "AT")
	if err != nil || !strings.Contains(resp, "OK") {
		return status, fmt.Errorf("modem not responding")
	}
	status.Connected = true

	// Get manufacturer
	resp, err = m.sendAT(port, "AT+CGMI")
	if err == nil && strings.Contains(resp, "OK") {
		status.Manufacturer = parseSimpleResponse(resp)
	}

	// Get model
	resp, err = m.sendAT(port, "AT+CGMM")
	if err == nil && strings.Contains(resp, "OK") {
		status.Model = parseSimpleResponse(resp)
	}

	// Check network registration
	resp, err = m.sendAT(port, "AT+CREG?")
	if err == nil {
		re := regexp.MustCompile(`\+CREG: \d,(\d)`)
		if matches := re.FindStringSubmatch(resp); len(matches) > 1 {
			regStatus := matches[1]
			// 1 = registered home, 5 = registered roaming
			if regStatus == "1" || regStatus == "5" {
				status.NetworkRegistered = true
			}
		}
	}

	// Check signal quality
	resp, err = m.sendAT(port, "AT+CSQ")
	if err == nil {
		re := regexp.MustCompile(`\+CSQ: (\d+),`)
		if matches := re.FindStringSubmatch(resp); len(matches) > 1 {
			if val, err := strconv.Atoi(matches[1]); err == nil {
				status.SignalStrength = val
				switch {
				case val == 99:
					status.SignalStrengthDesc = "unknown"
				case val < 10:
					status.SignalStrengthDesc = "weak"
				case val < 20:
					status.SignalStrengthDesc = "fair"
				case val < 30:
					status.SignalStrengthDesc = "good"
				default:
					status.SignalStrengthDesc = "excellent"
				}
			}
		}
	}

	return status, nil
}

// SendSMS sends an SMS message to the specified phone number.
func (m *GSMModem) SendSMS(phoneNumber, message string) (*SendResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := &SendResult{}

	if phoneNumber == "" {
		return result, fmt.Errorf("phone number is required")
	}
	if message == "" {
		return result, fmt.Errorf("message is required")
	}

	port, err := m.openPort()
	if err != nil {
		result.Error = fmt.Sprintf("modem not accessible: %v", err)
		return result, fmt.Errorf("modem not accessible: %w", err)
	}
	defer port.Close()

	// Step 1: Test modem communication
	resp, err := m.sendAT(port, "AT")
	if err != nil || !strings.Contains(resp, "OK") {
		result.Error = "modem not responding"
		return result, fmt.Errorf("modem not responding")
	}

	// Step 2: Set SMS text mode
	resp, err = m.sendAT(port, "AT+CMGF=1")
	if err != nil || !strings.Contains(resp, "OK") {
		result.Error = "failed to set SMS text mode"
		return result, fmt.Errorf("failed to set SMS text mode")
	}

	// Step 3: Set character set to GSM
	_, _ = m.sendAT(port, `AT+CSCS="GSM"`)

	// Step 4: Set SMS parameters
	_, _ = m.sendAT(port, "AT+CSMP=17,167,0,0")

	// Step 5: Check network registration
	resp, err = m.sendAT(port, "AT+CREG?")
	if err == nil {
		re := regexp.MustCompile(`\+CREG: \d,(\d)`)
		if matches := re.FindStringSubmatch(resp); len(matches) > 1 {
			regStatus := matches[1]
			if regStatus != "1" && regStatus != "5" {
				result.Error = "not registered on network"
				return result, fmt.Errorf("not registered on network (status: %s)", regStatus)
			}
		}
	}

	// Step 6: Initiate SMS send — modem responds with "> " prompt
	_, err = port.Write([]byte(fmt.Sprintf("AT+CMGS=\"%s\"\r", phoneNumber)))
	if err != nil {
		result.Error = fmt.Sprintf("failed to initiate SMS: %v", err)
		return result, fmt.Errorf("failed to initiate SMS: %w", err)
	}

	// Wait for the "> " prompt before sending the body
	prompt := m.readUntil(port, []string{">", "ERROR"}, 3*time.Second)
	if strings.Contains(prompt, "ERROR") {
		result.Error = "modem rejected the send command"
		return result, fmt.Errorf("modem rejected AT+CMGS: %s", prompt)
	}

	// Step 7: Send message body followed by Ctrl+Z (ASCII 26)
	_, err = port.Write([]byte(message + string(rune(26))))
	if err != nil {
		result.Error = fmt.Sprintf("failed to write message body: %v", err)
		return result, fmt.Errorf("failed to write message body: %w", err)
	}

	// Poll for the final result — the modem sends +CMGS on success.
	// Allow up to 30 seconds for the network to accept the message.
	finalResp := m.readUntil(port, []string{"+CMGS:", "OK", "ERROR"}, 30*time.Second)

	// Check for success
	re := regexp.MustCompile(`\+CMGS: (\d+)`)
	if matches := re.FindStringSubmatch(finalResp); len(matches) > 1 {
		result.Success = true
		result.MessageReference = matches[1]
		return result, nil
	}

	if strings.Contains(finalResp, "OK") {
		result.Success = true
		return result, nil
	}

	if strings.Contains(finalResp, "ERROR") {
		result.Error = "modem returned an error"
		return result, fmt.Errorf("modem returned an error: %s", finalResp)
	}

	// Uncertain result
	result.Success = true
	result.Error = "uncertain result — check phone for delivery"
	return result, nil
}

// parseSimpleResponse extracts the first non-empty, non-command, non-OK line from an AT response.
func parseSimpleResponse(resp string) string {
	lines := strings.Split(resp, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "OK" || strings.HasPrefix(line, "AT") {
			continue
		}
		return line
	}
	return ""
}
