package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/db"
	"github.com/patrickrho-patty/pccp/internal/pia"
)

func main() {
	addr := flag.String("addr", "", "PIA HTTP listen address")
	orgID := flag.String("org", "", "Organization ID to enroll with")
	modelPkg := flag.String("model", "", "Model package ID to serve")
	cpURL := flag.String("cp-url", "", "Control Plane API URL")
	enroll := flag.Bool("enroll", false, "Enroll with control plane on startup")
	flag.Parse()

	cfg := config.LoadPIAFromEnv()
	if *addr != "" {
		cfg.ServingEngineURL = *addr
	}
	if *modelPkg != "" {
		cfg.ModelPackageID = *modelPkg
	}

	cpAPIURL := config.LoadRelayFromEnv().ControlPlaneURL
	if *cpURL != "" {
		cpAPIURL = *cpURL
	}
	if v := os.Getenv("PCCP_CP_URL"); v != "" {
		cpAPIURL = v
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Patty Inference Agent (PIA) — starting up")

	database, err := db.FromEnv()
	if err != nil {
		log.Fatalf("failed to init database: %v", err)
	}

	svc, err := pia.New(database, pia.Config{
		PeerID:          cfg.PeerID,
		ServingURL:      cfg.ServingEngineURL,
		ServingType:     cfg.ServingEngineType,
		AssuranceLevel:  cfg.AssuranceLevel,
		ModelPackageID:  cfg.ModelPackageID,
		ControlPlaneURL: cpAPIURL,
	})
	if err != nil {
		log.Fatalf("failed to create PIA: %v", err)
	}

	log.Printf("PIA Peer ID:     %s", svc.PeerID())
	log.Printf("PIA Public Key:  %s", svc.PublicKeyHex())
	log.Printf("Serving Type:    %s", cfg.ServingEngineType)
	log.Printf("Serving URL:     %s", cfg.ServingEngineURL)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if *enroll && *orgID != "" && *modelPkg != "" {
		log.Printf("Enrolling with control plane at %s ...", cpAPIURL)
		if err := svc.EnrollWithControlPlane(ctx, *orgID, *modelPkg); err != nil {
			log.Fatalf("enrollment failed: %v", err)
		}
		log.Printf("Enrolled as endpoint: %s", svc.EndpointID())

		if err := svc.RequestLease(ctx); err != nil {
			log.Printf("Warning: initial lease request failed: %v", err)
		}

		interval := 5 * time.Minute
		svc.StartAttestationLoop(ctx, interval)
		log.Printf("Re-attestation loop started (interval: %v)", interval)
	}

	go func() {
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
		<-sigChan
		log.Println("Shutting down PIA...")
		cancel()
		os.Exit(0)
	}()

	piaAddr := os.Getenv("PCCP_PIA_HTTP_ADDR")
	if piaAddr == "" {
		piaAddr = ":9090"
	}
	// Start PAPER listener for Relay connections (v2 §9.2)
	paperAddr := os.Getenv("PCCP_PIA_PAPER_ADDR")
	if paperAddr == "" {
		paperAddr = ":9444"
	}
	paperListener := pia.NewPaperListener(svc)
	go func() {
		log.Printf("Starting PAPER listener on %s", paperAddr)
		if err := paperListener.ListenTCP(ctx, paperAddr); err != nil && ctx.Err() == nil {
			log.Printf("PAPER listener error: %v", err)
		}
	}()
	log.Printf("PAPER listener on %s, HTTP on %s", paperAddr, piaAddr)

	// HTTP server (health, admin, internal vLLM adapter only)
	server := pia.NewServer(svc)
	if err := server.ListenAndServe(piaAddr); err != nil {
		log.Fatalf("PIA server error: %v", err)
	}
}
