package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"ece573-prj05/auth"
	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/argon2"
)

type service struct {
	db      *sql.DB
	private ed25519.PrivateKey
}

func openService() (*service, error) {
	dsn := fmt.Sprintf("host=%s port=%s dbname=%s user=%s password=%s sslmode=disable",
		required("POSTGRES_HOST"), required("POSTGRES_PORT"), required("POSTGRES_DB"),
		required("POSTGRES_USER"), required("POSTGRES_PASSWORD"))
	keyPath := required("JWT_PRIVATE_KEY_FILE")
	adminPasswordPath := required("ADMIN_PASSWORD_FILE")
	private, err := auth.LoadPrivateKey(keyPath)
	if err != nil {
		return nil, err
	}
	password, err := os.ReadFile(adminPasswordPath)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	for range 30 {
		if err = db.Ping(); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		db.Close()
		return nil, err
	}
	if err := bootstrapAdmin(db, strings.TrimSpace(string(password))); err != nil {
		db.Close()
		return nil, err
	}
	return &service{db: db, private: private}, nil
}

func (s *service) Close() error {
	return s.db.Close()
}

func required(name string) string {
	value := os.Getenv(name)
	if value == "" {
		log.Fatalf("FATAL required environment variable %s is not set", name)
	}
	return value
}

func bootstrapAdmin(db *sql.DB, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return fmt.Errorf("admin password: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec("INSERT INTO accounts (account_id) VALUES ('course-admin') ON CONFLICT DO NOTHING")
	if err != nil {
		return err
	}
	_, err = tx.Exec("INSERT INTO credentials (account_id, password_hash, role) VALUES ('course-admin', $1, 'admin') ON CONFLICT DO NOTHING", hash)
	if err != nil {
		return err
	}
	var role, storedHash string
	err = tx.QueryRow("SELECT role, password_hash FROM credentials WHERE account_id = 'course-admin'").Scan(&role, &storedHash)
	if err != nil || role != "admin" || storedHash == "" {
		return errors.New("course-admin exists but is not a bootstrapped admin")
	}
	if !verifyPassword(password, storedHash) {
		if _, err := tx.Exec("UPDATE credentials SET password_hash = $1 WHERE account_id = 'course-admin'", hash); err != nil {
			return err
		}
	}
	return tx.Commit()
}

var (
	errAccountExists      = errors.New("account already exists")
	errInvalidCredentials = errors.New("invalid account or password")
)

func accountInsertError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return errAccountExists
	}
	return err
}

func (s *service) registerAccount(ctx context.Context, accountID, password string) error {
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "INSERT INTO accounts (account_id) VALUES ($1)", accountID); err != nil {
		return accountInsertError(err)
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO credentials (account_id, password_hash, role) VALUES ($1, $2, 'user')", accountID, hash); err != nil {
		return accountInsertError(err)
	}
	return accountInsertError(tx.Commit())
}

func (s *service) authenticate(accountID, password string) (string, error) {
	var encoded, role string
	err := s.db.QueryRow("SELECT password_hash, role FROM credentials WHERE account_id = $1", accountID).Scan(&encoded, &role)
	if errors.Is(err, sql.ErrNoRows) {
		return "", errInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if !verifyPassword(password, encoded) {
		return "", errInvalidCredentials
	}
	return role, nil
}

const (
	hashTime    uint32 = 2
	hashMemory  uint32 = 19 * 1024
	hashThreads uint8  = 1
	hashLength  uint32 = 32
)

func validPassword(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) >= 8
}

func hashPassword(password string) (string, error) {
	if !validPassword(password) {
		return "", errors.New("password must have at least eight characters")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, hashTime, hashMemory, hashThreads, hashLength)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		hashMemory, hashTime, hashThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(hash)), nil
}

func verifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, passes uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &passes, &threads); err != nil {
		return false
	}
	if memory < 19*1024 || memory > 256*1024 || passes < 2 || passes > 10 || threads < 1 || threads > 8 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) != 16 {
		return false
	}
	expected, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(expected) != int(hashLength) {
		return false
	}
	actual := argon2.IDKey([]byte(password), salt, passes, memory, threads, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1
}
