package config

import (
	"flag"
	"os"
)

// Config holds the application configuration.
type Config struct {
	// Serial port settings
	PortName string
	BaudRate int

	// HTTP server settings
	ListenAddr string

	// Queue settings
	QueueSize   int
	HistorySize int

	// Simulator mode — run without a real modem
	Simulator bool

	// Auth settings
	AdminPassword string
	KeysFile      string
}

// Load parses command-line flags and returns the application configuration.
func Load() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.PortName, "port", "COM3", "serial port name (e.g. COM3, /dev/ttyUSB0)")
	flag.IntVar(&cfg.BaudRate, "baud", 115200, "serial port baud rate")
	flag.StringVar(&cfg.ListenAddr, "listen", "127.0.0.1:8080", "HTTP listen address (e.g. 127.0.0.1:8080, :8080)")
	flag.IntVar(&cfg.QueueSize, "queue-size", 100, "maximum number of SMS jobs waiting in the queue")
	flag.IntVar(&cfg.HistorySize, "history-size", 1000, "maximum number of completed SMS jobs to keep in history")
	flag.BoolVar(&cfg.Simulator, "simulator", false, "run in simulator mode without a real modem")
	flag.StringVar(&cfg.AdminPassword, "admin-password", "", "admin dashboard password (overwrites stored value; if empty, uses stored or generates new)")
	flag.StringVar(&cfg.KeysFile, "keys-file", "keys.json", "path to the API keys and admin password JSON file")
	flag.Parse()

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
