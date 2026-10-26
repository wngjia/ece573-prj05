// Package marketplace contains Postgres-backed marketplace operations.
package marketplace

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var (
	ErrInvalidListing      = errors.New("seller_id, title, and positive credit_cost are required")
	ErrInvalidOrder        = errors.New("listing_id and buyer_id are required")
	ErrInvalidAccount      = errors.New("account_id is required")
	ErrAccountNotFound     = errors.New("account not found")
	ErrListingNotFound     = errors.New("listing not found")
	ErrListingUnavailable  = errors.New("listing is no longer available")
	ErrInsufficientCredit  = errors.New("buyer has insufficient credit")
	ErrSelfPurchase        = errors.New("seller cannot buy their own listing")
	ErrInvalidCreditGrant  = errors.New("account_id and a positive credit amount are required")
	ErrCreditTargetNotUser = errors.New("admin accounts cannot receive credits")
)

// DatabaseConfig contains the Compose-provided Postgres connection settings.
type DatabaseConfig struct {
	Host     string
	Port     string
	Database string
	User     string
	Password string
}

// Store provides marketplace operations backed by Postgres.
type Store struct {
	db *sql.DB
}

// OpenStore connects to Postgres and waits briefly for it to become ready.
func OpenStore(config DatabaseConfig) (*Store, error) {
	connection := fmt.Sprintf(
		"host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		config.Host, config.Port, config.Database, config.User, config.Password,
	)
	db, err := sql.Open("pgx", connection)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	var pingErr error
	for range 30 {
		pingErr = db.Ping()
		if pingErr == nil {
			return &Store{db: db}, nil
		}
		time.Sleep(time.Second)
	}
	db.Close()
	return nil, fmt.Errorf("connect to database: %w", pingErr)
}

// Close releases the database connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

func formatTime(value time.Time) string {
	return value.UTC().Format(time.RFC3339)
}
