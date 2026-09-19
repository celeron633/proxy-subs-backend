package main

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResetPasswordCommand(t *testing.T) {
	ctx := context.Background()
	store, err := OpenStore(filepath.Join(t.TempDir(), "reset.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.CreateAdmin(ctx, "admin", "old-password"); err != nil {
		t.Fatal(err)
	}
	tokenHash := []byte("session-token-hash")
	if err := store.CreateSession(ctx, tokenHash, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runResetPassword(ctx, store, strings.NewReader("short\nshort\n"), &out); err == nil {
		t.Fatal("expected short password to be rejected")
	}
	if err := runResetPassword(ctx, store, strings.NewReader("new-password\nmismatch-password\n"), &out); err == nil {
		t.Fatal("expected mismatched confirmation to be rejected")
	}

	out.Reset()
	if err := runResetPassword(ctx, store, strings.NewReader("new-password\nnew-password\n"), &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "admin") {
		t.Fatalf("administrator list missing from output: %s", out.String())
	}
	if _, err := store.Authenticate(ctx, "admin", "old-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password still accepted: %v", err)
	}
	if _, err := store.Authenticate(ctx, "admin", "new-password"); err != nil {
		t.Fatalf("new password rejected: %v", err)
	}
	if _, err := store.SessionUsername(ctx, tokenHash, time.Now()); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("existing session not revoked: %v", err)
	}
}
