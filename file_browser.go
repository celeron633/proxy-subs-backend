package main

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
)

var (
	errBrowseOutsideRoot  = errors.New("browse path is outside the configured root")
	errBrowseNotDirectory = errors.New("browse path is not a directory")
)

type fileBrowserEntry struct {
	Name         string `json:"name"`
	Path         string `json:"path"`
	RelativePath string `json:"relative_path"`
	IsDirectory  bool   `json:"is_directory"`
	Size         int64  `json:"size"`
	ModifiedAt   int64  `json:"modified_at"`
}

func resolveFileRoot(path string) (string, error) {
	expanded, err := expandPath(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(expanded)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errBrowseNotDirectory
	}
	return filepath.Clean(resolved), nil
}

func resolveBrowseDirectory(root, requestedPath string) (string, string, error) {
	requestedPath = filepath.FromSlash(requestedPath)
	if requestedPath == "" {
		requestedPath = "."
	}
	cleaned := filepath.Clean(requestedPath)
	if filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", "", errBrowseOutsideRoot
	}

	resolved, err := filepath.EvalSymlinks(filepath.Join(root, cleaned))
	if err != nil {
		return "", "", err
	}
	if !pathWithinRoot(root, resolved) {
		return "", "", errBrowseOutsideRoot
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", "", err
	}
	if !info.IsDir() {
		return "", "", errBrowseNotDirectory
	}

	relative, err := filepath.Rel(root, resolved)
	if err != nil {
		return "", "", err
	}
	if relative == "." {
		relative = ""
	}
	return filepath.Clean(resolved), filepath.ToSlash(relative), nil
}

func pathWithinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func (s *SubsServer) fileBrowserHandler(c *gin.Context) {
	currentPath, relativePath, err := resolveBrowseDirectory(s.fileRoot, c.Query("path"))
	if err != nil {
		switch {
		case errors.Is(err, errBrowseOutsideRoot):
			c.JSON(http.StatusBadRequest, gin.H{"error": "目录超出允许浏览的范围"})
		case errors.Is(err, errBrowseNotDirectory):
			c.JSON(http.StatusBadRequest, gin.H{"error": "所选路径不是目录"})
		case errors.Is(err, fs.ErrNotExist):
			c.JSON(http.StatusNotFound, gin.H{"error": "目录不存在"})
		case errors.Is(err, fs.ErrPermission):
			c.JSON(http.StatusForbidden, gin.H{"error": "没有权限读取该目录"})
		default:
			internalError(c, err)
		}
		return
	}

	directoryEntries, err := os.ReadDir(currentPath)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			c.JSON(http.StatusForbidden, gin.H{"error": "没有权限读取该目录"})
			return
		}
		internalError(c, err)
		return
	}

	entries := make([]fileBrowserEntry, 0, len(directoryEntries))
	for _, directoryEntry := range directoryEntries {
		entryPath := filepath.Join(currentPath, directoryEntry.Name())
		resolvedEntryPath, err := filepath.EvalSymlinks(entryPath)
		if err != nil || !pathWithinRoot(s.fileRoot, resolvedEntryPath) {
			continue
		}
		info, err := os.Stat(resolvedEntryPath)
		if err != nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			continue
		}
		entryRelativePath, err := filepath.Rel(s.fileRoot, resolvedEntryPath)
		if err != nil {
			continue
		}
		entries = append(entries, fileBrowserEntry{
			Name:         directoryEntry.Name(),
			Path:         filepath.Clean(resolvedEntryPath),
			RelativePath: filepath.ToSlash(entryRelativePath),
			IsDirectory:  info.IsDir(),
			Size:         info.Size(),
			ModifiedAt:   info.ModTime().Unix(),
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDirectory != entries[j].IsDirectory {
			return entries[i].IsDirectory
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	parentPath := ""
	if relativePath != "" {
		parentPath = filepath.ToSlash(filepath.Dir(filepath.FromSlash(relativePath)))
		if parentPath == "." {
			parentPath = ""
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"root":          s.fileRoot,
		"current_path":  currentPath,
		"relative_path": relativePath,
		"parent_path":   parentPath,
		"entries":       entries,
	})
}
