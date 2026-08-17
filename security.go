package main

import (
	"context"
	"database/sql"
	"errors"
	"image/color"
	"strings"
	"time"

	"github.com/mojocn/base64Captcha"
)

const (
	securityScopeLogin = "login"
	securityScopeAPI   = "subscription_api"
	securityMaxErrors  = 5
	securityWindow     = 30 * time.Minute
	securityBlockTime  = 30 * time.Minute
)

type CaptchaManager struct {
	captcha *base64Captcha.Captcha
	store   base64Captcha.Store
}

type SecurityLimit struct {
	Blocked      bool
	BlockedUntil time.Time
}

func NewCaptchaManager() *CaptchaManager {
	store := base64Captcha.NewMemoryStore(512, 5*time.Minute)
	driver := base64Captcha.NewDriverString(
		72,
		240,
		7,
		base64Captcha.OptionShowHollowLine|base64Captcha.OptionShowSlimeLine|base64Captcha.OptionShowSineLine,
		7,
		"23456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz",
		&color.RGBA{R: 245, G: 247, B: 248, A: 255},
		nil,
		[]string{},
	)
	return &CaptchaManager{captcha: base64Captcha.NewCaptcha(driver, store), store: store}
}

func (m *CaptchaManager) Generate() (id, image, answer string, err error) {
	return m.captcha.Generate()
}

func (m *CaptchaManager) Verify(id, answer string) bool {
	id = strings.TrimSpace(id)
	answer = strings.TrimSpace(answer)
	if id == "" || answer == "" {
		return false
	}
	return m.store.Verify(id, answer, true)
}

func (s *Store) ProtectionEnabled(ctx context.Context) (bool, error) {
	var value string
	if err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = 'protection_enabled'").Scan(&value); err != nil {
		return false, err
	}
	return value == "true", nil
}

func (s *Store) SetProtectionEnabled(ctx context.Context, enabled bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	value := "false"
	if enabled {
		value = "true"
	}
	if _, err := tx.ExecContext(ctx, "UPDATE settings SET value = ?, updated_at = ? WHERE key = 'protection_enabled'", value, time.Now().Unix()); err != nil {
		return err
	}
	if !enabled {
		if _, err := tx.ExecContext(ctx, "DELETE FROM security_failures"); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) SecurityLimit(ctx context.Context, scope, clientIP string, now time.Time) (SecurityLimit, error) {
	var blockedUntil int64
	err := s.db.QueryRowContext(ctx, "SELECT blocked_until FROM security_failures WHERE scope = ? AND client_ip = ?", scope, clientIP).Scan(&blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		return SecurityLimit{}, nil
	}
	if err != nil {
		return SecurityLimit{}, err
	}
	if blockedUntil > now.Unix() {
		return SecurityLimit{Blocked: true, BlockedUntil: time.Unix(blockedUntil, 0)}, nil
	}
	return SecurityLimit{}, nil
}

func (s *Store) RecordSecurityFailure(ctx context.Context, scope, clientIP string, now time.Time) (SecurityLimit, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return SecurityLimit{}, err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, "DELETE FROM security_failures WHERE updated_at < ?", now.Add(-24*time.Hour).Unix()); err != nil {
		return SecurityLimit{}, err
	}

	var count int
	var firstFailureAt, blockedUntil int64
	err = tx.QueryRowContext(ctx, `SELECT failure_count, first_failure_at, blocked_until
		FROM security_failures WHERE scope = ? AND client_ip = ?`, scope, clientIP).Scan(&count, &firstFailureAt, &blockedUntil)
	if errors.Is(err, sql.ErrNoRows) {
		_, err = tx.ExecContext(ctx, `INSERT INTO security_failures(scope, client_ip, failure_count, first_failure_at, blocked_until, updated_at)
			VALUES (?, ?, 1, ?, 0, ?)`, scope, clientIP, now.Unix(), now.Unix())
		if err != nil {
			return SecurityLimit{}, err
		}
		return SecurityLimit{}, tx.Commit()
	}
	if err != nil {
		return SecurityLimit{}, err
	}

	if blockedUntil > now.Unix() {
		if err := tx.Commit(); err != nil {
			return SecurityLimit{}, err
		}
		return SecurityLimit{Blocked: true, BlockedUntil: time.Unix(blockedUntil, 0)}, nil
	}

	if now.Unix()-firstFailureAt >= int64(securityWindow.Seconds()) || blockedUntil > 0 {
		count = 1
		firstFailureAt = now.Unix()
		blockedUntil = 0
	} else {
		count++
	}
	if count >= securityMaxErrors {
		blockedUntil = now.Add(securityBlockTime).Unix()
	}
	_, err = tx.ExecContext(ctx, `UPDATE security_failures
		SET failure_count = ?, first_failure_at = ?, blocked_until = ?, updated_at = ?
		WHERE scope = ? AND client_ip = ?`, count, firstFailureAt, blockedUntil, now.Unix(), scope, clientIP)
	if err != nil {
		return SecurityLimit{}, err
	}
	if err := tx.Commit(); err != nil {
		return SecurityLimit{}, err
	}
	if blockedUntil > now.Unix() {
		return SecurityLimit{Blocked: true, BlockedUntil: time.Unix(blockedUntil, 0)}, nil
	}
	return SecurityLimit{}, nil
}

func (s *Store) ClearSecurityFailures(ctx context.Context, scope, clientIP string) error {
	_, err := s.db.ExecContext(ctx, "DELETE FROM security_failures WHERE scope = ? AND client_ip = ?", scope, clientIP)
	return err
}
