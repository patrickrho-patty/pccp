package main

import (
	"context"
	"crypto/tls"
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
	addr := flag.String("addr", "", "Relay HTTP admin listen address")
	paperAddr := flag.String("paper-addr", "", "PAPER native protocol listen address (TLS/TCP)")
	flag.Parse()

	cfg := config.LoadRelayFromEnv()
	httpAddr := *addr
	if httpAddr == "" {
		httpAddr = os.Getenv("PCCP_RELAY_HTTP_ADDR")
		if httpAddr == "" {
			httpAddr = ":8090"
		}
	}
	if *paperAddr == "" {
		*paperAddr = os.Getenv("PCCP_RELAY_PAPER_ADDR")
		if *paperAddr == "" {
			*paperAddr = ":8444"
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

	ctx, cancel := context.WithCancel(context.Background())

	// Start PAPER native protocol listener (TLS/TCP with CBOR framing)
	// Per README guardrail: "No HTTP/REST/WebSocket for protocol traffic."
	paperTLS := &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13,
		NextProtos:         []string{relay.PaperALPN},
	}
	// In production, load actual certs from cfg.TLSCertFile / cfg.TLSKeyFile
	paperListener := relay.NewPaperListener(svc, paperTLS)
	go func() {
		log.Printf("Starting PAPER native listener on %s", *paperAddr)
		if err := paperListener.ListenTCP(ctx, *paperAddr); err != nil && ctx.Err() == nil {
			log.Printf("PAPER listener error: %v", err)
		}
	}()

	// Graceful shutdown
	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down relay...")
		cancel()
		os.Exit(0)
	}()

	log.Printf("Relay admin API on %s (PAPER native on %s)", httpAddr, *paperAddr)

	// HTTP admin API (for control-plane operations, NOT for protocol traffic)
	server := relay.NewServer(svc)
	if err := server.ListenAndServe(httpAddr); err != nil {
		log.Fatalf("relay server error: %v", err)
	}
}
