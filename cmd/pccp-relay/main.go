package main

import (
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/db"
	"github.com/patrickrho-patty/pccp/internal/relay"
)

func main() {
	addr := flag.String("addr", "", "Relay HTTP admin listen address")
	paperAddr := flag.String("dari-addr", "", "DARI native protocol listen address (TLS/TCP)")
	flag.Parse()

	cfg := config.LoadRelayFromEnv()
	httpAddr := *addr
	if httpAddr == "" {
		httpAddr = os.Getenv("PCCP_RELAY_HTTP_ADDR")
		if httpAddr == "" {
			httpAddr = ":8090"
		}
	}
	dariAddr := *paperAddr
	if dariAddr == "" {
		dariAddr = os.Getenv("PCCP_RELAY_DARI_ADDR")
		if dariAddr == "" {
			dariAddr = os.Getenv(dari.LegacyPaper1RelayAddrEnv)
		}
		if dariAddr == "" {
			dariAddr = ":8444"
		}
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("DARI Relay — starting up")

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

	// Build the DARI trust bundle from the identity CA. The relay's
	// PeerAuthenticator verifies every AUTH_PROOF against this issuer
	// set and the live revocation snapshot — the A1 e2e wiring. A
	// harness without a CA-issued, unrevoked PPC cannot authenticate.
	epoch, revoked := svc.Identity().RevocationSnapshot()
	revokedSerials := make(map[string]uint64, len(revoked))
	for serial, at := range revoked {
		revokedSerials[serial] = at
	}
	trust := relay.TrustBundle{
		Issuers: map[string]ed25519.PublicKey{
			svc.Identity().CAIssuerID(): svc.Identity().CAPublicKeyRaw(),
		},
		ProtocolVersion: 1,
		RevocationEpoch: epoch,
		RevokedSerials:  revokedSerials,
	}

	// Start DARI native protocol listener (TLS/TCP with CBOR framing)
	// Per README guardrail: "No HTTP/REST/WebSocket for protocol traffic."
	// A nil config makes the listener generate a self-signed dev cert;
	// when real certs are configured they take precedence.
	var paperTLS *tls.Config
	if cert := os.Getenv("PCCP_RELAY_TLS_CERT"); cert != "" {
		key := os.Getenv("PCCP_RELAY_TLS_KEY")
		loaded, lerr := tls.LoadX509KeyPair(cert, key)
		if lerr != nil {
			log.Fatalf("failed to load DARI TLS cert/key: %v", lerr)
		}
		paperTLS = &tls.Config{
			MinVersion:   tls.VersionTLS13,
			NextProtos:   []string{relay.DARIALPN},
			Certificates: []tls.Certificate{loaded},
		}
	}
	paperListener := relay.NewDARIListener(svc, paperTLS, trust)
	go func() {
		log.Printf("Starting DARI native listener on %s (issuer=%s, revoked=%d)", dariAddr, svc.Identity().CAIssuerID(), len(revokedSerials))
		if err := paperListener.ListenTCP(ctx, dariAddr); err != nil && ctx.Err() == nil {
			log.Printf("DARI listener error: %v", err)
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

	log.Printf("Relay admin API on %s (DARI native on %s)", httpAddr, dariAddr)

	// HTTP admin API (for control-plane operations, NOT for protocol traffic)
	server := relay.NewServer(svc)
	if err := server.ListenAndServe(httpAddr); err != nil {
		log.Fatalf("relay server error: %v", err)
	}
}
