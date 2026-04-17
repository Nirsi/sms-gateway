// Package generic implements a vendor-agnostic GSM modem driver that speaks
// standard Hayes AT commands over a serial port in SMS text mode. It should
// work with most commodity GSM modems (Huawei, Quectel, SIM800, etc.) that
// implement +CMGF=1 text-mode SMS.
//
// Registered as driver name "generic".
package generic

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.bug.st/serial"

	"sms-gateway/internal/modem"
	"sms-gateway/internal/modem/transport/atserial"
)

func init() {
	modem.Register("generic", New)
}

// Driver is a generic AT-over-serial GSM modem.
type Driver struct {
	t *atserial.ATSerial
}

// New is the registered factory. It honors these Options.Extra keys:
//
//	(none yet — reserved for future use: pin, smsc, init, csmp, ...)
func New(opts modem.Options) (modem.Modem, error) {
	if opts.Port == "" {
		return nil, fmt.Errorf("generic: port is required")
	}
	baud := opts.BaudRate
	if baud <= 0 {
		baud = 115200
	}
	return &Driver{t: atserial.New(opts.Port, baud)}, nil
}

// Open performs a one-shot AT ping so startup fails fast if the modem is
// missing or wedged.
func (d *Driver) Open(ctx context.Context) error {
	return d.t.WithPort(ctx, func(port serial.Port) error {
		resp, err := atserial.SendAT(ctx, port, "AT", 3*time.Second)
		if err != nil {
			return fmt.Errorf("modem ping failed: %w", err)
		}
		if !strings.Contains(resp, "OK") {
			return fmt.Errorf("modem did not acknowledge AT ping")
		}
		return nil
	})
}

// Close is a no-op: the generic driver opens/closes the port per operation.
func (d *Driver) Close() error { return nil }

// GetStatus checks connectivity, manufacturer/model, network registration
// and signal strength.
func (d *Driver) GetStatus(ctx context.Context) (*modem.Status, error) {
	status := &modem.Status{}
	err := d.t.WithPort(ctx, func(port serial.Port) error {
		resp, err := atserial.SendAT(ctx, port, "AT", 3*time.Second)
		if err != nil || !strings.Contains(resp, "OK") {
			return fmt.Errorf("modem not responding")
		}
		status.Connected = true

		if resp, err := atserial.SendAT(ctx, port, "AT+CGMI", 3*time.Second); err == nil && strings.Contains(resp, "OK") {
			status.Manufacturer = atserial.ParseSimpleResponse(resp)
		}
		if resp, err := atserial.SendAT(ctx, port, "AT+CGMM", 3*time.Second); err == nil && strings.Contains(resp, "OK") {
			status.Model = atserial.ParseSimpleResponse(resp)
		}

		if resp, err := atserial.SendAT(ctx, port, "AT+CREG?", 3*time.Second); err == nil {
			re := regexp.MustCompile(`\+CREG: \d,(\d)`)
			if matches := re.FindStringSubmatch(resp); len(matches) > 1 {
				// 1 = registered home, 5 = registered roaming
				if matches[1] == "1" || matches[1] == "5" {
					status.NetworkRegistered = true
				}
			}
		}

		if resp, err := atserial.SendAT(ctx, port, "AT+CSQ", 3*time.Second); err == nil {
			re := regexp.MustCompile(`\+CSQ: (\d+),`)
			if matches := re.FindStringSubmatch(resp); len(matches) > 1 {
				if val, err := strconv.Atoi(matches[1]); err == nil {
					status.SignalStrength = val
					status.SignalStrengthDesc = signalDesc(val)
				}
			}
		}
		return nil
	})
	if err != nil {
		return status, fmt.Errorf("modem not accessible: %w", err)
	}
	return status, nil
}

// SendSMS sends a text-mode SMS.
func (d *Driver) SendSMS(ctx context.Context, phoneNumber, message string) (*modem.SendResult, error) {
	result := &modem.SendResult{}
	if phoneNumber == "" {
		return result, fmt.Errorf("phone number is required")
	}
	if message == "" {
		return result, fmt.Errorf("message is required")
	}

	err := d.t.WithPort(ctx, func(port serial.Port) error {
		// Step 1: ping
		resp, err := atserial.SendAT(ctx, port, "AT", 3*time.Second)
		if err != nil || !strings.Contains(resp, "OK") {
			result.Error = "modem not responding"
			return fmt.Errorf("modem not responding")
		}

		// Step 2: SMS text mode
		resp, err = atserial.SendAT(ctx, port, "AT+CMGF=1", 3*time.Second)
		if err != nil || !strings.Contains(resp, "OK") {
			result.Error = "failed to set SMS text mode"
			return fmt.Errorf("failed to set SMS text mode")
		}

		// Step 3: GSM charset
		_, _ = atserial.SendAT(ctx, port, `AT+CSCS="GSM"`, 3*time.Second)

		// Step 4: SMS parameters (validity period etc.)
		_, _ = atserial.SendAT(ctx, port, "AT+CSMP=17,167,0,0", 3*time.Second)

		// Step 5: network registration guard
		if resp, err := atserial.SendAT(ctx, port, "AT+CREG?", 3*time.Second); err == nil {
			re := regexp.MustCompile(`\+CREG: \d,(\d)`)
			if matches := re.FindStringSubmatch(resp); len(matches) > 1 {
				regStatus := matches[1]
				if regStatus != "1" && regStatus != "5" {
					result.Error = "not registered on network"
					return fmt.Errorf("not registered on network (status: %s)", regStatus)
				}
			}
		}

		// Step 6: CMGS
		if _, err := port.Write([]byte(fmt.Sprintf("AT+CMGS=\"%s\"\r", phoneNumber))); err != nil {
			result.Error = fmt.Sprintf("failed to initiate SMS: %v", err)
			return fmt.Errorf("failed to initiate SMS: %w", err)
		}
		prompt, err := atserial.ReadUntil(ctx, port, []string{">", "ERROR"}, 3*time.Second)
		if err != nil {
			result.Error = err.Error()
			return err
		}
		if strings.Contains(prompt, "ERROR") {
			result.Error = "modem rejected the send command"
			return fmt.Errorf("modem rejected AT+CMGS: %s", prompt)
		}

		// Step 7: body + Ctrl-Z
		if _, err := port.Write([]byte(message + string(rune(26)))); err != nil {
			result.Error = fmt.Sprintf("failed to write message body: %v", err)
			return fmt.Errorf("failed to write message body: %w", err)
		}

		finalResp, err := atserial.ReadUntil(ctx, port, []string{"+CMGS:", "OK", "ERROR"}, 30*time.Second)
		if err != nil {
			result.Error = err.Error()
			return err
		}

		re := regexp.MustCompile(`\+CMGS: (\d+)`)
		if matches := re.FindStringSubmatch(finalResp); len(matches) > 1 {
			result.Success = true
			result.MessageReference = matches[1]
			return nil
		}
		if strings.Contains(finalResp, "OK") {
			result.Success = true
			return nil
		}
		if strings.Contains(finalResp, "ERROR") {
			result.Error = "modem returned an error"
			return fmt.Errorf("modem returned an error: %s", finalResp)
		}

		// Uncertain
		result.Success = true
		result.Error = "uncertain result — check phone for delivery"
		return nil
	})
	if err != nil {
		// Err on port-open also needs a useful result.Error.
		if result.Error == "" {
			result.Error = fmt.Sprintf("modem not accessible: %v", err)
		}
		return result, err
	}
	return result, nil
}

func signalDesc(val int) string {
	switch {
	case val == 99:
		return "unknown"
	case val < 10:
		return "weak"
	case val < 20:
		return "fair"
	case val < 30:
		return "good"
	default:
		return "excellent"
	}
}
