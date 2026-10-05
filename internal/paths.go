package internal

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Muxcore-Media/core/sdk/go/module/pathguard"
)

func pathUnderRoot(path, root string) (string, error) {
	path = strings.TrimSpace(path)
	root = strings.TrimSpace(root)
	if path == "" {
		return "", fmt.Errorf("path is required")
	}
	if root == "" {
		return "", fmt.Errorf("library root is not configured")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve path: %w", err)
	}
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("resolve library root: %w", err)
	}
	resolved, err := pathguard.Confine(abs, []string{rootAbs})
	if err != nil {
		return "", fmt.Errorf("path %q is outside library root: %w", abs, err)
	}
	return resolved, nil
}

func issueHasFile(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path) //nolint:gosec // stat-only existence check on a path persisted by the library scan
	return err == nil && !info.IsDir()
}
