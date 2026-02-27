package config

import (
	"flag"
)

// Config holds the application configuration.
type Config struct {
	// Serial port settings
	PortName string
	BaudRate int

	// HTTP server settings
	ListenAddr string

	// Queue settings
	QueueSize int

	// Simulator mode — run without a real modem
	Simulator bool
}

// Load parses command-line flags and returns the application configuration.
func Load() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.PortName, "port", "COM3", "serial port name (e.g. COM3, /dev/ttyUSB0)")
	flag.IntVar(&cfg.BaudRate, "baud", 115200, "serial port baud rate")
	flag.StringVar(&cfg.ListenAddr, "listen", ":8080", "HTTP listen address (e.g. :8080, 127.0.0.1:9000)")
	flag.IntVar(&cfg.QueueSize, "queue-size", 100, "maximum number of SMS jobs waiting in the queue")
	flag.BoolVar(&cfg.Simulator, "simulator", false, "run in simulator mode without a real modem")
	flag.Parse()

	return cfg
}
