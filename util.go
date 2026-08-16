package main

import (
	"os"
	"path/filepath"
	"strings"
)

func expandPath(path string) (string, error) {
	if path == "~" || strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		userHome, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}

		remainder := strings.TrimLeft(strings.TrimPrefix(path, "~"), `/\`)
		path = filepath.Join(userHome, remainder)
	}

	return filepath.Abs(path)
}
