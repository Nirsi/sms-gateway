package main

import (
	"log"
	"net/http"

	"sms-gateway/internal/api"
	"sms-gateway/internal/auth"
	"sms-gateway/internal/config"
	"sms-gateway/internal/modem"
	"sms-gateway/internal/queue"
	"sms-gateway/internal/web"
)

func main() {
	cfg := config.Load()

	log.Printf("SMS Gateway starting")
	log.Printf("  Listen addr  : %s", cfg.ListenAddr)
	log.Printf("  Queue size   : %d", cfg.QueueSize)
	log.Printf("  History size : %d", cfg.HistorySize)

	// Initialize auth store (loads/creates keys.json).
	authStore, err := auth.NewStore(cfg.KeysFile, cfg.AdminPassword)
	if err != nil {
		log.Fatalf("failed to initialize auth store: %v", err)
	}

	// Initialize modem.
	var m modem.Modem
	if cfg.Simulator {
		log.Printf("  Mode         : SIMULATOR (no real modem)")
		m = modem.NewSimulator()
	} else {
		log.Printf("  Serial port  : %s @ %d baud", cfg.PortName, cfg.BaudRate)
		m = modem.New(cfg.PortName, cfg.BaudRate)
	}

	// Initialize queue.
	q := queue.New(m, cfg.QueueSize, cfg.HistorySize)

	// Set up HTTP mux.
	mux := http.NewServeMux()

	// Register API routes (protected by API key middleware).
	apiHandler := api.NewHandler(m, q)
	apiHandler.RegisterRoutes(mux, auth.RequireAPIKey(authStore))

	// Register web dashboard routes.
	webServer, err := web.NewServer(authStore, m, q)
	if err != nil {
		log.Fatalf("failed to initialize web server: %v", err)
	}
	webServer.RegisterRoutes(mux)

	log.Printf("Listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
