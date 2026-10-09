//go:build !windows

// Package durablefs publishes already-synced temporary files in the same directory.
package durablefs

import (
	"os"
	"path/filepath"
)

func Publish(source, target string, replace bool) error {
	if replace {
		if err := os.Rename(source, target); err != nil {
			return err
		}
	} else {
		if err := os.Link(source, target); err != nil {
			return err
		}
		if err := os.Remove(source); err != nil {
			return err
		}
	}
	dir, err := os.Open(filepath.Dir(target))
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
