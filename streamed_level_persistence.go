package gekko

import (
	"os"
	"path/filepath"
	"strings"
)

// writeStreamedLevelPayload publishes a unique, durable payload before its
// path can enter a manifest. It never changes an earlier published payload.
func writeStreamedLevelPayload(dir, name string, serialize func(string) error) (string, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	ext := filepath.Ext(name)
	stem := sanitizePathSegment(strings.TrimSuffix(filepath.Base(name), ext))
	reservation, err := os.CreateTemp(dir, stem+"_*"+ext)
	if err != nil {
		return "", err
	}
	finalPath := reservation.Name()
	renamed := false
	defer func() {
		if !renamed {
			_ = os.Remove(finalPath)
		}
	}()
	if err := reservation.Close(); err != nil {
		return "", err
	}
	temp, err := os.CreateTemp(dir, ".payload-*"+ext)
	if err != nil {
		return "", err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Close(); err != nil {
		return "", err
	}
	if err := serialize(tempPath); err != nil {
		return "", err
	}
	completed, err := os.OpenFile(tempPath, os.O_RDWR, 0)
	if err != nil {
		return "", err
	}
	if err := completed.Chmod(0644); err != nil {
		_ = completed.Close()
		return "", err
	}
	if err := completed.Sync(); err != nil {
		_ = completed.Close()
		return "", err
	}
	if err := completed.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tempPath, finalPath); err != nil {
		return "", err
	}
	// After rename, failure may leave an unreferenced unique orphan. Its path
	// is never returned and no previously published payload is affected.
	renamed = true
	directory, err := os.Open(dir)
	if err != nil {
		return "", err
	}
	if err := directory.Sync(); err != nil {
		_ = directory.Close()
		return "", err
	}
	if err := directory.Close(); err != nil {
		return "", err
	}
	return finalPath, nil
}
