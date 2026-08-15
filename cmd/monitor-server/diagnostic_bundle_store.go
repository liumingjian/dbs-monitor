package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	diagnosticBundlePrefix       = "dbs-monitor-diagnostics-"
	diagnosticBundleSuffix       = ".tar.gz"
	diagnosticBundleDeletedEvent = "diagnostic_bundle_deleted"
)

type diagnosticBundleInfo struct {
	Name       string
	Bytes      int64
	ModifiedAt time.Time
}

type diagnosticBundleDeletionEvent struct {
	Event      string    `json:"event"`
	BundleName string    `json:"bundle_name"`
	Bytes      int64     `json:"bytes"`
	DeletedAt  time.Time `json:"deleted_at"`
}

type diagnosticBundleDeletionRecorder func(diagnosticBundleDeletionEvent) error

func listDiagnosticBundles(directory string) ([]diagnosticBundleInfo, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, fmt.Errorf("list diagnostic bundle directory: %w", err)
	}

	bundles := make([]diagnosticBundleInfo, 0, len(entries))
	for _, entry := range entries {
		if !managedDiagnosticBundleName(entry.Name()) || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, fmt.Errorf("inspect diagnostic bundle %s: %w", entry.Name(), err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		bundles = append(bundles, diagnosticBundleInfo{
			Name:       entry.Name(),
			Bytes:      info.Size(),
			ModifiedAt: info.ModTime().UTC(),
		})
	}
	sort.Slice(bundles, func(left, right int) bool {
		if bundles[left].ModifiedAt.Equal(bundles[right].ModifiedAt) {
			return bundles[left].Name < bundles[right].Name
		}
		return bundles[left].ModifiedAt.Before(bundles[right].ModifiedAt)
	})
	return bundles, nil
}

func deleteDiagnosticBundle(directory, name string, deletedAt time.Time, record diagnosticBundleDeletionRecorder) error {
	if !managedDiagnosticBundleName(name) || filepath.Base(name) != name {
		return fmt.Errorf("invalid managed diagnostic bundle name %q", name)
	}
	if record == nil {
		return errors.New("diagnostic bundle deletion requires an event recorder")
	}

	path := filepath.Join(directory, name)
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("inspect diagnostic bundle %s: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("diagnostic bundle %s is not a regular file", name)
	}

	stagedPath := filepath.Join(directory, "."+name+".deleting")
	if _, err := os.Lstat(stagedPath); err == nil {
		return fmt.Errorf("diagnostic bundle deletion already staged for %s", name)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect diagnostic bundle deletion staging path: %w", err)
	}
	if err := os.Rename(path, stagedPath); err != nil {
		return fmt.Errorf("stage diagnostic bundle deletion: %w", err)
	}

	event := diagnosticBundleDeletionEvent{
		Event:      diagnosticBundleDeletedEvent,
		BundleName: name,
		Bytes:      info.Size(),
		DeletedAt:  deletedAt.UTC(),
	}
	if err := record(event); err != nil {
		if restoreErr := os.Rename(stagedPath, path); restoreErr != nil {
			return fmt.Errorf("record diagnostic bundle deletion event: %v; restore bundle: %w", err, restoreErr)
		}
		return fmt.Errorf("record diagnostic bundle deletion event: %w", err)
	}
	if err := os.Remove(stagedPath); err != nil {
		return fmt.Errorf("remove staged diagnostic bundle %s: %w", name, err)
	}
	return nil
}

func managedDiagnosticBundleName(name string) bool {
	return strings.HasPrefix(name, diagnosticBundlePrefix) && strings.HasSuffix(name, diagnosticBundleSuffix)
}
