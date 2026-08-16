package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type testClient struct {
	router http.Handler
	cookie *http.Cookie
}

func newTestServer(t *testing.T) (*Store, *testClient) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewSubsServer(store, "web")
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, &testClient{router: server.Router}
}

func (client *testClient) request(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		reader = bytes.NewReader(encoded)
	}
	request := httptest.NewRequest(method, path, reader)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if client.cookie != nil {
		request.AddCookie(client.cookie)
	}
	response := httptest.NewRecorder()
	client.router.ServeHTTP(response, request)
	return response
}

func (client *testClient) rememberSession(t *testing.T, response *httptest.ResponseRecorder) {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == sessionCookieName {
			client.cookie = cookie
			return
		}
	}
	t.Fatal("session cookie was not set")
}

func TestFirstRunAuthenticationAndSubscriptionFlow(t *testing.T) {
	_, client := newTestServer(t)

	status := client.request(t, http.MethodGet, "/admin/api/status", nil)
	assertStatus(t, status, http.StatusOK)
	if !strings.Contains(status.Body.String(), `"initialized":false`) {
		t.Fatalf("unexpected initial status: %s", status.Body.String())
	}

	unauthorized := client.request(t, http.MethodGet, "/admin/api/dashboard", nil)
	assertStatus(t, unauthorized, http.StatusUnauthorized)

	weakPassword := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "admin", "password": "short",
	})
	assertStatus(t, weakPassword, http.StatusBadRequest)

	setup := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "admin", "password": "correct horse battery staple",
	})
	assertStatus(t, setup, http.StatusOK)
	client.rememberSession(t, setup)
	if !client.cookie.HttpOnly || client.cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie is missing security attributes: %#v", client.cookie)
	}

	secondSetup := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "other", "password": "another password",
	})
	assertStatus(t, secondSetup, http.StatusConflict)

	subscriptionFile := filepath.Join(t.TempDir(), "clash.yaml")
	const fileContents = "proxies:\n  - name: test\n"
	if err := os.WriteFile(subscriptionFile, []byte(fileContents), 0o600); err != nil {
		t.Fatal(err)
	}

	created := client.request(t, http.MethodPost, "/admin/api/subscriptions", map[string]any{
		"name": "Clash 主配置", "url_path": "clash-main", "file_path": subscriptionFile,
		"token": "subscription-secret", "note": "测试订阅", "enabled": true,
	})
	assertStatus(t, created, http.StatusCreated)
	if strings.Contains(created.Body.String(), "token_hash") {
		t.Fatalf("token hash leaked in response: %s", created.Body.String())
	}

	wrongToken := client.request(t, http.MethodGet, "/api/clash-main?token=wrong-secret", nil)
	assertStatus(t, wrongToken, http.StatusUnauthorized)

	download := client.request(t, http.MethodGet, "/api/clash-main?token=subscription-secret", nil)
	assertStatus(t, download, http.StatusOK)
	if download.Body.String() != fileContents {
		t.Fatalf("unexpected subscription contents: %q", download.Body.String())
	}

	bearerRequest := httptest.NewRequest(http.MethodGet, "/api/clash-main", nil)
	bearerRequest.Header.Set("Authorization", "Bearer subscription-secret")
	bearerResponse := httptest.NewRecorder()
	client.router.ServeHTTP(bearerResponse, bearerRequest)
	assertStatus(t, bearerResponse, http.StatusOK)

	disable := client.request(t, http.MethodPut, "/admin/api/switch", map[string]any{"enabled": false})
	assertStatus(t, disable, http.StatusOK)
	paused := client.request(t, http.MethodGet, "/api/clash-main?token=subscription-secret", nil)
	assertStatus(t, paused, http.StatusServiceUnavailable)
}

func TestLoginAndUniqueURLPath(t *testing.T) {
	_, client := newTestServer(t)
	setup := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "operator", "password": "long-enough-password",
	})
	assertStatus(t, setup, http.StatusOK)

	client.cookie = nil
	badLogin := client.request(t, http.MethodPost, "/admin/api/login", map[string]any{
		"username": "operator", "password": "not-the-password",
	})
	assertStatus(t, badLogin, http.StatusUnauthorized)
	login := client.request(t, http.MethodPost, "/admin/api/login", map[string]any{
		"username": "operator", "password": "long-enough-password",
	})
	assertStatus(t, login, http.StatusOK)
	client.rememberSession(t, login)

	first := client.request(t, http.MethodPost, "/admin/api/subscriptions", map[string]any{
		"name": "First", "url_path": "shared", "file_path": "/tmp/first", "enabled": true,
	})
	assertStatus(t, first, http.StatusCreated)
	var payload struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Token) < 20 {
		t.Fatalf("expected a generated token, got %q", payload.Token)
	}

	duplicate := client.request(t, http.MethodPost, "/admin/api/subscriptions", map[string]any{
		"name": "Second", "url_path": "SHARED", "file_path": "/tmp/second", "enabled": true,
	})
	assertStatus(t, duplicate, http.StatusConflict)
}

func TestCrossOriginMutationIsRejected(t *testing.T) {
	_, client := newTestServer(t)
	request := httptest.NewRequest(http.MethodPost, "/admin/api/setup", strings.NewReader(`{"username":"admin","password":"secure-password"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	client.router.ServeHTTP(response, request)
	assertStatus(t, response, http.StatusForbidden)
}

func TestSQLiteSettingsPersistAfterReopen(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "persistent.db")
	store, err := OpenStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetAPIEnabled(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenStore(databasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	enabled, err := reopened.APIEnabled(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if enabled {
		t.Fatal("expected the API switch to remain disabled after reopening SQLite")
	}
}

func TestWebConsoleAndAssetsAreServedFromDisk(t *testing.T) {
	_, client := newTestServer(t)
	index := client.request(t, http.MethodGet, "/", nil)
	assertStatus(t, index, http.StatusOK)
	if !strings.Contains(index.Body.String(), "<title>管理后台</title>") {
		t.Fatalf("unexpected index document: %s", index.Body.String())
	}
	for _, sensitiveCopy := range []string{"Proxy Subs", "订阅管理", "token"} {
		if strings.Contains(index.Body.String(), sensitiveCopy) {
			t.Fatalf("login page exposes product purpose %q", sensitiveCopy)
		}
	}
	if index.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("expected a content security policy")
	}

	dashboardRedirect := client.request(t, http.MethodGet, "/dashboard", nil)
	assertStatus(t, dashboardRedirect, http.StatusFound)
	if dashboardRedirect.Header().Get("Location") != "/" {
		t.Fatalf("unexpected dashboard redirect: %s", dashboardRedirect.Header().Get("Location"))
	}

	setup := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "webadmin", "password": "web-console-password",
	})
	assertStatus(t, setup, http.StatusOK)
	client.rememberSession(t, setup)
	authenticatedRoot := client.request(t, http.MethodGet, "/", nil)
	assertStatus(t, authenticatedRoot, http.StatusFound)
	if authenticatedRoot.Header().Get("Location") != "/dashboard" {
		t.Fatalf("unexpected authenticated redirect: %s", authenticatedRoot.Header().Get("Location"))
	}
	dashboard := client.request(t, http.MethodGet, "/dashboard", nil)
	assertStatus(t, dashboard, http.StatusOK)
	if !strings.Contains(dashboard.Body.String(), "<title>订阅管理</title>") {
		t.Fatalf("unexpected dashboard document: %s", dashboard.Body.String())
	}

	javascript := client.request(t, http.MethodGet, "/assets/auth.js", nil)
	assertStatus(t, javascript, http.StatusOK)
	if !strings.Contains(javascript.Body.String(), "initializeAuthPage") {
		t.Fatal("authentication JavaScript was not served")
	}
	sharedStyles := client.request(t, http.MethodGet, "/assets/shared.css", nil)
	assertStatus(t, sharedStyles, http.StatusOK)
	if !strings.Contains(sharedStyles.Body.String(), ".button-primary") {
		t.Fatal("shared stylesheet was not served")
	}
}

func assertStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("expected status %d, got %d: %s", expected, response.Code, response.Body.String())
	}
}
