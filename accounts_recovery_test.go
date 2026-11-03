package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMultipleAdminStore(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestServer(t)
	if err := store.CreateAdmin(ctx, "admin", "original-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, authenticateTestAdmin(t, store, "admin", "original-password"), hashToken("first-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	second, err := store.AddAdmin(ctx, "second", "second-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(ctx, authenticateTestAdmin(t, store, "second", "second-password"), hashToken("second-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := store.DeleteAdmin(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SessionUsername(ctx, hashToken("first-session"), time.Now()); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("deleted account session survived: %v", err)
	}
	if username, err := store.SessionUsername(ctx, hashToken("second-session"), time.Now()); err != nil || username != "second" {
		t.Fatalf("second session identity: %s %v", username, err)
	}
	if err := store.CreateAdmin(ctx, "replacement", "replacement-password"); !errors.Is(err, ErrAlreadyInitialized) {
		t.Fatalf("setup reopened after deleting id 1: %v", err)
	}
	if err := store.DeleteAdmin(ctx, second.ID); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("last administrator deleted: %v", err)
	}
	var enabled int
	if err := store.db.QueryRow("PRAGMA foreign_keys").Scan(&enabled); err != nil || enabled != 1 {
		t.Fatalf("foreign keys disabled: %d %v", enabled, err)
	}
}

func TestConcurrentAdminDeletionKeepsOneAccount(t *testing.T) {
	store, _ := newTestServer(t)
	ctx := context.Background()
	if err := store.CreateAdmin(ctx, "first", "first-password"); err != nil {
		t.Fatal(err)
	}
	second, err := store.AddAdmin(ctx, "second", "second-password")
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, id := range []int64{1, second.ID} {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			err := store.DeleteAdmin(ctx, id)
			if err != nil && !errors.Is(err, ErrLastAdmin) {
				t.Errorf("delete: %v", err)
			}
		}(id)
	}
	wg.Wait()
	users, err := store.ListAdmins(ctx)
	if err != nil || len(users) != 1 {
		t.Fatalf("expected one remaining admin: %v %v", users, err)
	}
}

func setupAccountClient(t *testing.T, client *testClient) {
	t.Helper()
	response := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{"username": "admin", "password": "original-password"})
	assertStatus(t, response, http.StatusOK)
	client.rememberSession(t, response)
}

func TestUserManagementAndSessionOwnership(t *testing.T) {
	store, client := newTestServer(t)
	for _, endpoint := range []struct{ method, path string }{{"GET", "/admin/api/users"}, {"POST", "/admin/api/users"}, {"PUT", "/admin/api/users/1/password"}, {"DELETE", "/admin/api/users/1"}} {
		assertStatus(t, client.request(t, endpoint.method, endpoint.path, map[string]any{}), http.StatusUnauthorized)
	}
	assertStatus(t, client.request(t, "GET", "/users", nil), http.StatusFound)
	setupAccountClient(t, client)
	created := client.request(t, "POST", "/admin/api/users", map[string]any{"username": "operator", "password": "operator-password"})
	assertStatus(t, created, http.StatusCreated)
	if strings.Contains(created.Body.String(), "password") {
		t.Fatal("user response contains password material")
	}
	assertStatus(t, client.request(t, "POST", "/admin/api/users", map[string]any{"username": "OPERATOR", "password": "another-password"}), http.StatusConflict)
	assertStatus(t, client.request(t, "POST", "/admin/api/users", map[string]any{"username": "short", "password": "short"}), http.StatusBadRequest)
	other := &testClient{server: client.server, router: client.router, captcha: client.captcha}
	id, answer := client.captchaCredentials(t)
	login := other.request(t, "POST", "/admin/api/login", map[string]any{"username": "operator", "password": "operator-password", "captcha_id": id, "captcha_answer": answer})
	assertStatus(t, login, http.StatusOK)
	other.rememberSession(t, login)
	dashboard := other.request(t, "GET", "/admin/api/dashboard", nil)
	if !strings.Contains(dashboard.Body.String(), `"username":"operator"`) {
		t.Fatalf("second user bound to wrong account: %s", dashboard.Body.String())
	}
	assertStatus(t, other.request(t, "GET", "/admin/api/users", nil), http.StatusOK)
	assertStatus(t, client.request(t, "DELETE", "/admin/api/users/1", nil), http.StatusBadRequest)
	assertStatus(t, client.request(t, "PUT", "/admin/api/users/2/password", map[string]any{"password": "operator-new-password"}), http.StatusOK)
	assertStatus(t, other.request(t, "GET", "/admin/api/users", nil), http.StatusUnauthorized)
	if _, err := store.Authenticate(context.Background(), "operator", "operator-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("old password still valid")
	}
	if _, err := store.Authenticate(context.Background(), "operator", "operator-new-password"); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(context.Background(), authenticateTestAdmin(t, store, "operator", "operator-new-password"), hashToken("deleted-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	assertStatus(t, client.request(t, "DELETE", "/admin/api/users/2", nil), http.StatusNoContent)
	if _, err := store.SessionUsername(context.Background(), hashToken("deleted-session"), time.Now()); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("deleted user's session survived")
	}
	assertStatus(t, client.request(t, "GET", "/admin/api/users", nil), http.StatusOK)
}

func recoveryTestServer(t *testing.T) (*Store, *testClient, string) {
	t.Helper()
	store, client := newTestServer(t)
	path := filepath.Join(client.fileRoot, "recovery.txt")
	client.server.Recovery = NewRecoveryManager(path)
	setupAccountClient(t, client)
	return store, client, path
}

func issueRecovery(t *testing.T, client *testClient, username string) {
	t.Helper()
	id, answer := client.captchaCredentials(t)
	response := client.request(t, "POST", "/admin/api/recovery/request", map[string]any{"username": username, "captcha_id": id, "captcha_answer": answer})
	assertStatus(t, response, http.StatusAccepted)
}

func readRecoveryCode(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	code := strings.TrimSpace(string(data))
	if len(code) != 43 {
		t.Fatalf("unexpected recovery code length: %d", len(code))
	}
	return code
}

func TestRecoveryCaptchaResetAndReplay(t *testing.T) {
	store, client, path := recoveryTestServer(t)
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/request", map[string]any{"username": "admin", "captcha_id": "bad", "captcha_answer": "bad"}), http.StatusBadRequest)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file created without captcha")
	}
	issueRecovery(t, client, "unknown-user")
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("file created for unknown user")
	}
	issueRecovery(t, client, "admin")
	code := readRecoveryCode(t, path)
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("recovery file permissions: %v", info.Mode())
		}
	}
	issueRecovery(t, client, "admin")
	if readRecoveryCode(t, path) != code {
		t.Fatal("pending code overwritten")
	}
	reset := map[string]any{"username": "admin", "recovery_code": "wrong", "password": "recovered-password"}
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", reset), http.StatusBadRequest)
	reset["recovery_code"] = code
	reset["username"] = "unknown-user"
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", reset), http.StatusBadRequest)
	reset["username"] = "admin"
	reset["password"] = "short"
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", reset), http.StatusBadRequest)
	reset["password"] = "recovered-password"
	// Prove recovery does not require an existing login session.
	client.cookie = nil
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", reset), http.StatusOK)
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("used recovery file was not removed")
	}
	if _, err := store.Authenticate(context.Background(), "admin", "recovered-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Authenticate(context.Background(), "admin", "original-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("old password survived reset")
	}
	var sessions int
	if err := store.db.QueryRow("SELECT COUNT(*) FROM sessions WHERE admin_id = 1").Scan(&sessions); err != nil || sessions != 0 {
		t.Fatal("sessions survived recovery")
	}
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", reset), http.StatusBadRequest)
}

func TestRecoveryExpiryAndPasswordChangeInvalidation(t *testing.T) {
	for _, scenario := range []string{"expired", "password-changed"} {
		t.Run(scenario, func(t *testing.T) {
			store, client, path := recoveryTestServer(t)
			issueRecovery(t, client, "admin")
			code := readRecoveryCode(t, path)
			if scenario == "expired" {
				m := client.server.Recovery
				m.mu.Lock()
				m.challenge.expiresAt = time.Now().Add(-time.Minute)
				m.mu.Unlock()
			} else if err := store.ResetAdminPassword(context.Background(), 1, "changed-elsewhere"); err != nil {
				t.Fatal(err)
			}
			assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", map[string]any{"username": "admin", "recovery_code": code, "password": "recovered-password"}), http.StatusBadRequest)
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalidated recovery file remains")
			}
		})
	}
}

func TestRecoveryLimitsAndFileIsolation(t *testing.T) {
	store, client, path := recoveryTestServer(t)
	issueRecovery(t, client, "admin")
	code := readRecoveryCode(t, path)
	listing := client.request(t, "GET", "/admin/api/files", nil)
	if strings.Contains(listing.Body.String(), "recovery.txt") {
		t.Fatal("recovery file exposed by browser")
	}
	created := client.request(t, "POST", "/admin/api/subscriptions", map[string]any{"name": "blocked", "url_path": "blocked", "file_path": path, "token": "subscription-token"})
	assertStatus(t, created, http.StatusCreated)
	assertStatus(t, client.request(t, "GET", "/api/blocked?token=subscription-token", nil), http.StatusForbidden)
	alias := filepath.Join(client.fileRoot, "alias.yaml")
	if err := os.Link(path, alias); err == nil {
		assertStatus(t, client.request(t, "POST", "/admin/api/subscriptions", map[string]any{"name": "alias", "url_path": "alias", "file_path": alias, "token": "subscription-token"}), http.StatusCreated)
		assertStatus(t, client.request(t, "GET", "/api/alias?token=subscription-token", nil), http.StatusForbidden)
		if strings.Contains(client.request(t, "GET", "/admin/api/files", nil).Body.String(), "alias.yaml") {
			t.Fatal("recovery hard link exposed by file browser")
		}
	} else {
		t.Logf("hard links unavailable: %v", err)
	}
	assertStatus(t, client.request(t, "GET", "/assets/recovery.txt", nil), http.StatusNotFound)
	assertStatus(t, client.request(t, "GET", "/recovery.txt", nil), http.StatusNotFound)
	if err := store.SetProtectionEnabled(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= securityMaxErrors; attempt++ {
		expected := http.StatusBadRequest
		if attempt == securityMaxErrors {
			expected = http.StatusTooManyRequests
		}
		response := client.request(t, "POST", "/admin/api/recovery/reset", map[string]any{"username": "admin", "recovery_code": "wrong", "password": "new-password"})
		assertStatus(t, response, expected)
		if strings.Contains(response.Body.String(), code) {
			t.Fatal("response exposed recovery code")
		}
	}
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", map[string]any{"username": "admin", "recovery_code": code, "password": "new-password"}), http.StatusTooManyRequests)
}

func TestRecoveryDoesNotOverwriteExistingFile(t *testing.T) {
	_, client, path := recoveryTestServer(t)
	if err := os.WriteFile(path, []byte("existing file"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, answer := client.captchaCredentials(t)
	response := client.request(t, "POST", "/admin/api/recovery/request", map[string]any{"username": "admin", "captcha_id": id, "captcha_answer": answer})
	assertStatus(t, response, http.StatusServiceUnavailable)
	data, _ := os.ReadFile(path)
	if string(data) != "existing file" {
		t.Fatal("existing file overwritten")
	}
}

func TestRecoveryIsAccountBound(t *testing.T) {
	store, client, path := recoveryTestServer(t)
	second, err := store.AddAdmin(context.Background(), "second", "second-password")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateSession(context.Background(), authenticateTestAdmin(t, store, "second", "second-password"), hashToken("second-session"), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	issueRecovery(t, client, "second")
	code := readRecoveryCode(t, path)
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", map[string]any{"username": "admin", "recovery_code": code, "password": "new-password"}), http.StatusBadRequest)
	assertStatus(t, client.request(t, "POST", "/admin/api/recovery/reset", map[string]any{"username": "second", "recovery_code": code, "password": "new-password"}), http.StatusOK)
	if _, err := store.SessionUsername(context.Background(), hashToken("second-session"), time.Now()); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatal("second session survived reset")
	}
	assertStatus(t, client.request(t, "GET", "/admin/api/users", nil), http.StatusOK)
	users := client.request(t, "GET", "/admin/api/users", nil)
	var payload struct {
		Users []Admin `json:"users"`
	}
	if err := json.Unmarshal(users.Body.Bytes(), &payload); err != nil || len(payload.Users) != 2 || payload.Users[1].ID != second.ID {
		t.Fatalf("list users: %s %v", users.Body.String(), err)
	}
}

func authenticateTestAdmin(t *testing.T, store *Store, username, password string) authenticatedAdmin {
	t.Helper()
	admin, err := store.Authenticate(context.Background(), username, password)
	if err != nil {
		t.Fatal(err)
	}
	return admin
}

func TestPasswordResetBlocksInFlightLogin(t *testing.T) {
	store, client, _ := recoveryTestServer(t)
	admin := authenticateTestAdmin(t, store, "admin", "original-password")
	assertStatus(t, client.request(t, "PUT", "/admin/api/users/1/password", map[string]any{"password": "changed-password"}), http.StatusOK)
	if err := store.CreateSession(context.Background(), admin, hashToken("stale-login"), time.Now().Add(time.Hour)); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("stale authenticated credentials created a session: %v", err)
	}
}

func TestConcurrentRecoveryCanOnlySucceedOnce(t *testing.T) {
	store, client, path := recoveryTestServer(t)
	issueRecovery(t, client, "admin")
	code := readRecoveryCode(t, path)
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			results <- client.server.Recovery.Reset(context.Background(), store, "admin", code, "recovered-password")
		}()
	}
	succeeded := 0
	for i := 0; i < 2; i++ {
		err := <-results
		if err == nil {
			succeeded++
		} else if !errors.Is(err, ErrInvalidRecovery) {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly one reset, got %d", succeeded)
	}
}
