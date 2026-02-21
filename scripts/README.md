# Scripts

## send-sms.ps1

A standalone PowerShell script for testing modem communication directly. Useful for verifying that the modem is responsive and the COM port is correctly assigned.

### Usage

```powershell
# Send with defaults (COM3, 115200 baud)
.\send-sms.ps1 -PhoneNumber "+420123456789" -Message "Test message"

# Specify a different port
.\send-sms.ps1 -PortName "COM5" -PhoneNumber "+420123456789"

# All parameters
.\send-sms.ps1 -PortName "COM3" -BaudRate 115200 -PhoneNumber "+420123456789" -Message "Hello!"
```

### Parameters

| Parameter      | Default                                             | Description                       |
|----------------|-----------------------------------------------------|-----------------------------------|
| `-PortName`    | `COM3`                                              | Serial port the modem is bound to |
| `-BaudRate`    | `115200`                                            | Serial port baud rate             |
| `-PhoneNumber` | —                                                   | Destination phone number (E.164)  |
| `-Message`     | `Hello, this is a test SMS from the SMS Gateway.`   | Text to send                      |

### AT commands order and function

The script opens the serial port and walks through the following AT command sequence:

1. `AT` — test modem communication
2. `AT+CMGF=1` — set SMS text mode
3. `AT+CSCS="GSM"` — set GSM character encoding
4. `AT+CSMP=17,167,0,0` — set SMS parameters
5. `AT+CREG?` — check network registration
6. `AT+CSQ` — check signal quality
7. `AT+CMGS="<number>"` — initiate SMS send, followed by the message body and `Ctrl+Z` (`0x1A`)

Each step prints the AT command and the modem's response, making it easy to diagnose connectivity or configuration issues.
