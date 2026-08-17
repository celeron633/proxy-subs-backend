package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const sessionCookieName = "proxy_subs_session"

type SubsServer struct {
	Router   *gin.Engine
	Store    *Store
	Captcha  *CaptchaManager
	webDir   string
	fileRoot string
}

type credentialsRequest struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	CaptchaID     string `json:"captcha_id"`
	CaptchaAnswer string `json:"captcha_answer"`
}

type switchRequest struct {
	Enabled *bool `json:"enabled"`
}

type subscriptionRequest struct {
	Name     string `json:"name"`
	URLPath  string `json:"url_path"`
	FilePath string `json:"file_path"`
	Token    string `json:"token"`
	Note     string `json:"note"`
	Enabled  *bool  `json:"enabled"`
}

func setReleaseMode() {
	gin.SetMode(gin.ReleaseMode)
}

func NewSubsServer(store *Store, webDir, fileRoot string, requestLogging bool) (*SubsServer, error) {
	if store == nil {
		return nil, errors.New("store is required")
	}
	for _, page := range []string{"index.html", "dashboard.html", "settings.html"} {
		pagePath := filepath.Join(webDir, page)
		if info, err := os.Stat(pagePath); err != nil || info.IsDir() {
			return nil, fmt.Errorf("web page not found at %s", pagePath)
		}
	}
	resolvedFileRoot, err := resolveFileRoot(fileRoot)
	if err != nil {
		return nil, fmt.Errorf("resolve file browser root: %w", err)
	}

	router := gin.New()
	if err := router.SetTrustedProxies([]string{"127.0.0.1", "::1"}); err != nil {
		return nil, fmt.Errorf("configure trusted proxies: %w", err)
	}
	if requestLogging {
		router.Use(gin.Logger())
	}
	router.Use(gin.Recovery(), securityHeaders())
	server := &SubsServer{Router: router, Store: store, Captcha: NewCaptchaManager(), webDir: webDir, fileRoot: resolvedFileRoot}
	server.initRoutes()
	return server, nil
}

func (s *SubsServer) StartServer(listenAddr string) error {
	return s.Router.Run(listenAddr)
}

func (s *SubsServer) initRoutes() {
	s.Router.Static("/assets", filepath.Join(s.webDir, "assets"))
	s.Router.GET("/favicon.ico", func(c *gin.Context) {
		c.File(filepath.Join("static", "favicon.ico"))
	})
	s.Router.GET("/", func(c *gin.Context) {
		if _, authenticated := s.currentUsername(c); authenticated {
			c.Redirect(http.StatusFound, "/dashboard")
			return
		}
		c.Header("Cache-Control", "no-cache")
		c.File(filepath.Join(s.webDir, "index.html"))
	})
	s.Router.GET("/dashboard", func(c *gin.Context) {
		if _, authenticated := s.currentUsername(c); !authenticated {
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.Header("Cache-Control", "no-store")
		c.File(filepath.Join(s.webDir, "dashboard.html"))
	})
	s.Router.GET("/settings", func(c *gin.Context) {
		if _, authenticated := s.currentUsername(c); !authenticated {
			c.Redirect(http.StatusFound, "/")
			return
		}
		c.Header("Cache-Control", "no-store")
		c.File(filepath.Join(s.webDir, "settings.html"))
	})

	// Keep the existing public subscription URL shape, but match URL paths exactly.
	s.Router.GET("/api/:apiPath", s.subscriptionHandler)
	s.Router.NoRoute(s.notFoundHandler)

	adminAPI := s.Router.Group("/admin/api")
	adminAPI.Use(noStore(), sameOrigin())
	adminAPI.GET("/status", s.statusHandler)
	adminAPI.GET("/captcha", s.captchaHandler)
	adminAPI.POST("/setup", s.setupHandler)
	adminAPI.POST("/login", s.loginHandler)

	authenticated := adminAPI.Group("")
	authenticated.Use(s.requireSession())
	authenticated.POST("/logout", s.logoutHandler)
	authenticated.GET("/dashboard", s.dashboardHandler)
	authenticated.GET("/files", s.fileBrowserHandler)
	authenticated.PUT("/switch", s.switchHandler)
	authenticated.GET("/settings/security", s.securitySettingsHandler)
	authenticated.PUT("/settings/security", s.updateSecuritySettingsHandler)
	authenticated.POST("/subscriptions", s.createSubscriptionHandler)
	authenticated.PUT("/subscriptions/:id", s.updateSubscriptionHandler)
	authenticated.DELETE("/subscriptions/:id", s.deleteSubscriptionHandler)
}

func (s *SubsServer) statusHandler(c *gin.Context) {
	initialized, err := s.Store.Initialized(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	username, authenticated := s.currentUsername(c)
	c.JSON(http.StatusOK, gin.H{
		"initialized":   initialized,
		"authenticated": authenticated,
		"username":      username,
	})
}

func (s *SubsServer) captchaHandler(c *gin.Context) {
	id, image, _, err := s.Captcha.Generate()
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id, "image": image})
}

func (s *SubsServer) setupHandler(c *gin.Context) {
	var request credentialsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		badRequest(c, "请输入用户名和密码")
		return
	}
	if err := validateCredentials(request.Username, request.Password); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := s.Store.CreateAdmin(c.Request.Context(), strings.TrimSpace(request.Username), request.Password); err != nil {
		if errors.Is(err, ErrAlreadyInitialized) {
			c.JSON(http.StatusConflict, gin.H{"error": "管理员已经初始化，请直接登录"})
			return
		}
		internalError(c, err)
		return
	}
	s.startSession(c, strings.TrimSpace(request.Username))
}

func (s *SubsServer) loginHandler(c *gin.Context) {
	clientIP := c.ClientIP()
	if s.rejectBlockedClient(c, securityScopeLogin, clientIP) {
		return
	}

	var request credentialsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		s.recordProtectedFailureWithStatus(c, securityScopeLogin, clientIP, http.StatusBadRequest, "请输入用户名、密码和验证码")
		return
	}
	if !s.Captcha.Verify(request.CaptchaID, request.CaptchaAnswer) {
		s.recordProtectedFailure(c, securityScopeLogin, clientIP, "验证码错误，请重新输入")
		return
	}
	username, err := s.Store.Authenticate(c.Request.Context(), strings.TrimSpace(request.Username), request.Password)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			s.recordProtectedFailure(c, securityScopeLogin, clientIP, "用户名或密码不正确")
			return
		}
		internalError(c, err)
		return
	}
	if err := s.clearProtectedFailures(c, securityScopeLogin, clientIP); err != nil {
		internalError(c, err)
		return
	}
	s.startSession(c, username)
}

func (s *SubsServer) startSession(c *gin.Context, username string) {
	rawToken, err := randomToken(32)
	if err != nil {
		internalError(c, err)
		return
	}
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	if err := s.Store.CreateSession(c.Request.Context(), hashToken(rawToken), expiresAt); err != nil {
		internalError(c, err)
		return
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookieName, rawToken, int((7 * 24 * time.Hour).Seconds()), "/", "", requestIsHTTPS(c), true)
	c.JSON(http.StatusOK, gin.H{"username": username})
}

func (s *SubsServer) logoutHandler(c *gin.Context) {
	if rawToken, err := c.Cookie(sessionCookieName); err == nil {
		_ = s.Store.DeleteSession(c.Request.Context(), hashToken(rawToken))
	}
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(sessionCookieName, "", -1, "/", "", requestIsHTTPS(c), true)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *SubsServer) dashboardHandler(c *gin.Context) {
	enabled, err := s.Store.APIEnabled(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	subscriptions, err := s.Store.ListSubscriptions(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	views := make([]SubscriptionView, 0, len(subscriptions))
	for _, subscription := range subscriptions {
		views = append(views, makeSubscriptionView(subscription))
	}
	c.JSON(http.StatusOK, gin.H{
		"username":      c.GetString("username"),
		"api_enabled":   enabled,
		"subscriptions": views,
	})
}

func (s *SubsServer) switchHandler(c *gin.Context) {
	var request switchRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		badRequest(c, "开关状态无效")
		return
	}
	if err := s.Store.SetAPIEnabled(c.Request.Context(), *request.Enabled); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": *request.Enabled})
}

func (s *SubsServer) securitySettingsHandler(c *gin.Context) {
	enabled, err := s.Store.ProtectionEnabled(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled":             enabled,
		"max_errors":          securityMaxErrors,
		"window_minutes":      int(securityWindow.Minutes()),
		"block_minutes":       int(securityBlockTime.Minutes()),
		"captcha_length":      7,
		"captcha_expiry_mins": 5,
	})
}

func (s *SubsServer) updateSecuritySettingsHandler(c *gin.Context) {
	var request switchRequest
	if err := c.ShouldBindJSON(&request); err != nil || request.Enabled == nil {
		badRequest(c, "保护开关状态无效")
		return
	}
	if err := s.Store.SetProtectionEnabled(c.Request.Context(), *request.Enabled); err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"enabled": *request.Enabled})
}

func (s *SubsServer) createSubscriptionHandler(c *gin.Context) {
	var request subscriptionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		badRequest(c, "订阅配置格式不正确")
		return
	}
	subscription, plainToken, err := subscriptionFromRequest(request, nil)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	created, err := s.Store.CreateSubscription(c.Request.Context(), subscription)
	if err != nil {
		handleStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"subscription": makeSubscriptionView(created), "token": plainToken})
}

func (s *SubsServer) updateSubscriptionHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(c, "订阅 ID 无效")
		return
	}
	existing, err := s.Store.SubscriptionByID(c.Request.Context(), id)
	if err != nil {
		handleStoreError(c, err)
		return
	}
	var request subscriptionRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		badRequest(c, "订阅配置格式不正确")
		return
	}
	updated, plainToken, err := subscriptionFromRequest(request, &existing)
	if err != nil {
		badRequest(c, err.Error())
		return
	}
	updated.ID = id
	updated, err = s.Store.UpdateSubscription(c.Request.Context(), updated)
	if err != nil {
		handleStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"subscription": makeSubscriptionView(updated), "token": plainToken})
}

func (s *SubsServer) deleteSubscriptionHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(c, "订阅 ID 无效")
		return
	}
	if err := s.Store.DeleteSubscription(c.Request.Context(), id); err != nil {
		handleStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (s *SubsServer) subscriptionHandler(c *gin.Context) {
	enabled, err := s.Store.APIEnabled(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	if !enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "订阅服务当前已暂停"})
		return
	}
	clientIP := c.ClientIP()
	if s.rejectBlockedClient(c, securityScopeAPI, clientIP) {
		return
	}

	subscription, err := s.Store.SubscriptionByURLPath(c.Request.Context(), c.Param("apiPath"))
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.recordProtectedFailureWithStatus(c, securityScopeAPI, clientIP, http.StatusNotFound, "订阅不存在")
			return
		}
		internalError(c, err)
		return
	}
	if !subscription.Enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "该订阅当前已停用"})
		return
	}

	token := c.Query("token")
	if token == "" {
		if authorization := c.GetHeader("Authorization"); strings.HasPrefix(authorization, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer "))
		}
	}
	candidate := sha256.Sum256([]byte(token))
	if token == "" || subtle.ConstantTimeCompare(candidate[:], subscription.TokenHash) != 1 {
		s.recordProtectedFailure(c, securityScopeAPI, clientIP, "token 无效")
		return
	}
	if err := s.clearProtectedFailures(c, securityScopeAPI, clientIP); err != nil {
		internalError(c, err)
		return
	}

	expandedPath, err := expandPath(subscription.FilePath)
	if err != nil {
		log.Printf("expand subscription file path for id %d: %v", subscription.ID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "订阅文件路径无效"})
		return
	}
	info, err := os.Stat(expandedPath)
	if err != nil || info.IsDir() {
		log.Printf("read subscription file for id %d: %v", subscription.ID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "订阅文件不存在或不可读取"})
		return
	}
	c.FileAttachment(expandedPath, filepath.Base(expandedPath))
}

func (s *SubsServer) notFoundHandler(c *gin.Context) {
	if c.Request.URL.Path != "/api" && !strings.HasPrefix(c.Request.URL.Path, "/api/") {
		c.Status(http.StatusNotFound)
		return
	}

	enabled, err := s.Store.APIEnabled(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	if !enabled {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "订阅服务当前已暂停"})
		return
	}

	clientIP := c.ClientIP()
	if s.rejectBlockedClient(c, securityScopeAPI, clientIP) {
		return
	}
	s.recordProtectedFailureWithStatus(c, securityScopeAPI, clientIP, http.StatusNotFound, "订阅不存在")
}

func (s *SubsServer) rejectBlockedClient(c *gin.Context, scope, clientIP string) bool {
	enabled, err := s.Store.ProtectionEnabled(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return true
	}
	if !enabled {
		return false
	}
	limit, err := s.Store.SecurityLimit(c.Request.Context(), scope, clientIP, time.Now())
	if err != nil {
		internalError(c, err)
		return true
	}
	if limit.Blocked {
		writeRateLimitResponse(c, limit.BlockedUntil)
		return true
	}
	return false
}

func (s *SubsServer) recordProtectedFailure(c *gin.Context, scope, clientIP, message string) {
	s.recordProtectedFailureWithStatus(c, scope, clientIP, http.StatusUnauthorized, message)
}

func (s *SubsServer) recordProtectedFailureWithStatus(c *gin.Context, scope, clientIP string, status int, message string) {
	enabled, err := s.Store.ProtectionEnabled(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	if !enabled {
		c.JSON(status, gin.H{"error": message})
		return
	}
	limit, err := s.Store.RecordSecurityFailure(c.Request.Context(), scope, clientIP, time.Now())
	if err != nil {
		internalError(c, err)
		return
	}
	if limit.Blocked {
		writeRateLimitResponse(c, limit.BlockedUntil)
		return
	}
	c.JSON(status, gin.H{"error": message})
}

func (s *SubsServer) clearProtectedFailures(c *gin.Context, scope, clientIP string) error {
	return s.Store.ClearSecurityFailures(c.Request.Context(), scope, clientIP)
}

func writeRateLimitResponse(c *gin.Context, blockedUntil time.Time) {
	retryAfter := int(time.Until(blockedUntil).Seconds())
	if retryAfter < 1 {
		retryAfter = 1
	}
	c.Header("Retry-After", strconv.Itoa(retryAfter))
	c.JSON(http.StatusTooManyRequests, gin.H{
		"error":         "错误次数过多，请稍后重试",
		"retry_after":   retryAfter,
		"blocked_until": blockedUntil.Unix(),
	})
}

func requestIsHTTPS(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	forwardedProto := strings.TrimSpace(strings.Split(c.GetHeader("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(forwardedProto, "https")
}

func (s *SubsServer) requireSession() gin.HandlerFunc {
	return func(c *gin.Context) {
		username, ok := s.currentUsername(c)
		if !ok {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "登录已失效，请重新登录"})
			return
		}
		c.Set("username", username)
		c.Next()
	}
}

func (s *SubsServer) currentUsername(c *gin.Context) (string, bool) {
	rawToken, err := c.Cookie(sessionCookieName)
	if err != nil || rawToken == "" {
		return "", false
	}
	username, err := s.Store.SessionUsername(c.Request.Context(), hashToken(rawToken), time.Now())
	return username, err == nil
}

func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.URL.Path == "/" || c.Request.URL.Path == "/dashboard" || c.Request.URL.Path == "/settings" || c.Request.URL.Path == "/favicon.ico" || strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			c.Header("Cache-Control", "no-cache")
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "same-origin")
		c.Header("Content-Security-Policy", "default-src 'self'; style-src 'self'; script-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'self'; form-action 'self'; frame-ancestors 'none'")
		c.Next()
	}
}

func noStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		c.Next()
	}
}

func sameOrigin() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead || c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		origin := c.GetHeader("Origin")
		if origin == "" {
			c.Next()
			return
		}
		parsed, err := url.Parse(origin)
		if err != nil || !strings.EqualFold(parsed.Host, c.Request.Host) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "请求来源无效"})
			return
		}
		c.Next()
	}
}

func badRequest(c *gin.Context, message string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": message})
}

func internalError(c *gin.Context, err error) {
	log.Printf("request failed: %v", err)
	c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器内部错误"})
}

func handleStoreError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "订阅不存在"})
	case errors.Is(err, ErrDuplicateURLPath):
		c.JSON(http.StatusConflict, gin.H{"error": "这个 URL 标识已经被使用"})
	default:
		internalError(c, err)
	}
}

func makeSubscriptionView(subscription Subscription) SubscriptionView {
	view := SubscriptionView{Subscription: subscription, TokenConfigured: len(subscription.TokenHash) > 0}
	view.TokenHash = nil
	expandedPath, err := expandPath(subscription.FilePath)
	if err != nil {
		view.FileStatus = "invalid"
		return view
	}
	info, err := os.Stat(expandedPath)
	if err != nil || info.IsDir() {
		view.FileStatus = "missing"
		return view
	}
	view.FileStatus = "ready"
	view.FileSize = info.Size()
	view.FileModifiedAt = info.ModTime().Unix()
	return view
}
