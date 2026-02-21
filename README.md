# SMS Gateway

A lightweight HTTP API for sending SMS messages through a Huawei GSM modem connected via serial port. Distributed as a single `.exe` file for Windows.

Incoming SMS requests are saved to an in-memory queue and processed one at a time by a background worker, so no messages are lost under heavy traffic.

## Modem Setup

Before running the gateway, the Huawei modem must be bound to a COM port.

1. Connect the Huawei modem via USB.
2. Use the [`balong_usbdload.exe`](https://github.com/pearlxcore/balong-usbload-english) utility to initialize the modem so that Windows exposes it as a serial COM port:
   ```
   tools\balong_usbdload.exe
   ```
3. Open **Device Manager** and verify the modem appears under **Ports (COM & LPT)** — note the assigned port name (e.g. `COM3`).

You can verify the modem is working correctly using the standalone PowerShell test script — see [scripts/README.md](scripts/README.md) for details.

> **Note:** The phone number must include the country code (e.g. `+420` for Czech Republic). This was confirmed as required during testing on a Huawei E3372h-153 modem — without the country code the modem rejects the message.

## Building

Requires Go 1.25+.

```
go build -o sms-gateway.exe .
```

## Usage

```
sms-gateway.exe [flags]
```

### Flags

| Flag           | Default  | Description                                           |
|----------------|----------|-------------------------------------------------------|
| `-port`        | `COM3`   | Serial port name (e.g. `COM3`, `/dev/ttyUSB0`)       |
| `-baud`        | `115200` | Serial port baud rate                                 |
| `-listen`      | `:8080`  | HTTP listen address (e.g. `:8080`, `127.0.0.1:9000`) |
| `-queue-size`  | `100`    | Maximum number of SMS jobs waiting in the queue       |

### Examples

```
# Run with defaults (COM3, 115200 baud, listen on :8080, queue size 100)
sms-gateway.exe

# Custom port and listen address
sms-gateway.exe -port COM5 -listen 127.0.0.1:9000

# Larger queue for high-traffic use
sms-gateway.exe -queue-size 500
```

## API

All responses are JSON with `Content-Type: application/json`.

### GET /api/status

Returns the current modem status.

**Response:**

```json
{
  "connected": true,
  "network_registered": true,
  "signal_strength": 21,
  "signal_strength_desc": "good",
  "manufacturer": "huawei",
  "model": "E3372"
}
```

| Field                  | Type    | Description                                          |
|------------------------|---------|------------------------------------------------------|
| `connected`            | bool    | Whether the modem responds to AT commands            |
| `network_registered`   | bool    | Whether the modem is registered on a mobile network  |
| `signal_strength`      | int     | Raw signal value (0–31, 99 = unknown)                |
| `signal_strength_desc` | string  | Human-readable signal level (`weak`, `fair`, `good`, `excellent`, `unknown`) |
| `manufacturer`         | string  | Modem manufacturer                                   |
| `model`                | string  | Modem model                                          |

### POST /api/send

Enqueues an SMS message for delivery. Returns immediately with a job ID that can be used to track the result.

**Request body:**

```json
{
  "phone": "+420123456789",
  "message": "Hello!"
}
```

| Field     | Type   | Required | Description                      |
|-----------|--------|----------|----------------------------------|
| `phone`   | string | yes      | Destination phone number (E.164) |
| `message` | string | yes      | Text message to send             |


**Response (202 Accepted):**

```json
{
  "id": "a1b2c3d4e5f67890",
  "status": "queued",
  "pending": 3
}
```

| Field     | Type   | Description                                   |
|-----------|--------|-----------------------------------------------|
| `id`      | string | Unique job ID for tracking                    |
| `status`  | string | Always `queued` on success                    |
| `pending` | int    | Number of jobs currently waiting in the queue |

If the queue is full, the endpoint returns **503 Service Unavailable**.

### GET /api/queue/{id}

Returns the current status of a queued SMS job.

**Response:**

```json
{
  "id": "a1b2c3d4e5f67890",
  "phone": "+420123456789",
  "message": "Hello!",
  "status": "sent",
  "result": {
    "success": true,
    "message_reference": "42"
  },
  "created_at": "2025-01-15T10:30:00Z",
  "updated_at": "2025-01-15T10:30:05Z"
}
```

| Field        | Type   | Description                                                     |
|--------------|--------|-----------------------------------------------------------------|
| `id`         | string | Unique job ID                                                   |
| `phone`      | string | Destination phone number                                        |
| `message`    | string | Text message                                                    |
| `status`     | string | One of `queued`, `sending`, `sent`, `failed`                    |
| `result`     | object | Modem send result (present once sending completes)              |
| `error`      | string | Error description (present when status is `failed`)             |
| `created_at` | string | Timestamp when the job was enqueued                             |
| `updated_at` | string | Timestamp of the last status change                             |

Returns **404 Not Found** if the job ID does not exist.

## License

See [LICENSE](LICENSE).
