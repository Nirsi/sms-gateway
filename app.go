package main

import (
	"log"
	"net/http"

	"sms-gateway/internal/api"
	"sms-gateway/internal/config"
	"sms-gateway/internal/modem"
	"sms-gateway/internal/queue"
)

func main() {
	cfg := config.Load()

	log.Printf("SMS Gateway starting")
	log.Printf("  Serial port : %s @ %d baud", cfg.PortName, cfg.BaudRate)
	log.Printf("  Listen addr : %s", cfg.ListenAddr)
	log.Printf("  Queue size  : %d", cfg.QueueSize)

	m := modem.New(cfg.PortName, cfg.BaudRate)
	q := queue.New(m, cfg.QueueSize)

	handler := api.NewHandler(m, q)
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	log.Printf("Listening on %s", cfg.ListenAddr)
	if err := http.ListenAndServe(cfg.ListenAddr, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
