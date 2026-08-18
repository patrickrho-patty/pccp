// Command pccp-alert-backfill migrates legacy plaintext alert-endpoint
// targets to envelope-encrypted storage. Idempotent: rows that
// already have TargetEnc populated are skipped. Designed to run after
// every running instance understands the encrypted column
// (PAT-1502 PR 2).
//
// Usage:
//
//	pccp-alert-backfill --database-url "$DATABASE_URL" \
//	    --kek-file ./kek.bin --kek-id prod-kek-2026-q3 \
//	    --batch-size 200 --approve
//
// Without --approve, runs in dry-run mode and reports counts only.
package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/patrickrho-patty/pccp/internal/keymgmt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type legacyRow struct {
	ID             string
	OrganizationID string
	Target         string
}

func main() {
	dsn := flag.String("database-url", "", "Postgres DSN (required)")
	kekFile := flag.String("kek-file", "", "Path to a 32-byte KEK file (required)")
	kekID := flag.String("kek-id", "prod-kek-2026-q3", "KEK identifier")
	batch := flag.Int("batch-size", 200, "Rows per transaction")
	approve := flag.Bool("approve", false, "Apply changes (omit for dry-run)")
	flag.Parse()

	if *dsn == "" || *kekFile == "" {
		fmt.Fprintln(os.Stderr, "usage: pccp-alert-backfill --database-url ... --kek-file ... [--kek-id ...] [--approve]")
		os.Exit(2)
	}

	kek, err := os.ReadFile(*kekFile)
	if err != nil {
		log.Fatalf("read KEK: %v", err)
	}
	provider, err := keymgmt.NewLocalProvider(kek, *kekID)
	if err != nil {
		log.Fatalf("provider: %v", err)
	}

	db, err := gorm.Open(postgres.Open(*dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("open: %v", err)
	}

	rows := []legacyRow{}
	if err := db.Raw(`SELECT id, organization_id, target FROM alert_endpoints WHERE target <> '' AND (target_enc IS NULL OR target_enc = '')`).Scan(&rows).Error; err != nil {
		log.Fatalf("scan: %v", err)
	}
	fmt.Printf("found %d legacy rows\n", len(rows))

	if !*approve {
		fmt.Println("dry-run: re-run with --approve to apply")
		return
	}

	migrated := 0
	failed := 0
	for i := 0; i < len(rows); i += *batch {
		end := i + *batch
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		if err := db.Transaction(func(tx *gorm.DB) error {
			for _, r := range chunk {
				enc, err := seal(provider, r.Target)
				if err != nil {
					log.Printf("seal %s: %v", r.ID, err)
					failed++
					return nil
				}
				if err := tx.Exec(`UPDATE alert_endpoints SET target_enc = ?, target_kek_id = ?, target = '' WHERE id = ?`, enc, *kekID, r.ID).Error; err != nil {
					log.Printf("update %s: %v", r.ID, err)
					failed++
					return nil
				}
				migrated++
			}
			return nil
		}); err != nil {
			log.Printf("transaction: %v", err)
		}
	}
	fmt.Printf("migrated=%d failed=%d\n", migrated, failed)

	if err := db.Exec(`SELECT 1`).Error; err != nil { // touch connection
		log.Fatalf("verify: %v", err)
	}
	fmt.Println("backfill complete")
}

func seal(provider keymgmt.KeyProvider, raw string) (string, error) {
	env, err := keymgmt.Seal(provider, []byte(raw))
	if err != nil {
		return "", err
	}
	raw2, err := json.Marshal(env)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw2), nil
}
