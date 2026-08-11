package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/db"
	"github.com/patrickrho-patty/pccp/internal/relay"
)

func main() {
	addr := flag.String("addr", "", "Relay HTTP listen address")
	flag.Parse()

	cfg := config.LoadRelayFromEnv()
	listenAddr := *addr
	if listenAddr == "" {
		listenAddr = os.Getenv("PCCP_RELAY_HTTP_ADDR")
		if listenAddr == "" {
			listenAddr = ":8090"
		}
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("PAPER Relay — starting up")

	database, err := db.FromEnv()
	if err != nil {
		log.Fatalf("failed to init database: %v", err)
	}

	relayID := "relay-local-1"
	svc, err := relay.New(database, cfg.ControlPlaneURL, relayID)
	if err != nil {
		log.Fatalf("failed to create relay: %v", err)
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down relay...")
		os.Exit(0)
	}()

	server := relay.NewServer(svc)
	if err := server.ListenAndServe(listenAddr); err != nil {
		log.Fatalf("relay server error: %v", err)
	}
}
