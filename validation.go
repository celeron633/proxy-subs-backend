package main

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

var (
	usernamePattern = regexp.MustCompile(`^[\p{L}\p{N}_.-]+$`)
	urlPathPattern  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
)

func validateCredentials(username, password string) error {
	username = strings.TrimSpace(username)
	usernameLength := utf8.RuneCountInString(username)
	if usernameLength < 3 || usernameLength > 32 || !usernamePattern.MatchString(username) {
		return errors.New("用户名需为 3–32 个字符，只能包含文字、数字、点、横线和下划线")
	}
	if utf8.RuneCountInString(password) < 8 {
		return errors.New("密码至少需要 8 个字符")
	}
	if len(password) > 72 {
		return errors.New("密码不能超过 72 个字节")
	}
	return nil
}

func subscriptionFromRequest(request subscriptionRequest, existing *Subscription) (Subscription, string, error) {
	name := strings.TrimSpace(request.Name)
	urlPath := strings.Trim(strings.TrimSpace(request.URLPath), "/")
	filePath := strings.TrimSpace(request.FilePath)
	note := strings.TrimSpace(request.Note)
	if name == "" || utf8.RuneCountInString(name) > 80 {
		return Subscription{}, "", errors.New("订阅名称需为 1–80 个字符")
	}
	if !urlPathPattern.MatchString(urlPath) {
		return Subscription{}, "", errors.New("URL 标识需为 1–64 位字母、数字、点、横线或下划线，且必须以字母或数字开头")
	}
	if filePath == "" || utf8.RuneCountInString(filePath) > 1024 {
		return Subscription{}, "", errors.New("请填写有效的本地文件路径")
	}
	if utf8.RuneCountInString(note) > 500 {
		return Subscription{}, "", errors.New("备注不能超过 500 个字符")
	}

	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	subscription := Subscription{Name: name, URLPath: urlPath, FilePath: filePath, Note: note, Enabled: enabled}

	plainToken := strings.TrimSpace(request.Token)
	if existing != nil {
		subscription.TokenHash = existing.TokenHash
		subscription.TokenHint = existing.TokenHint
	}
	if plainToken == "" && existing == nil {
		generated, err := randomToken(24)
		if err != nil {
			return Subscription{}, "", err
		}
		plainToken = generated
	}
	if plainToken != "" {
		if len(plainToken) < 8 || len(plainToken) > 128 {
			return Subscription{}, "", errors.New("token 需为 8–128 个字符，留空可由系统自动生成")
		}
		subscription.TokenHash = hashToken(plainToken)
		subscription.TokenHint = tokenHint(plainToken)
	}
	if len(subscription.TokenHash) == 0 {
		return Subscription{}, "", fmt.Errorf("token 不能为空")
	}
	return subscription, plainToken, nil
}

func randomToken(byteLength int) (string, error) {
	buffer := make([]byte, byteLength)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buffer), nil
}

func hashToken(token string) []byte {
	hash := sha256.Sum256([]byte(token))
	return hash[:]
}

func tokenHint(token string) string {
	runes := []rune(token)
	if len(runes) <= 4 {
		return strings.Repeat("•", len(runes))
	}
	return "••••" + string(runes[len(runes)-4:])
}
