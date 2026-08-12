package db

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/patrickrho-patty/pccp/internal/identity"
	"github.com/patrickrho-patty/pccp/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config holds database connection configuration.
type Config struct {
 	Driver string `json:"driver"`  // "postgres" or "sqlite"
 	DSN    string `json:"d_s_n"`  // connection string
}

// New creates a new GORM database connection based on the config.
func New(cfg Config) (*gorm.DB, error) {
	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	var db *gorm.DB
	var err error

	switch cfg.Driver {
	case "postgres", "postgresql":
		db, err = gorm.Open(postgres.Open(cfg.DSN), gormCfg)
	case "sqlite", "sqlite3":
		// Ensure the parent directory exists for SQLite files
		if cfg.DSN != "" {
			dir := filepath.Dir(cfg.DSN)
			if dir != "." && dir != "/" {
				if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
					return nil, fmt.Errorf("db: create sqlite directory: %w", mkErr)
				}
			}
		}
		db, err = gorm.Open(sqlite.Open(cfg.DSN), gormCfg)
		if db != nil {
			// Enable foreign keys for SQLite
			if execErr := db.Exec("PRAGMA foreign_keys = ON").Error; execErr != nil {
				return nil, fmt.Errorf("db: enable sqlite foreign keys: %w", execErr)
			}
		}
	default:
		return nil, fmt.Errorf("db: unsupported driver %q (use postgres or sqlite)", cfg.Driver)
	}

	if err != nil {
		return nil, fmt.Errorf("db: open %s: %w", cfg.Driver, err)
	}

	log.Printf("db: connected via %s", cfg.Driver)
	return db, nil
}

// AutoMigrate runs GORM auto-migration for all domain models.
func AutoMigrate(db *gorm.DB) error {
	if err := db.AutoMigrate(append(models.AllModels(), &identity.AdminCredentials{})...); err != nil {
		return fmt.Errorf("db: auto-migrate: %w", err)
	}
	log.Printf("db: auto-migration complete (%d models)", len(models.AllModels())+1)
	return nil
}

// FromEnv creates a database connection from environment variables.
//
// PCCP_DB_DRIVER — "postgres" or "sqlite" (default: "sqlite")
// PCCP_DB_DSN    — connection string
//
// For SQLite, if DSN is empty, defaults to .data/pccp.db
// For Postgres, if DSN is empty, builds from standard PG env vars.
func FromEnv() (*gorm.DB, error) {
	driver := os.Getenv("PCCP_DB_DRIVER")
	if driver == "" {
		driver = "sqlite"
	}

	dsn := os.Getenv("PCCP_DB_DSN")
	if dsn == "" {
		switch driver {
		case "postgres", "postgresql":
			host := getenvDefault("PCCP_PG_HOST", "localhost")
			port := getenvDefault("PCCP_PG_PORT", "5432")
			user := getenvDefault("PCCP_PG_USER", "pccp")
			pass := getenvDefault("PCCP_PG_PASSWORD", "pccp")
			dbname := getenvDefault("PCCP_PG_DB", "pccp")
			sslmode := getenvDefault("PCCP_PG_SSLMODE", "disable")
			dsn = fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
				host, port, user, pass, dbname, sslmode)
		default:
			dsn = ".data/pccp.db"
		}
	}

	db, err := New(Config{Driver: driver, DSN: dsn})
	if err != nil {
		return nil, err
	}

	if err := AutoMigrate(db); err != nil {
		return nil, err
	}

	return db, nil
}

func getenvDefault(key, defaultVal string) string {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	return v
}
