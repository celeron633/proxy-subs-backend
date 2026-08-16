package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

var (
	ErrAlreadyInitialized = errors.New("administrator already initialized")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrDuplicateURLPath   = errors.New("duplicate URL path")
	ErrNotFound           = errors.New("not found")
)

type Store struct {
	db *sql.DB
}

type Subscription struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	URLPath   string `json:"url_path"`
	FilePath  string `json:"file_path"`
	TokenHash []byte `json:"-"`
	TokenHint string `json:"token_hint"`
	Note      string `json:"note"`
	Enabled   bool   `json:"enabled"`
	CreatedAt int64  `json:"created_at"`
	UpdatedAt int64  `json:"updated_at"`
}

type SubscriptionView struct {
	Subscription
	TokenConfigured bool   `json:"token_configured"`
	FileStatus      string `json:"file_status"`
	FileSize        int64  `json:"file_size"`
	FileModifiedAt  int64  `json:"file_modified_at"`
}

func OpenStore(databasePath string) (*Store, error) {
	expandedPath, err := expandPath(databasePath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(expandedPath), 0o750); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	_, statErr := os.Stat(expandedPath)

	db, err := sql.Open("sqlite", expandedPath)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	store := &Store{db: db}
	if err := store.initialize(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	if errors.Is(statErr, os.ErrNotExist) {
		_ = os.Chmod(expandedPath, 0o600)
	}
	return store, nil
}

func (s *Store) initialize(ctx context.Context) error {
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, statement := range pragmas {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("initialize sqlite: %w", err)
		}
	}

	migrations := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS admins (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			username TEXT NOT NULL UNIQUE COLLATE NOCASE,
			password_hash BLOB NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			token_hash BLOB PRIMARY KEY,
			admin_id INTEGER NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
			expires_at INTEGER NOT NULL,
			created_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS sessions_expires_at_idx ON sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`INSERT OR IGNORE INTO settings(key, value, updated_at) VALUES ('api_enabled', 'true', CAST(strftime('%s','now') AS INTEGER))`,
		`CREATE TABLE IF NOT EXISTS subscriptions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			url_path TEXT NOT NULL UNIQUE COLLATE NOCASE,
			file_path TEXT NOT NULL,
			token_hash BLOB NOT NULL,
			token_hint TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, statement := range migrations {
		if _, err := tx.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("apply database schema: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES (1, ?)", time.Now().Unix()); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) Initialized(ctx context.Context) (bool, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins").Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) CreateAdmin(ctx context.Context, username, password string) error {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, "INSERT OR IGNORE INTO admins(id, username, password_hash, created_at, updated_at) VALUES (1, ?, ?, ?, ?)", username, passwordHash, now, now)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrAlreadyInitialized
	}
	return nil
}

func (s *Store) Authenticate(ctx context.Context, username, password string) (string, error) {
	var storedUsername string
	var passwordHash []byte
	err := s.db.QueryRowContext(ctx, "SELECT username, password_hash FROM admins WHERE username = ?", username).Scan(&storedUsername, &passwordHash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidCredentials
	}
	if err != nil {
		return "", err
	}
	if err := bcrypt.CompareHashAndPassword(passwordHash, []byte(password)); err != nil {
		return "", ErrInvalidCredentials
	}
	return storedUsername, nil
}

func (s *Store) CreateSession(ctx context.Context, tokenHash []byte, expiresAt time.Time) error {
	now := time.Now().Unix()
	if _, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE expires_at <= ?", now); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, "INSERT INTO sessions(token_hash, admin_id, expires_at, created_at) VALUES (?, 1, ?, ?)", tokenHash, expiresAt.Unix(), now)
	return err
}

func (s *Store) SessionUsername(ctx context.Context, tokenHash []byte, now time.Time) (string, error) {
	var username string
	err := s.db.QueryRowContext(ctx, `SELECT admins.username
		FROM sessions JOIN admins ON admins.id = sessions.admin_id
		WHERE sessions.token_hash = ? AND sessions.expires_at > ?`, tokenHash, now.Unix()).Scan(&username)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvalidCredentials
	}
	return username, err
}

func (s *Store) DeleteSession(ctx context.Context, tokenHash []byte) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM sessions WHERE token_hash = ?", tokenHash)
	return err
}

func (s *Store) APIEnabled(ctx context.Context) (bool, error) {
	var value string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'api_enabled'").Scan(&value); err != nil {
		return false, err
	}
	return value == "true", nil
}

func (s *Store) SetAPIEnabled(ctx context.Context, enabled bool) error {
	value := "false"
	if enabled {
		value = "true"
	}
	_, err := s.db.ExecContext(ctx, "UPDATE settings SET value = ?, updated_at = ? WHERE key = 'api_enabled'", value, time.Now().Unix())
	return err
}

func (s *Store) ListSubscriptions(ctx context.Context) ([]Subscription, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, url_path, file_path, token_hash, token_hint, note, enabled, created_at, updated_at
		FROM subscriptions ORDER BY created_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var subscriptions []Subscription
	for rows.Next() {
		subscription, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		subscriptions = append(subscriptions, subscription)
	}
	return subscriptions, rows.Err()
}

func (s *Store) SubscriptionByID(ctx context.Context, id int64) (Subscription, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, url_path, file_path, token_hash, token_hint, note, enabled, created_at, updated_at
		FROM subscriptions WHERE id = ?`, id)
	return scanSubscription(row)
}

func (s *Store) SubscriptionByURLPath(ctx context.Context, urlPath string) (Subscription, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, url_path, file_path, token_hash, token_hint, note, enabled, created_at, updated_at
		FROM subscriptions WHERE url_path = ?`, urlPath)
	return scanSubscription(row)
}

func (s *Store) CreateSubscription(ctx context.Context, subscription Subscription) (Subscription, error) {
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, `INSERT INTO subscriptions(name, url_path, file_path, token_hash, token_hint, note, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, subscription.Name, subscription.URLPath, subscription.FilePath, subscription.TokenHash,
		subscription.TokenHint, subscription.Note, subscription.Enabled, now, now)
	if err != nil {
		return Subscription{}, normalizeStoreError(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Subscription{}, err
	}
	return s.SubscriptionByID(ctx, id)
}

func (s *Store) UpdateSubscription(ctx context.Context, subscription Subscription) (Subscription, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE subscriptions
		SET name = ?, url_path = ?, file_path = ?, token_hash = ?, token_hint = ?, note = ?, enabled = ?, updated_at = ?
		WHERE id = ?`, subscription.Name, subscription.URLPath, subscription.FilePath, subscription.TokenHash,
		subscription.TokenHint, subscription.Note, subscription.Enabled, time.Now().Unix(), subscription.ID)
	if err != nil {
		return Subscription{}, normalizeStoreError(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return Subscription{}, err
	}
	if rows == 0 {
		return Subscription{}, ErrNotFound
	}
	return s.SubscriptionByID(ctx, subscription.ID)
}

func (s *Store) DeleteSubscription(ctx context.Context, id int64) error {
	result, err := s.db.ExecContext(ctx, "DELETE FROM subscriptions WHERE id = ?", id)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanSubscription(row scanner) (Subscription, error) {
	var subscription Subscription
	var enabled int
	err := row.Scan(&subscription.ID, &subscription.Name, &subscription.URLPath, &subscription.FilePath, &subscription.TokenHash,
		&subscription.TokenHint, &subscription.Note, &enabled, &subscription.CreatedAt, &subscription.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Subscription{}, ErrNotFound
	}
	subscription.Enabled = enabled == 1
	return subscription, err
}

func normalizeStoreError(err error) error {
	if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: subscriptions.url_path") {
		return ErrDuplicateURLPath
	}
	return err
}
