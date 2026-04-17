# Adding a New Modem Driver

The SMS gateway supports pluggable modem drivers. A driver is any Go package
that implements the `modem.Modem` interface and registers itself in the driver
registry at init time.

This document walks through adding a new driver end to end.

## The interface

`internal/modem/modem.go`:

```go
type Modem interface {
    Open(ctx context.Context) error
    Close() error
    GetStatus(ctx context.Context) (*Status, error)
    SendSMS(ctx context.Context, phoneNumber, message string) (*SendResult, error)
}
```

Lifecycle contract:

- `Open` is called once at startup. Establish a persistent connection, do a
  startup ping, or return `nil` if your driver opens the port per call.
- `Close` is called once at shutdown. It must be safe to call even if `Open`
  failed.
- `GetStatus` and `SendSMS` may run concurrently. The driver is responsible
  for serializing access to any shared hardware resource.
- Honor `ctx` for cancellation/timeouts where you can.

`Status` and `SendResult` are returned verbatim to HTTP clients as JSON, so
fill them in with user-facing semantics.

## Steps

### 1. Create the driver package

Put it under `internal/modem/drivers/<name>/<name>.go`.

```go
package mydriver

import (
    "context"

    "sms-gateway/internal/modem"
)

func init() {
    modem.Register("mydriver", New)
}

type Driver struct {
    // fields your driver needs
}

// New is the registered factory.
func New(opts modem.Options) (modem.Modem, error) {
    // validate opts, construct Driver
    return &Driver{}, nil
}

func (d *Driver) Open(ctx context.Context) error  { return nil }
func (d *Driver) Close() error                    { return nil }

func (d *Driver) GetStatus(ctx context.Context) (*modem.Status, error) {
    return &modem.Status{Connected: true /* ... */}, nil
}

func (d *Driver) SendSMS(ctx context.Context, phone, message string) (*modem.SendResult, error) {
    return &modem.SendResult{Success: true, MessageReference: "..."}, nil
}
```

### 2. Wire it into the binary

Add a blank import in `app.go` so the driver's `init()` runs:

```go
import (
    // ...
    _ "sms-gateway/internal/modem/drivers/mydriver"
)
```

That is the only edit needed in the main package.

### 3. Pick up driver-specific options

Driver-specific options arrive through `modem.Options.Extra` (a
`map[string]string`). Parse them in `New`:

```go
func New(opts modem.Options) (modem.Modem, error) {
    pin := opts.String("pin", "")
    initStr := opts.String("init", "")
    useBinary := opts.Bool("pdu", false)
    retries := opts.Int("retries", 3)
    // ...
}
```

These are populated by the repeatable `-modem-opt key=value` CLI flag.

### 4. Reuse the AT-over-serial transport (optional)

If your modem speaks AT over a serial port, embed the shared transport
helper instead of re-implementing port open/close, polling, and AT
request/response plumbing:

```go
import (
    "sms-gateway/internal/modem/transport/atserial"
    "go.bug.st/serial"
)

type Driver struct {
    t *atserial.ATSerial
}

func New(opts modem.Options) (modem.Modem, error) {
    return &Driver{t: atserial.New(opts.Port, opts.BaudRate)}, nil
}

func (d *Driver) GetStatus(ctx context.Context) (*modem.Status, error) {
    status := &modem.Status{}
    err := d.t.WithPort(ctx, func(port serial.Port) error {
        resp, err := atserial.SendAT(port, "AT", 3*time.Second)
        // ...
        return nil
    })
    return status, err
}
```

`atserial.ATSerial` serializes access with an internal mutex, so concurrent
`GetStatus`/`SendSMS` callers are safe.

See `internal/modem/drivers/generic/generic.go` for a complete example.

### 5. Run it

```
./sms-gateway -driver=mydriver -port=/dev/ttyUSB0 -modem-opt=pin=1234 -modem-opt=init='AT+CNMI=2,1,0,0,0'
```

Unknown driver names fail fast at startup with a list of available drivers.

## Checklist

- [ ] Package under `internal/modem/drivers/<name>/`
- [ ] `init()` calls `modem.Register("<name>", New)`
- [ ] Factory validates `Options` and returns a useful error on bad input
- [ ] Blank import added in `app.go`
- [ ] Thread safety: hardware access is serialized
- [ ] `Close` is idempotent and safe after a failed `Open`
- [ ] Documented `-modem-opt` keys at the top of the driver file

## Reference drivers

- `internal/modem/drivers/generic/` — AT-over-serial, text-mode SMS, works
  with most commodity GSM modems. A good starting point to copy.
- `internal/modem/drivers/simulator/` — virtual modem, no hardware. Useful
  as a minimal example of the interface surface.
