package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const recoveryLifetime = 10 * time.Minute
const securityScopeRecovery = "password_recovery"

var ErrInvalidRecovery = errors.New("invalid or expired recovery code")

type recoveryChallenge struct {
	adminID      int64
	passwordHash []byte
	codeHash     []byte
	expiresAt    time.Time
	fileInfo     os.FileInfo
}

// A single outstanding challenge owns recovery.txt. Never overwrite an active
// challenge, or follow an existing file/symlink supplied at this path.
type RecoveryManager struct {
	mu        sync.Mutex
	path      string
	challenge *recoveryChallenge
	timer     *time.Timer
}

func NewRecoveryManager(path string) *RecoveryManager {
	return &RecoveryManager{path: path}
}

func (m *RecoveryManager) Issue(adminID int64, passwordHash []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.challenge != nil && time.Now().Before(m.challenge.expiresAt) {
		return nil
	}
	m.clearLocked()
	code, err := randomToken(32)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(m.path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	info, statErr := f.Stat()
	_, writeErr := f.WriteString(code + "\n")
	syncErr := f.Sync()
	closeErr := f.Close()
	if err := errors.Join(statErr, writeErr, syncErr, closeErr); err != nil {
		if info != nil {
			m.removeOwnedFile(info)
		}
		return err
	}
	challenge := &recoveryChallenge{adminID: adminID, passwordHash: append([]byte(nil), passwordHash...), codeHash: hashToken(code), expiresAt: time.Now().Add(recoveryLifetime), fileInfo: info}
	m.challenge = challenge
	m.timer = time.AfterFunc(recoveryLifetime, func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.challenge == challenge {
			m.clearLocked()
		}
	})
	return nil
}

func (m *RecoveryManager) Reset(ctx context.Context, store *Store, username, code, password string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	challenge := m.challenge
	if challenge == nil {
		return ErrInvalidRecovery
	}
	if !time.Now().Before(challenge.expiresAt) {
		m.clearLocked()
		return ErrInvalidRecovery
	}
	if subtle.ConstantTimeCompare(hashToken(strings.TrimSpace(code)), challenge.codeHash) != 1 {
		return ErrInvalidRecovery
	}
	id, _, err := store.adminCredentials(ctx, username)
	if errors.Is(err, ErrNotFound) || (err == nil && id != challenge.adminID) {
		return ErrInvalidRecovery
	}
	if err != nil {
		return err
	}
	// The conditional update also invalidates a code if an administrator or the
	// command-line tool changed the password after the code was issued.
	err = store.resetAdminPassword(ctx, challenge.adminID, password, challenge.passwordHash)
	if errors.Is(err, ErrNotFound) {
		m.clearLocked()
		return ErrInvalidRecovery
	}
	if err != nil {
		return err
	}
	m.clearLocked()
	return nil
}

func (m *RecoveryManager) removeOwnedFile(info os.FileInfo) {
	current, err := os.Lstat(m.path)
	if err == nil && current.Mode().IsRegular() && os.SameFile(current, info) {
		if err := os.Remove(m.path); err != nil {
			log.Printf("remove expired recovery file: %v", err)
		}
	}
}

func (m *RecoveryManager) clearLocked() {
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}
	if m.challenge != nil {
		m.removeOwnedFile(m.challenge.fileInfo)
		m.challenge = nil
	}
}

func (m *RecoveryManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearLocked()
}

func (s *SubsServer) isRecoveryFile(path string) bool {
	if strings.EqualFold(filepath.Base(path), "recovery.txt") {
		return true
	}
	if s.Recovery == nil {
		return false
	}
	a, errA := filepath.Abs(path)
	b, errB := filepath.Abs(s.Recovery.path)
	if errA == nil && errB == nil && (a == b || (runtime.GOOS == "windows" && strings.EqualFold(a, b))) {
		return true
	}
	infoA, errA := os.Stat(path)
	infoB, errB := os.Stat(s.Recovery.path)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

func (s *SubsServer) rejectRecoveryClient(c *gin.Context) bool {
	// Recovery limits remain enabled even if ordinary login/API protection is off.
	limit, err := s.Store.SecurityLimit(c.Request.Context(), securityScopeRecovery, c.ClientIP(), time.Now())
	if err != nil {
		internalError(c, err)
		return true
	}
	if limit.Blocked {
		writeRateLimitResponse(c, limit.BlockedUntil)
		return true
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	return false
}

func (s *SubsServer) recoveryFailure(c *gin.Context, message string) {
	limit, err := s.Store.RecordSecurityFailure(c.Request.Context(), securityScopeRecovery, c.ClientIP(), time.Now())
	if err != nil {
		internalError(c, err)
		return
	}
	if limit.Blocked {
		writeRateLimitResponse(c, limit.BlockedUntil)
		return
	}
	c.JSON(http.StatusBadRequest, gin.H{"error": message})
}

func (s *SubsServer) requestRecoveryHandler(c *gin.Context) {
	if s.rejectRecoveryClient(c) {
		return
	}
	var request credentialsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		s.recoveryFailure(c, "请求格式不正确")
		return
	}
	if !s.Captcha.Verify(request.CaptchaID, request.CaptchaAnswer) {
		s.recoveryFailure(c, "验证码错误，请重新输入")
		return
	}
	if err := validateUsername(request.Username); err != nil {
		badRequest(c, err.Error())
		return
	}
	id, passwordHash, err := s.Store.adminCredentials(c.Request.Context(), request.Username)
	if err != nil && !errors.Is(err, ErrNotFound) {
		internalError(c, err)
		return
	}
	if err == nil {
		if err := s.Recovery.Issue(id, passwordHash); err != nil {
			log.Printf("create recovery file: %v", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "无法创建 recovery.txt。请检查程序目录写入权限；若有重启前遗留的同名文件，请先手动删除。"})
			return
		}
	}
	// Do not reveal whether an account exists or return the secret through HTTP.
	c.JSON(http.StatusAccepted, gin.H{"ok": true, "expires_in": int(recoveryLifetime.Seconds())})
}

func (s *SubsServer) resetRecoveryHandler(c *gin.Context) {
	if s.rejectRecoveryClient(c) {
		return
	}
	var request struct {
		Username string `json:"username"`
		Code     string `json:"recovery_code"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		s.recoveryFailure(c, "请求格式不正确")
		return
	}
	if err := validateCredentials(request.Username, request.Password); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := s.Recovery.Reset(c.Request.Context(), s.Store, request.Username, request.Code, request.Password); err != nil {
		if errors.Is(err, ErrInvalidRecovery) {
			s.recoveryFailure(c, "恢复码无效或已过期")
			return
		}
		internalError(c, err)
		return
	}
	for _, scope := range []string{securityScopeLogin, securityScopeRecovery} {
		if err := s.Store.ClearSecurityFailures(c.Request.Context(), scope, c.ClientIP()); err != nil {
			log.Printf("clear recovered account failures: %v", err)
		}
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
