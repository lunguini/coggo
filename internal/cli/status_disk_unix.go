//go:build !windows

package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

func probeDisk(dbPath string) (string, string) {
	path := nearestExistingPath(dbPath)
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return "unknown", err.Error()
	}
	free := st.Bavail * uint64(st.Bsize)
	total := st.Blocks * uint64(st.Bsize)
	detail := fmt.Sprintf("%s free of %s at %s", formatBytes(free), formatBytes(total), path)
	switch {
	case free < minDiskCritical:
		return "critical", detail
	case free < minDiskWarning:
		return "low", detail
	default:
		return "ok", detail
	}
}

func nearestExistingPath(path string) string {
	if path == "" {
		return "."
	}
	for {
		if _, err := os.Stat(path); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}
