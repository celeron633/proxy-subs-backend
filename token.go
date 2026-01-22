package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"os"
	"strings"
)

type TokenManager struct {
	tokenMap map[string]struct{}
}

func (t *TokenManager) LoadTokenFromRedis() error {
	return nil
}

// LoadTokenFromFile 从文件读取sha256后的结果到set里面
func (t *TokenManager) LoadTokenFromFile(filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		log.Default().Println("Error loading token file:", filePath)
		return err
	}
	reader := bufio.NewReader(file)
	for {
		bLine, _, err := reader.ReadLine()
		if err == io.EOF {
			break
		}
		line := string(bLine)
		line = strings.TrimSpace(line)
		t.tokenMap[line] = struct{}{}
	}
	return nil
}

// ValidateHashedToken 验证hash后的token是否在set中
func (t *TokenManager) ValidateHashedToken(token string) bool {
	_, ok := t.tokenMap[token]
	return ok
}

// ValidateToken 验证原始字符串hash后是否在set中
func (t *TokenManager) ValidateToken(token string) bool {
	hash := sha256.Sum256([]byte(token))
	hashString := hex.EncodeToString(hash[:])
	_, ok := t.tokenMap[hashString]
	return ok
}

func NewTokenManager() *TokenManager {
	return &TokenManager{make(map[string]struct{})}
}
