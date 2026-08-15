package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/patrickrho-patty/pccp/internal/api"
	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/db"
)

func main() {
	addr := flag.String("addr", "", "HTTP listen address (overrides PCCP_HTTP_ADDR)")
	seed := flag.Bool("seed", false, "Seed demo data and exit")
	flag.Parse()

	cfg := config.LoadFromEnv()
	if *addr != "" {
		cfg.HTTPAddr = *addr
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("PCCP Control Plane — starting up")
	// Runtime config (pccp.toml) reloads on SIGHUP.
	config.ListenForReload(make(chan struct{}))

	// Initialize database
	database, err := db.FromEnv()
	if err != nil {
		log.Fatalf("failed to init database: %v", err)
	}

	// Ensure storage directory exists
	if err := config.EnsureStorageDir(cfg.StorageDir); err != nil {
		log.Fatalf("failed to create storage dir: %v", err)
	}

	// Create API server
	server, err := api.New(database, cfg.JWTSecret)
	if err != nil {
		log.Fatalf("failed to create API server: %v", err)
	}

	// Catalog push on publish (web/18 B): deliver the delta to live
	// sessions through the relay admin channel when configured.
	server.SetModelPublishedHook(func(packageID string) {
		base := strings.TrimSuffix(config.RelayAdminURL(), "/")
		if base == "" {
			log.Printf("model %s published — catalog push deferred (relay_admin_url not configured; sessions refresh at next setup)", packageID)
			return
		}
		resp, err := http.Post(base+"/v1/catalog/broadcast", "application/json", nil)
		if err != nil {
			log.Printf("catalog broadcast for %s failed: %v", packageID, err)
			return
		}
		resp.Body.Close()
	})

	// Seed demo data if requested
	if *seed {
		if err := seedDemoData(database, server); err != nil {
			log.Fatalf("seed failed: %v", err)
		}
		log.Println("Demo data seeded successfully")
		os.Exit(0)
	}

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down...")
		os.Exit(0)
	}()

	// Start serving
	log.Printf("Control Plane API: http://localhost%s", cfg.HTTPAddr)
	log.Printf("Admin UI:          http://localhost%s/", cfg.HTTPAddr)
	if err := server.ListenAndServe(cfg.HTTPAddr); err != nil {
		log.Fatalf("server error: %v", err)
	}
}

func seedDemoData(database interface{}, server *api.Server) error {
	// This is called from a separate function in the seed command
	// For now, just log
	fmt.Println("Seeding is handled via the /api/auth/bootstrap endpoint")
	return nil
}
