package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type testClient struct {
	router   http.Handler
	cookie   *http.Cookie
	captcha  *CaptchaManager
	fileRoot string
}

func newTestServer(t *testing.T) (*Store, *testClient) {
	t.Helper()
	store, err := OpenStore(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	fileRoot := t.TempDir()
	server, err := NewSubsServer(store, "web", fileRoot, false)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, &testClient{router: server.Router, captcha: server.Captcha, fileRoot: fileRoot}
}

func (client *testClient) request(t *testing.T, method, path string, body any) *httptest.ResponseRecorder {
	return client.requestFromIP(t, method, path, body, "192.0.2.1")
}

func (client *testClient) requestFromIP(t *testing.T, method, path string, body any, clientIP string) *httptest.ResponseRecorder {
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
	request.RemoteAddr = net.JoinHostPort(clientIP, "1234")
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

func (client *testClient) requestFromTrustedProxy(t *testing.T, method, path, forwardedIP string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, nil)
	request.RemoteAddr = net.JoinHostPort("127.0.0.1", "1234")
	request.Header.Set("X-Forwarded-For", forwardedIP)
	request.Header.Set("X-Forwarded-Proto", "https")
	if client.cookie != nil {
		request.AddCookie(client.cookie)
	}
	response := httptest.NewRecorder()
	client.router.ServeHTTP(response, request)
	return response
}

func (client *testClient) captchaCredentials(t *testing.T) (string, string) {
	t.Helper()
	id, _, answer, err := client.captcha.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return id, answer
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
	wrongCaptchaID, _ := client.captchaCredentials(t)
	wrongCaptcha := client.request(t, http.MethodPost, "/admin/api/login", map[string]any{
		"username": "operator", "password": "long-enough-password",
		"captcha_id": wrongCaptchaID, "captcha_answer": "0000000",
	})
	assertStatus(t, wrongCaptcha, http.StatusUnauthorized)
	badCaptchaID, badCaptchaAnswer := client.captchaCredentials(t)
	badLogin := client.request(t, http.MethodPost, "/admin/api/login", map[string]any{
		"username": "operator", "password": "not-the-password",
		"captcha_id": badCaptchaID, "captcha_answer": badCaptchaAnswer,
	})
	assertStatus(t, badLogin, http.StatusUnauthorized)
	loginCaptchaID, loginCaptchaAnswer := client.captchaCredentials(t)
	login := client.request(t, http.MethodPost, "/admin/api/login", map[string]any{
		"username": "operator", "password": "long-enough-password",
		"captcha_id": loginCaptchaID, "captcha_answer": loginCaptchaAnswer,
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
	protectionEnabled, err := store.ProtectionEnabled(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !protectionEnabled {
		t.Fatal("expected error protection to be enabled by default")
	}
	if err := store.SetAPIEnabled(t.Context(), false); err != nil {
		t.Fatal(err)
	}
	if err := store.SetProtectionEnabled(t.Context(), false); err != nil {
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
	protectionEnabled, err = reopened.ProtectionEnabled(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if protectionEnabled {
		t.Fatal("expected the protection switch to remain disabled after reopening SQLite")
	}
}

func TestCaptchaIsGeneratedAndCanOnlyBeUsedOnce(t *testing.T) {
	_, client := newTestServer(t)
	response := client.request(t, http.MethodGet, "/admin/api/captcha", nil)
	assertStatus(t, response, http.StatusOK)

	var payload map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["id"] == "" || !strings.HasPrefix(payload["image"], "data:image/png;base64,") {
		t.Fatalf("unexpected captcha payload: %#v", payload)
	}
	if _, exposed := payload["answer"]; exposed {
		t.Fatal("captcha answer must not be exposed by the API")
	}

	id, answer := client.captchaCredentials(t)
	if !client.captcha.Verify(id, answer) {
		t.Fatal("expected a fresh captcha answer to be accepted")
	}
	if client.captcha.Verify(id, answer) {
		t.Fatal("expected captcha to be invalid after its first use")
	}
}

func TestSecurityFailureWindowAndBlockExpiry(t *testing.T) {
	store, _ := newTestServer(t)
	startedAt := time.Date(2026, time.August, 17, 12, 0, 0, 0, time.UTC)
	var limit SecurityLimit
	for attempt := 0; attempt < securityMaxErrors; attempt++ {
		failureAt := startedAt.Add(time.Duration(attempt) * time.Minute)
		var err error
		limit, err = store.RecordSecurityFailure(t.Context(), securityScopeLogin, "203.0.113.50", failureAt)
		if err != nil {
			t.Fatal(err)
		}
		if attempt < securityMaxErrors-1 && limit.Blocked {
			t.Fatalf("client was blocked after only %d failures", attempt+1)
		}
	}
	if !limit.Blocked {
		t.Fatal("expected the fifth failure to block the client")
	}
	wantBlockedUntil := startedAt.Add(4*time.Minute + securityBlockTime)
	if !limit.BlockedUntil.Equal(wantBlockedUntil) {
		t.Fatalf("unexpected block expiry: got %s, want %s", limit.BlockedUntil, wantBlockedUntil)
	}

	beforeExpiry, err := store.SecurityLimit(t.Context(), securityScopeLogin, "203.0.113.50", wantBlockedUntil.Add(-time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if !beforeExpiry.Blocked {
		t.Fatal("expected the client to remain blocked before the 30-minute expiry")
	}
	afterExpiry, err := store.SecurityLimit(t.Context(), securityScopeLogin, "203.0.113.50", wantBlockedUntil)
	if err != nil {
		t.Fatal(err)
	}
	if afterExpiry.Blocked {
		t.Fatal("expected the client to be unblocked after 30 minutes")
	}
	reset, err := store.RecordSecurityFailure(t.Context(), securityScopeLogin, "203.0.113.50", wantBlockedUntil)
	if err != nil {
		t.Fatal(err)
	}
	if reset.Blocked {
		t.Fatal("expected a new failure sequence after the block expires")
	}
}

func TestLoginProtectionBlocksTheClientIPAndCanBeDisabled(t *testing.T) {
	_, client := newTestServer(t)
	setup := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "operator", "password": "long-enough-password",
	})
	assertStatus(t, setup, http.StatusOK)
	client.rememberSession(t, setup)
	adminSession := client.cookie

	const blockedIP = "203.0.113.10"
	client.cookie = nil
	for attempt := 1; attempt <= securityMaxErrors; attempt++ {
		captchaID, captchaAnswer := client.captchaCredentials(t)
		response := client.requestFromIP(t, http.MethodPost, "/admin/api/login", map[string]any{
			"username": "operator", "password": "wrong-password",
			"captcha_id": captchaID, "captcha_answer": captchaAnswer,
		}, blockedIP)
		expected := http.StatusUnauthorized
		if attempt == securityMaxErrors {
			expected = http.StatusTooManyRequests
		}
		assertStatus(t, response, expected)
		if expected == http.StatusTooManyRequests && response.Header().Get("Retry-After") == "" {
			t.Fatal("blocked login response is missing Retry-After")
		}
	}

	captchaID, captchaAnswer := client.captchaCredentials(t)
	blockedLogin := client.requestFromIP(t, http.MethodPost, "/admin/api/login", map[string]any{
		"username": "operator", "password": "long-enough-password",
		"captcha_id": captchaID, "captcha_answer": captchaAnswer,
	}, blockedIP)
	assertStatus(t, blockedLogin, http.StatusTooManyRequests)

	client.cookie = adminSession
	settings := client.request(t, http.MethodGet, "/admin/api/settings/security", nil)
	assertStatus(t, settings, http.StatusOK)
	if !strings.Contains(settings.Body.String(), `"enabled":true`) {
		t.Fatalf("expected protection to be enabled: %s", settings.Body.String())
	}
	disable := client.request(t, http.MethodPut, "/admin/api/settings/security", map[string]any{"enabled": false})
	assertStatus(t, disable, http.StatusOK)

	client.cookie = nil
	captchaID, captchaAnswer = client.captchaCredentials(t)
	loginAfterDisable := client.requestFromIP(t, http.MethodPost, "/admin/api/login", map[string]any{
		"username": "operator", "password": "long-enough-password",
		"captcha_id": captchaID, "captcha_answer": captchaAnswer,
	}, blockedIP)
	assertStatus(t, loginAfterDisable, http.StatusOK)
}

func TestSubscriptionAPIProtectionCountsInvalidPathsAndTokens(t *testing.T) {
	_, client := newTestServer(t)
	setup := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "operator", "password": "long-enough-password",
	})
	assertStatus(t, setup, http.StatusOK)
	client.rememberSession(t, setup)

	subscriptionFile := filepath.Join(t.TempDir(), "subscription.yaml")
	if err := os.WriteFile(subscriptionFile, []byte("proxies: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created := client.request(t, http.MethodPost, "/admin/api/subscriptions", map[string]any{
		"name": "Protected", "url_path": "protected", "file_path": subscriptionFile,
		"token": "correct-token", "enabled": true,
	})
	assertStatus(t, created, http.StatusCreated)

	const blockedIP = "198.51.100.10"
	for attempt := 1; attempt < securityMaxErrors; attempt++ {
		response := client.requestFromIP(t, http.MethodGet, "/api/protected?token=wrong-token", nil, blockedIP)
		assertStatus(t, response, http.StatusUnauthorized)
	}
	fifthFailure := client.requestFromIP(t, http.MethodGet, "/api/missing", nil, blockedIP)
	assertStatus(t, fifthFailure, http.StatusTooManyRequests)
	blockedValidRequest := client.requestFromIP(t, http.MethodGet, "/api/protected?token=correct-token", nil, blockedIP)
	assertStatus(t, blockedValidRequest, http.StatusTooManyRequests)

	validFromAnotherIP := client.requestFromIP(t, http.MethodGet, "/api/protected?token=correct-token", nil, "198.51.100.11")
	assertStatus(t, validFromAnotherIP, http.StatusOK)

	const resetIP = "198.51.100.12"
	for attempt := 1; attempt < securityMaxErrors; attempt++ {
		response := client.requestFromIP(t, http.MethodGet, "/api/missing", nil, resetIP)
		assertStatus(t, response, http.StatusNotFound)
	}
	resetBySuccess := client.requestFromIP(t, http.MethodGet, "/api/protected?token=correct-token", nil, resetIP)
	assertStatus(t, resetBySuccess, http.StatusOK)
	afterReset := client.requestFromIP(t, http.MethodGet, "/api/missing", nil, resetIP)
	assertStatus(t, afterReset, http.StatusNotFound)

	const unmatchedIP = "198.51.100.13"
	for attempt := 1; attempt <= securityMaxErrors; attempt++ {
		response := client.requestFromTrustedProxy(t, http.MethodGet, "/api/extra/path", unmatchedIP)
		expected := http.StatusNotFound
		if attempt == securityMaxErrors {
			expected = http.StatusTooManyRequests
		}
		assertStatus(t, response, expected)
	}
}

func TestServerFileBrowserIsAuthenticatedAndRestrictedToRoot(t *testing.T) {
	_, client := newTestServer(t)
	unauthorized := client.request(t, http.MethodGet, "/admin/api/files", nil)
	assertStatus(t, unauthorized, http.StatusUnauthorized)

	setup := client.request(t, http.MethodPost, "/admin/api/setup", map[string]any{
		"username": "operator", "password": "long-enough-password",
	})
	assertStatus(t, setup, http.StatusOK)
	client.rememberSession(t, setup)

	subdirectory := filepath.Join(client.fileRoot, "configs")
	if err := os.Mkdir(subdirectory, 0o750); err != nil {
		t.Fatal(err)
	}
	rootFile := filepath.Join(client.fileRoot, "root.yaml")
	if err := os.WriteFile(rootFile, []byte("root: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(subdirectory, "nested.yaml")
	if err := os.WriteFile(nestedFile, []byte("nested: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	outsideFile := filepath.Join(t.TempDir(), "outside.yaml")
	if err := os.WriteFile(outsideFile, []byte("outside: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	symlinkName := "outside-link.yaml"
	symlinkCreated := os.Symlink(outsideFile, filepath.Join(client.fileRoot, symlinkName)) == nil

	rootResponse := client.request(t, http.MethodGet, "/admin/api/files", nil)
	assertStatus(t, rootResponse, http.StatusOK)
	var rootPayload struct {
		Root         string             `json:"root"`
		CurrentPath  string             `json:"current_path"`
		RelativePath string             `json:"relative_path"`
		ParentPath   string             `json:"parent_path"`
		Entries      []fileBrowserEntry `json:"entries"`
	}
	if err := json.Unmarshal(rootResponse.Body.Bytes(), &rootPayload); err != nil {
		t.Fatal(err)
	}
	if rootPayload.Root != filepath.Clean(client.fileRoot) || rootPayload.CurrentPath != filepath.Clean(client.fileRoot) {
		t.Fatalf("unexpected browser root: %#v", rootPayload)
	}
	if rootPayload.RelativePath != "" || rootPayload.ParentPath != "" {
		t.Fatalf("root directory must not expose a parent: %#v", rootPayload)
	}
	if len(rootPayload.Entries) != 2 {
		t.Fatalf("expected one directory and one file, got %#v", rootPayload.Entries)
	}
	if !rootPayload.Entries[0].IsDirectory || rootPayload.Entries[0].Name != "configs" {
		t.Fatalf("expected directories to be listed first: %#v", rootPayload.Entries)
	}
	if rootPayload.Entries[1].Path != rootFile || rootPayload.Entries[1].RelativePath != "root.yaml" {
		t.Fatalf("unexpected root file entry: %#v", rootPayload.Entries[1])
	}
	if symlinkCreated && strings.Contains(rootResponse.Body.String(), symlinkName) {
		t.Fatal("a symlink outside the configured root was exposed")
	}

	nestedResponse := client.request(t, http.MethodGet, "/admin/api/files?path=configs", nil)
	assertStatus(t, nestedResponse, http.StatusOK)
	var nestedPayload struct {
		RelativePath string             `json:"relative_path"`
		ParentPath   string             `json:"parent_path"`
		Entries      []fileBrowserEntry `json:"entries"`
	}
	if err := json.Unmarshal(nestedResponse.Body.Bytes(), &nestedPayload); err != nil {
		t.Fatal(err)
	}
	if nestedPayload.RelativePath != "configs" || nestedPayload.ParentPath != "" || len(nestedPayload.Entries) != 1 {
		t.Fatalf("unexpected nested directory response: %#v", nestedPayload)
	}
	if nestedPayload.Entries[0].Path != nestedFile || nestedPayload.Entries[0].IsDirectory {
		t.Fatalf("unexpected nested file entry: %#v", nestedPayload.Entries[0])
	}

	traversal := client.request(t, http.MethodGet, "/admin/api/files?path=..", nil)
	assertStatus(t, traversal, http.StatusBadRequest)
	fileAsDirectory := client.request(t, http.MethodGet, "/admin/api/files?path=root.yaml", nil)
	assertStatus(t, fileAsDirectory, http.StatusBadRequest)
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
	settingsRedirect := client.request(t, http.MethodGet, "/settings", nil)
	assertStatus(t, settingsRedirect, http.StatusFound)
	if settingsRedirect.Header().Get("Location") != "/" {
		t.Fatalf("unexpected settings redirect: %s", settingsRedirect.Header().Get("Location"))
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
	if !strings.Contains(dashboard.Body.String(), `id="file-browser-backdrop"`) {
		t.Fatal("dashboard is missing the server file browser")
	}
	settings := client.request(t, http.MethodGet, "/settings", nil)
	assertStatus(t, settings, http.StatusOK)
	if !strings.Contains(settings.Body.String(), "<title>安全设置</title>") {
		t.Fatalf("unexpected settings document: %s", settings.Body.String())
	}

	javascript := client.request(t, http.MethodGet, "/assets/auth.js", nil)
	assertStatus(t, javascript, http.StatusOK)
	if !strings.Contains(javascript.Body.String(), "initializeAuthPage") {
		t.Fatal("authentication JavaScript was not served")
	}
	dashboardScript := client.request(t, http.MethodGet, "/assets/dashboard.js", nil)
	assertStatus(t, dashboardScript, http.StatusOK)
	if !strings.Contains(dashboardScript.Body.String(), "loadFileDirectory") {
		t.Fatal("server file browser JavaScript was not served")
	}
	sharedStyles := client.request(t, http.MethodGet, "/assets/shared.css", nil)
	assertStatus(t, sharedStyles, http.StatusOK)
	if !strings.Contains(sharedStyles.Body.String(), ".button-primary") {
		t.Fatal("shared stylesheet was not served")
	}
	settingsScript := client.request(t, http.MethodGet, "/assets/settings.js", nil)
	assertStatus(t, settingsScript, http.StatusOK)
	if !strings.Contains(settingsScript.Body.String(), "loadSecuritySettings") {
		t.Fatal("settings JavaScript was not served")
	}
	settingsStyles := client.request(t, http.MethodGet, "/assets/settings.css", nil)
	assertStatus(t, settingsStyles, http.StatusOK)
	if !strings.Contains(settingsStyles.Body.String(), ".settings-page") {
		t.Fatal("settings stylesheet was not served")
	}
}

func assertStatus(t *testing.T, response *httptest.ResponseRecorder, expected int) {
	t.Helper()
	if response.Code != expected {
		t.Fatalf("expected status %d, got %d: %s", expected, response.Code, response.Body.String())
	}
}
