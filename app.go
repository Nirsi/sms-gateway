package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	q := queue.New(m, cfg.QueueSize, cfg.HistorySize, cfg.Simulator)

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

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	log.Printf("Listening on %s", cfg.ListenAddr)
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
		<-stop

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := server.Shutdown(ctx); err != nil {
			log.Printf("graceful shutdown failed: %v", err)
		}
	}()

	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self' 'unsafe-inline'; script-src 'self'; img-src 'self' data:; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}
