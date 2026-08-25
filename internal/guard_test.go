package internal_test

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func requireMakeTarget(t *testing.T, makefileContents, target string) string {
	t.Helper()
	pattern := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:[^\n]*\n(?:\t[^\n]*\n)*`)
	contents := pattern.FindString(makefileContents)
	if contents == "" {
		t.Fatalf("Makefile is missing the %s target", target)
	}
	return contents
}

func TestBuildInjectsCandidateIdentity(t *testing.T) {
	makefile, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	contents := string(makefile)
	for _, required := range []string{
		"CANDIDATE_SHA := $(shell git rev-parse HEAD)",
		"CANDIDATE_TAG := $(shell git describe --exact-match HEAD 2>/dev/null)",
		"0.0.0-dev+$(CANDIDATE_SHA)",
		"-X main.version=$(BUILD_VERSION)",
		"-X main.commitSHA=$(CANDIDATE_SHA)",
		"export BUILD_LDFLAGS",
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("build does not inject candidate identity: missing %q", required)
		}
	}
	if got := strings.Count(contents, `-ldflags "$$BUILD_LDFLAGS"`); got != 2 {
		t.Errorf("build applies candidate identity flags %d times, want both binaries", got)
	}
}

func TestToolchainGuardNamesConfigurationErrors(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	guard := filepath.Join(root, "scripts", "check-toolchain.sh")
	if output, err := exec.Command("sh", guard, filepath.Join(root, ".tool-versions"), filepath.Join(root, "go.mod")).CombinedOutput(); err != nil {
		t.Fatalf("repository toolchain guard failed: %v\n%s", err, output)
	}

	for _, test := range []struct {
		name         string
		toolVersions string
		goMod        string
		want         string
	}{
		{
			name:         "missing golang version",
			toolVersions: "nodejs 22.23.2\n",
			goMod:        "module example.invalid/test\n\ngo 1.23.0\n\ntoolchain go1.23.12\n",
			want:         "toolchain mismatch: .tool-versions is missing golang",
		},
		{
			name:         "missing Go toolchain",
			toolVersions: "golang 1.23.12\nnodejs 22.23.2\n",
			goMod:        "module example.invalid/test\n\ngo 1.23.0\n",
			want:         "toolchain mismatch: go.mod is missing toolchain",
		},
		{
			name:         "version mismatch",
			toolVersions: "golang 1.23.1\nnodejs 22.23.2\n",
			goMod:        "module example.invalid/test\n\ngo 1.23.0\n\ntoolchain go1.23.0\n",
			want:         "toolchain mismatch: golang .tool-versions=1.23.1, go.mod toolchain=1.23.0",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			temporaryRoot := t.TempDir()
			toolVersions := filepath.Join(temporaryRoot, ".tool-versions")
			goMod := filepath.Join(temporaryRoot, "go.mod")
			if err := os.WriteFile(toolVersions, []byte(test.toolVersions), 0600); err != nil {
				t.Fatalf("write .tool-versions: %v", err)
			}
			if err := os.WriteFile(goMod, []byte(test.goMod), 0600); err != nil {
				t.Fatalf("write go.mod: %v", err)
			}

			output, err := exec.Command("sh", guard, toolVersions, goMod).CombinedOutput()
			if err == nil {
				t.Fatalf("toolchain guard accepted invalid configuration:\n%s", output)
			}
			if !strings.Contains(string(output), test.want) {
				t.Fatalf("toolchain guard output = %q, want %q", output, test.want)
			}
		})
	}
}

func TestCheckRunsTheToolchainGuard(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	check := requireMakeTarget(t, string(makefile), "check")
	if !strings.Contains(check, "sh scripts/check-toolchain.sh") {
		t.Fatal("make check does not run the toolchain consistency guard")
	}
}

func TestMigrationsContainOnlyUpSections(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..", "migrations")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(root, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		if strings.Contains(strings.ToLower(string(contents)), "-- +goose down") {
			t.Errorf("migration %s contains a down section", entry.Name())
		}
	}
}

func TestClaudeMarkdownPathsExist(t *testing.T) {
	root := filepath.Clean(filepath.Join(internalRoot(t), ".."))
	pathPattern := regexp.MustCompile("`([^`]+(?:/[^`]+)+)`")
	for _, document := range []string{"CLAUDE.md", "web/CLAUDE.md"} {
		file, err := os.Open(filepath.Join(root, document))
		if err != nil {
			t.Fatalf("open %s: %v", document, err)
		}
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			for _, match := range pathPattern.FindAllStringSubmatch(scanner.Text(), -1) {
				path := strings.TrimSuffix(match[1], ".")
				if strings.ContainsAny(path, "*<>") || strings.Contains(path, " → ") || strings.Contains(path, " / ") {
					continue
				}
				if _, err := os.Stat(filepath.Join(root, path)); err != nil {
					t.Errorf("%s points to missing path %q", document, path)
				}
			}
		}
		file.Close()
		if err := scanner.Err(); err != nil {
			t.Fatalf("scan %s: %v", document, err)
		}
	}
}

func TestDeadDeliveryAssetsAreRemoved(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	for _, path := range []string{
		"scripts/package-linux.sh",
		"packaging/README.md",
		"packaging/bundle",
		"packaging/systemd/dbs-monitor-postgres.service",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(path))); !os.IsNotExist(err) {
			t.Errorf("dead delivery asset %s still exists", path)
		}
	}

	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	for _, obsolete := range []string{"legacy-package-", "scripts/package-linux.sh"} {
		if strings.Contains(string(makefile), obsolete) {
			t.Errorf("Makefile still exposes dead delivery entry point %q", obsolete)
		}
	}

	serverUnit, err := os.ReadFile(filepath.Join(root, "packaging", "systemd", "dbs-monitor-server.service"))
	if err != nil {
		t.Fatalf("read server systemd unit: %v", err)
	}
	if strings.Contains(string(serverUnit), "dbs-monitor-postgres.service") {
		t.Fatal("server systemd unit still depends on removed bundled PostgreSQL unit")
	}
}

func TestCheckFullWiresDatabaseCompatibilityGates(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	checkFull := requireMakeTarget(t, string(makefile), "_check-full")
	for _, required := range []string{"$(MAKE) check-pg-matrix", "$(MAKE) check-sqlc-vet"} {
		if !strings.Contains(checkFull, required) {
			t.Errorf("check-full is missing %q", required)
		}
	}
}

func TestSQLCVetCoversAllQuerySets(t *testing.T) {
	config, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "sqlc.yaml"))
	if err != nil {
		t.Fatalf("read sqlc config: %v", err)
	}

	const querySetCount = 5
	contents := string(config)
	for _, required := range []string{`uri: ${DATABASE_URL}`, `database: false`, `- sqlc/db-prepare`} {
		if got := strings.Count(contents, required); got != querySetCount {
			t.Errorf("sqlc config contains %q %d times, want %d", required, got, querySetCount)
		}
	}
}
