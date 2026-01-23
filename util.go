package main

import (
	"os"
	"path/filepath"
	"strings"
)

func expandPath(p string) (string, error) {
	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}

		// 处理 "~" 或 "~/xxx"
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}

	return filepath.Abs(p)
}
