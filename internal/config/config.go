package config

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

// Config holds the application configuration.
type Config struct {
	// Modem driver settings
	Driver    string            // registered driver name (e.g. "generic", "simulator")
	DriverSet bool              // true if -driver was explicitly passed on the command line
	PortName  string            // serial port name (drivers that need one)
	BaudRate  int               // serial baud rate
	ModemOpts map[string]string // driver-specific key=value options

	// HTTP server settings
	ListenAddr string

	// Queue settings
	QueueSize   int
	HistorySize int

	// Simulator mode — deprecated alias for -driver=simulator.
	Simulator bool

	// Auth settings
	AdminPassword string
	KeysFile      string
}

// modemOptsFlag implements flag.Value so -modem-opt can be repeated and
// accumulated into a map.
type modemOptsFlag struct {
	m map[string]string
}

func (f *modemOptsFlag) String() string {
	if f == nil || len(f.m) == 0 {
		return ""
	}
	parts := make([]string, 0, len(f.m))
	for k, v := range f.m {
		parts = append(parts, k+"="+v)
	}
	return strings.Join(parts, ",")
}

func (f *modemOptsFlag) Set(raw string) error {
	if f.m == nil {
		f.m = map[string]string{}
	}
	k, v, ok := strings.Cut(raw, "=")
	k = strings.TrimSpace(k)
	v = strings.TrimSpace(v)
	if !ok || k == "" {
		return fmt.Errorf("modem-opt must be key=value, got %q", raw)
	}
	f.m[k] = v
	return nil
}

// Load parses command-line flags and returns the application configuration.
func Load() *Config {
	cfg := &Config{}
	opts := &modemOptsFlag{}

	flag.StringVar(&cfg.Driver, "driver", "generic", "modem driver name (e.g. generic, simulator)")
	flag.StringVar(&cfg.PortName, "port", "COM3", "serial port name (e.g. COM3, /dev/ttyUSB0)")
	flag.IntVar(&cfg.BaudRate, "baud", 115200, "serial port baud rate")
	flag.Var(opts, "modem-opt", "driver-specific option key=value (repeatable)")
	flag.StringVar(&cfg.ListenAddr, "listen", "127.0.0.1:8080", "HTTP listen address (e.g. 127.0.0.1:8080, :8080)")
	flag.IntVar(&cfg.QueueSize, "queue-size", 100, "maximum number of SMS jobs waiting in the queue")
	flag.IntVar(&cfg.HistorySize, "history-size", 1000, "maximum number of completed SMS jobs to keep in history")
	flag.BoolVar(&cfg.Simulator, "simulator", false, "DEPRECATED: use -driver=simulator")
	flag.StringVar(&cfg.AdminPassword, "admin-password", "", "admin dashboard password (overwrites stored value; if empty, uses stored or generates new)")
	flag.StringVar(&cfg.KeysFile, "keys-file", "keys.json", "path to the API keys and admin password JSON file")
	flag.Parse()

	cfg.ModemOpts = opts.m

	flag.Visit(func(fl *flag.Flag) {
		if fl.Name == "driver" {
			cfg.DriverSet = true
		}
	})

	if cfg.AdminPassword == "" {
		cfg.AdminPassword = os.Getenv("SMS_GATEWAY_ADMIN_PASSWORD")
	}

	// Clamp values that must not be negative.
	if cfg.QueueSize < 0 {
		cfg.QueueSize = 0
	}
	if cfg.HistorySize < 0 {
		cfg.HistorySize = 0
	}

	return cfg
}
