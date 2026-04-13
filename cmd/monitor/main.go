package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/satnusa/keycloak-pam-ssh/internal/config"
	"github.com/satnusa/keycloak-pam-ssh/internal/logout"
	"github.com/satnusa/keycloak-pam-ssh/internal/session"
)

const (
	// Version of the monitor daemon
	version = "1.0.0"

	// cleanupInterval defines how often the daemon cleans up stale sessions.
	cleanupInterval = 5 * time.Minute
)

func main() {
	// Parse command-line flags
	configPath := flag.String("config", config.DefaultConfigPath, "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("keycloak-ssh-monitor v%s\n", version)
		os.Exit(0)
	}

	// Setup logger
	logger := log.New(os.Stdout, "", log.LstdFlags|log.Lmsgprefix)
	logger.SetPrefix("[keycloak-ssh-monitor] ")

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}

	logger.Printf("Configuration loaded from: %s", *configPath)
	logger.Printf("Keycloak: %s/realms/%s", cfg.Keycloak.ServerURL, cfg.Keycloak.Realm)
	logger.Printf("Session store: %s", cfg.Session.StorageDir)
	logger.Printf("Listen address: %s", cfg.Monitor.ListenAddress)

	// Initialize session store
	store, err := session.NewStore(cfg.Session.StorageDir)
	if err != nil {
		logger.Fatalf("Failed to initialize session store: %v", err)
	}

	// Create HTTP handler for backchannel logout
	logoutHandler := logout.NewHandler(store, logger)

	// Setup HTTP server
	mux := http.NewServeMux()
	mux.Handle("/backchannel-logout", logoutHandler)
	mux.HandleFunc("/healthz", healthCheckHandler)
	mux.HandleFunc("/sessions", sessionsHandler(store, logger))

	server := &http.Server{
		Addr:         cfg.Monitor.ListenAddress,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start background session cleanup goroutine
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sessionCleanupLoop(ctx, store, logger)

	// Start HTTP server in a goroutine
	go func() {
		if cfg.Monitor.TLSCert != "" && cfg.Monitor.TLSKey != "" {
			logger.Printf("Starting HTTPS server on %s", cfg.Monitor.ListenAddress)
			if err := server.ListenAndServeTLS(cfg.Monitor.TLSCert, cfg.Monitor.TLSKey); err != nil && err != http.ErrServerClosed {
				logger.Fatalf("HTTPS server error: %v", err)
			}
		} else {
			logger.Printf("Starting HTTP server on %s", cfg.Monitor.ListenAddress)
			if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Fatalf("HTTP server error: %v", err)
			}
		}
	}()

	logger.Printf("Keycloak SSH Monitor v%s started successfully", version)
	logger.Printf("Backchannel logout endpoint: POST http://%s/backchannel-logout", cfg.Monitor.ListenAddress)

	// Wait for shutdown signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigCh
	logger.Printf("Received signal %v — shutting down gracefully...", sig)

	// Graceful shutdown with timeout
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Printf("HTTP server shutdown error: %v", err)
	}

	logger.Printf("Shutdown complete")
}

// sessionCleanupLoop periodically removes expired or orphaned session files.
func sessionCleanupLoop(ctx context.Context, store *session.Store, logger *log.Logger) {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	// Run once at startup
	if cleaned, err := store.CleanExpired(); err != nil {
		logger.Printf("[WARN] Session cleanup error: %v", err)
	} else if cleaned > 0 {
		logger.Printf("[INFO] Cleaned up %d expired/orphaned sessions", cleaned)
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if cleaned, err := store.CleanExpired(); err != nil {
				logger.Printf("[WARN] Session cleanup error: %v", err)
			} else if cleaned > 0 {
				logger.Printf("[INFO] Cleaned up %d expired/orphaned sessions", cleaned)
			}
		}
	}
}

// healthCheckHandler returns a simple health check response.
func healthCheckHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"status":"ok","version":"%s","timestamp":"%s"}`, version, time.Now().UTC().Format(time.RFC3339))
}

// sessionsHandler returns a handler that lists all active sessions (for debugging).
func sessionsHandler(store *session.Store, logger *log.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		sessions, err := store.ListAll()
		if err != nil {
			logger.Printf("[ERROR] Failed to list sessions: %v", err)
			http.Error(w, "Internal error", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "Active SSH Sessions (%d):\n", len(sessions))
		fmt.Fprintf(w, "%-40s %-15s %-10s %-25s\n", "SESSION ID", "USER", "PID", "CREATED")
		fmt.Fprintf(w, "%s\n", "────────────────────────────────────────────────────────────────────────────────────────────")

		for _, sess := range sessions {
			fmt.Fprintf(w, "%-40s %-15s %-10d %-25s\n",
				sess.SessionID,
				sess.Username,
				sess.SSHPid,
				sess.CreatedAt.Format("2006-01-02 15:04:05"),
			)
		}
	}
}
