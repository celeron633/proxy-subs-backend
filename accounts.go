package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

var ErrDuplicateUsername = errors.New("duplicate username")
var ErrLastAdmin = errors.New("cannot delete the last administrator")

func (s *Store) AddAdmin(ctx context.Context, username, password string) (Admin, error) {
	username = strings.TrimSpace(username)
	if err := validateCredentials(username, password); err != nil {
		return Admin{}, err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return Admin{}, err
	}
	now := time.Now().Unix()
	result, err := s.db.ExecContext(ctx, "INSERT INTO admins(username, password_hash, created_at, updated_at) VALUES (?, ?, ?, ?)", username, hash, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique constraint failed: admins.username") {
			return Admin{}, ErrDuplicateUsername
		}
		return Admin{}, err
	}
	id, err := result.LastInsertId()
	return Admin{ID: id, Username: username, CreatedAt: time.Unix(now, 0), UpdatedAt: time.Unix(now, 0)}, err
}

func (s *Store) adminCredentials(ctx context.Context, username string) (int64, []byte, error) {
	var id int64
	var hash []byte
	err := s.db.QueryRowContext(ctx, "SELECT id, password_hash FROM admins WHERE username = ?", strings.TrimSpace(username)).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return id, hash, err
}

func (s *Store) DeleteAdmin(ctx context.Context, id int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, "DELETE FROM admins WHERE id = ? AND (SELECT COUNT(*) FROM admins) > 1", id)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM admins WHERE id = ?", id).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return ErrNotFound
		}
		return ErrLastAdmin
	}
	return tx.Commit()
}

func (s *SubsServer) listUsersHandler(c *gin.Context) {
	users, err := s.Store.ListAdmins(c.Request.Context())
	if err != nil {
		internalError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"users": users, "username": c.GetString("username")})
}

func (s *SubsServer) createUserHandler(c *gin.Context) {
	var request credentialsRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		badRequest(c, "请输入用户名和密码")
		return
	}
	if err := validateCredentials(request.Username, request.Password); err != nil {
		badRequest(c, err.Error())
		return
	}
	user, err := s.Store.AddAdmin(c.Request.Context(), request.Username, request.Password)
	if err != nil {
		handleAccountError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user})
}

func (s *SubsServer) resetUserPasswordHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(c, "用户 ID 无效")
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&request); err != nil {
		badRequest(c, "请输入新密码")
		return
	}
	if err := validatePassword(request.Password); err != nil {
		badRequest(c, err.Error())
		return
	}
	if err := s.Store.ResetAdminPassword(c.Request.Context(), id, request.Password); err != nil {
		handleAccountError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

func (s *SubsServer) deleteUserHandler(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		badRequest(c, "用户 ID 无效")
		return
	}
	currentID, _, err := s.Store.adminCredentials(c.Request.Context(), c.GetString("username"))
	if err != nil {
		internalError(c, err)
		return
	}
	if currentID == id {
		badRequest(c, "不能删除当前登录账号")
		return
	}
	if err := s.Store.DeleteAdmin(c.Request.Context(), id); err != nil {
		handleAccountError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func handleAccountError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrDuplicateUsername):
		c.JSON(http.StatusConflict, gin.H{"error": "用户名已存在"})
	case errors.Is(err, ErrLastAdmin):
		c.JSON(http.StatusConflict, gin.H{"error": "至少保留一个管理员"})
	case errors.Is(err, ErrNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "用户不存在"})
	default:
		internalError(c, err)
	}
}
