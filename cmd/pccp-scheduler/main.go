package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/patrickrho-patty/pccp/internal/config"
	"github.com/patrickrho-patty/pccp/internal/dari"
	"github.com/patrickrho-patty/pccp/internal/scheduler"
)

func main() {
	addr := flag.String("addr", "", "Scheduler HTTP admin listen address")
	dariAddr := flag.String("dari-addr", "", "DARI worker listen address (TLS/TCP)")
	flag.Parse()

	cfg := config.LoadSchedulerFromEnv()
	httpAddr := *addr
	if httpAddr == "" {
		httpAddr = cfg.HTTPAddr
	}
	workerAddr := *dariAddr
	if workerAddr == "" {
		workerAddr = cfg.DARIAddr
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("PCCP Scheduler — fleet registry starting")

	trust, err := scheduler.BuildTrust(cfg.CAPublicKeyHex, cfg.ConfigPublicKeyHex, cfg.CAIssuerID)
	if err != nil {
		log.Fatalf("scheduler: build trust: %v", err)
	}

	var policy scheduler.PolicySource
	if cfg.PolicyFile != "" {
		filePolicy, err := scheduler.LoadPolicyFile(cfg.PolicyFile)
		if err != nil {
			log.Fatalf("scheduler: load policy: %v", err)
		}
		policy = filePolicy
		log.Printf("scheduler: tenant reachability policy loaded from %s", cfg.PolicyFile)
	}

	_, evidenceKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		log.Fatalf("scheduler: generate evidence key: %v", err)
	}

	svc := scheduler.NewScheduler(
		trust,
		policy,
		time.Duration(cfg.LeaseTTLSeconds)*time.Second,
		time.Duration(cfg.LeaseGraceSeconds)*time.Second,
		evidenceKey,
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// DARI worker listener (TLS/TCP).
	listener := scheduler.NewDARIListener(svc, nil)
	ln, err := dari.ListenTCP(workerAddr, listener.TLSConfig())
	if err != nil {
		log.Fatalf("scheduler: dari listen: %v", err)
	}
	defer ln.Close()
	go listener.ServeTCP(ln)
	log.Printf("scheduler: DARI worker listener on %s", workerAddr)

	// HTTP admin API (CP read-through).
	httpSrv := &http.Server{
		Addr:    httpAddr,
		Handler: scheduler.NewHTTPHandler(svc, cfg.AdminToken),
	}
	go func() {
		log.Printf("scheduler: HTTP admin API on %s", httpAddr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("scheduler: http serve: %v", err)
		}
	}()

	// Lease sweep loop.
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				if evicted := svc.Sweep(now); len(evicted) > 0 {
					log.Printf("scheduler: evicted %d expired workers: %v", len(evicted), evicted)
				}
			}
		}
	}()

	// Revocation feed poll (offline PPC verification stays authoritative; the
	// feed refreshes the revocation view. CP down = last view kept).
	if cpURL := os.Getenv("PCCP_SCHED_CP_URL"); cpURL != "" {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					pollRevocations(cpURL, os.Getenv("PCCP_SCHED_CP_TOKEN"), svc)
				}
			}
		}()
	}

	// Shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	log.Println("scheduler: shutting down")
	cancel()
	httpSrv.Close()
}

// pollRevocations fetches the CP revocation feed and applies it to the
// scheduler's revocation view.
func pollRevocations(cpURL, token string, svc *scheduler.Scheduler) {
	req, err := http.NewRequest(http.MethodGet, cpURL+"/api/scheduler/revocations", nil)
	if err != nil {
		log.Printf("scheduler: revocation poll: %v", err)
		return
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("scheduler: revocation poll failed (CP down?): %v", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Printf("scheduler: revocation poll status %d", resp.StatusCode)
		return
	}
	var feed struct {
		RevokedPeerIDs []string `json:"revoked_peer_ids"`
		RevokedSerials []string `json:"revoked_serials"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&feed); err != nil {
		log.Printf("scheduler: revocation poll decode: %v", err)
		return
	}
	svc.UpdateRevocations(feed.RevokedSerials, feed.RevokedPeerIDs)
}
