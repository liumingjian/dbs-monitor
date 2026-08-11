package main

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

const (
	diagnosticBundleCommand  = "diagnostic-bundle"
	diagnosticBundleMaxBytes = int64(64 << 20)
	diagnosticJournalUnit    = "dbs-monitor-server.service"
)

type diagnosticManifest struct {
	GeneratedAt      time.Time `json:"generated_at"`
	JournalWindow    string    `json:"journal_window"`
	JournalTruncated bool      `json:"journal_truncated"`
	MaximumBytes     int64     `json:"maximum_bytes"`
}

type deploymentSummary struct {
	Version   string     `json:"version"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Shape     string     `json:"shape"`
}

type diagnosticFile struct {
	name    string
	content []byte
}

func runDiagnosticBundleCommand(ctx context.Context, output string) error {
	journal, inputTruncated, err := readPlatformJournal(ctx, diagnosticBundleMaxBytes)
	if err != nil {
		return err
	}
	return createDiagnosticBundleWithInputTruncation(
		output, journal, time.Now().UTC(), "linux-systemd", diagnosticBundleMaxBytes, inputTruncated,
	)
}

func readPlatformJournal(ctx context.Context, maximumBytes int64) ([]byte, bool, error) {
	command := exec.CommandContext(ctx, "journalctl", "--unit", diagnosticJournalUnit, "--since=-24 hours", "--reverse", "--no-pager", "--output=cat")
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, false, fmt.Errorf("open journal output: %w", err)
	}
	var stderr bytes.Buffer
	command.Stderr = &stderr
	if err := command.Start(); err != nil {
		return nil, false, fmt.Errorf("start journalctl: %w", err)
	}

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	newestFirst := make([][]byte, 0)
	var total int64
	truncated := false
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		line = append(line, '\n')
		if total+int64(len(line)) > maximumBytes {
			truncated = true
			break
		}
		newestFirst = append(newestFirst, line)
		total += int64(len(line))
	}
	if truncated {
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if scanErr := scanner.Err(); scanErr != nil {
		return nil, false, fmt.Errorf("read journalctl output: %w", scanErr)
	}
	if waitErr != nil && !truncated {
		return nil, false, fmt.Errorf("journalctl failed: %s", strings.TrimSpace(stderr.String()))
	}

	var journal bytes.Buffer
	for index := len(newestFirst) - 1; index >= 0; index-- {
		_, _ = journal.Write(newestFirst[index])
	}
	return journal.Bytes(), truncated, nil
}

func createDiagnosticBundle(output string, journal []byte, generatedAt time.Time, shape string, maximumBytes int64) error {
	return createDiagnosticBundleWithInputTruncation(output, journal, generatedAt, shape, maximumBytes, false)
}

func createDiagnosticBundleWithInputTruncation(output string, journal []byte, generatedAt time.Time, shape string, maximumBytes int64, inputTruncated bool) error {
	if maximumBytes <= 0 {
		return errors.New("diagnostic bundle maximum size must be positive")
	}
	if _, err := os.Lstat(output); err == nil {
		return fmt.Errorf("diagnostic bundle output already exists: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect diagnostic bundle output: %w", err)
	}

	if err := scanDiagnosticContent("journal.log", journal); err != nil {
		return err
	}
	snapshot, err := latestHealthSummary(journal)
	if err != nil {
		return err
	}
	baseFiles, err := diagnosticSnapshotFiles(snapshot, generatedAt, shape)
	if err != nil {
		return err
	}
	for _, file := range baseFiles {
		if err := scanDiagnosticContent(file.name, file.content); err != nil {
			return err
		}
	}

	lines := bytes.SplitAfter(journal, []byte{'\n'})
	if len(lines) > 0 && len(lines[len(lines)-1]) == 0 {
		lines = lines[:len(lines)-1]
	}
	archive, err := renderDiagnosticArchive(baseFiles, journal, generatedAt, maximumBytes, inputTruncated)
	if err != nil {
		return err
	}
	if int64(len(archive)) > maximumBytes {
		archive, err = largestFittingDiagnosticArchive(baseFiles, lines, generatedAt, maximumBytes)
		if err != nil {
			return err
		}
	}
	return publishDiagnosticArchive(output, archive)
}

func latestHealthSummary(journal []byte) (platformhealth.Snapshot, error) {
	var latest platformhealth.Snapshot
	found := false
	for _, line := range bytes.Split(journal, []byte{'\n'}) {
		start := bytes.IndexByte(line, '{')
		if start < 0 {
			continue
		}
		candidate := line[start:]
		var envelope struct {
			Event string `json:"event"`
		}
		if json.Unmarshal(candidate, &envelope) != nil || envelope.Event != "platform_health_summary" {
			continue
		}
		var snapshot platformhealth.Snapshot
		if err := json.Unmarshal(candidate, &snapshot); err != nil {
			continue
		}
		latest = snapshot
		found = true
	}
	if !found {
		return platformhealth.Snapshot{}, errors.New("diagnostic bundle requires a platform_health_summary from the last 24 hours")
	}
	return latest, nil
}

func diagnosticSnapshotFiles(snapshot platformhealth.Snapshot, generatedAt time.Time, shape string) ([]diagnosticFile, error) {
	files := make([]diagnosticFile, 0, 8)
	health, err := marshalDiagnosticJSON(snapshot)
	if err != nil {
		return nil, err
	}
	files = append(files, diagnosticFile{name: "diagnostics/health.json", content: health})

	endpoints := []struct {
		name   string
		source platformhealth.Source
	}{
		{name: "disk", source: platformhealth.SourceDisk},
		{name: "scheduler", source: platformhealth.SourceCollectionScheduler},
		{name: "partitions", source: platformhealth.SourcePartitionMaintenance},
		{name: "certificate", source: platformhealth.SourceTLSCertificate},
		{name: "keyring", source: platformhealth.SourceCredentialKeyring},
		{name: "platform", source: platformhealth.SourceServerProcess},
	}
	bySource := make(map[platformhealth.Source]platformhealth.SourceSnapshot, len(snapshot.Sources))
	for _, source := range snapshot.Sources {
		bySource[source.Source] = source
	}
	for _, endpoint := range endpoints {
		source, exists := bySource[endpoint.source]
		if !exists {
			source = platformhealth.SourceSnapshot{
				Source: endpoint.source, Status: platformhealth.StatusUnknown, Code: "FACT_UNAVAILABLE",
			}
		}
		content, err := marshalDiagnosticJSON(source)
		if err != nil {
			return nil, err
		}
		files = append(files, diagnosticFile{name: "diagnostics/" + endpoint.name + ".json", content: content})
	}

	process := bySource[platformhealth.SourceServerProcess]
	deployment, err := marshalDiagnosticJSON(deploymentSummary{
		Version: pointerValue(process.Version), StartedAt: process.StartedAt, Shape: shape,
	})
	if err != nil {
		return nil, err
	}
	files = append(files, diagnosticFile{name: "deployment.json", content: deployment})
	return files, nil
}

func marshalDiagnosticJSON(value any) ([]byte, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode diagnostic JSON: %w", err)
	}
	return append(encoded, '\n'), nil
}

func renderDiagnosticArchive(baseFiles []diagnosticFile, journal []byte, generatedAt time.Time, maximumBytes int64, truncated bool) ([]byte, error) {
	manifest, err := marshalDiagnosticJSON(diagnosticManifest{
		GeneratedAt: generatedAt.UTC(), JournalWindow: "24h", JournalTruncated: truncated, MaximumBytes: maximumBytes,
	})
	if err != nil {
		return nil, err
	}
	files := make([]diagnosticFile, 0, len(baseFiles)+2)
	files = append(files, diagnosticFile{name: "manifest.json", content: manifest})
	files = append(files, diagnosticFile{name: "journal.log", content: journal})
	files = append(files, baseFiles...)

	var output bytes.Buffer
	gzipWriter := gzip.NewWriter(&output)
	gzipWriter.Header.ModTime = generatedAt.UTC()
	tarWriter := tar.NewWriter(gzipWriter)
	for _, file := range files {
		header := &tar.Header{Name: file.name, Mode: 0600, Size: int64(len(file.content)), ModTime: generatedAt.UTC()}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write diagnostic archive header: %w", err)
		}
		if _, err := tarWriter.Write(file.content); err != nil {
			return nil, fmt.Errorf("write diagnostic archive content: %w", err)
		}
	}
	if err := tarWriter.Close(); err != nil {
		return nil, fmt.Errorf("close diagnostic tar: %w", err)
	}
	if err := gzipWriter.Close(); err != nil {
		return nil, fmt.Errorf("close diagnostic gzip: %w", err)
	}
	return output.Bytes(), nil
}

func largestFittingDiagnosticArchive(baseFiles []diagnosticFile, lines [][]byte, generatedAt time.Time, maximumBytes int64) ([]byte, error) {
	empty, err := renderDiagnosticArchive(baseFiles, nil, generatedAt, maximumBytes, true)
	if err != nil {
		return nil, err
	}
	if int64(len(empty)) > maximumBytes {
		return nil, fmt.Errorf("diagnostic metadata exceeds maximum bundle size of %d bytes", maximumBytes)
	}
	best := empty
	low, high := 0, len(lines)
	for low <= high {
		kept := low + (high-low)/2
		journal := bytes.Join(lines[len(lines)-kept:], nil)
		candidate, err := renderDiagnosticArchive(baseFiles, journal, generatedAt, maximumBytes, true)
		if err != nil {
			return nil, err
		}
		if int64(len(candidate)) <= maximumBytes {
			best = candidate
			low = kept + 1
		} else {
			high = kept - 1
		}
	}
	return best, nil
}

func scanDiagnosticContent(name string, content []byte) error {
	lower := bytes.ToLower(content)
	for _, forbidden := range [][]byte{
		[]byte("password"), []byte("ciphertext"), []byte("master_key"), []byte("master key"),
		[]byte("token"), []byte("authorization"), []byte("dsn"), []byte("request_body"), []byte("raw_sql"),
		[]byte("postgres://"), []byte("postgresql://"), []byte("bearer "), []byte("-----begin private key"),
		[]byte("select "), []byte("insert into "), []byte("update "), []byte("delete from "),
		[]byte("create table "), []byte("alter table "), []byte("drop table "),
	} {
		if bytes.Contains(lower, forbidden) {
			return fmt.Errorf("diagnostic bundle secret scan rejected %s: forbidden marker %q", name, forbidden)
		}
	}
	return nil
}

func publishDiagnosticArchive(output string, archive []byte) (returnErr error) {
	directory := filepath.Dir(output)
	temporary, err := os.CreateTemp(directory, ".dbs-monitor-diagnostics-*.tmp")
	if err != nil {
		return fmt.Errorf("create diagnostic bundle temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if returnErr != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err := temporary.Chmod(0600); err != nil {
		return fmt.Errorf("secure diagnostic bundle temporary file: %w", err)
	}
	if _, err := io.Copy(temporary, bytes.NewReader(archive)); err != nil {
		return fmt.Errorf("write diagnostic bundle: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close diagnostic bundle: %w", err)
	}
	if err := os.Rename(temporaryName, output); err != nil {
		return fmt.Errorf("publish diagnostic bundle: %w", err)
	}
	return nil
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
