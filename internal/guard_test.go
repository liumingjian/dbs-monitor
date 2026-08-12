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

func containsAnySubstring(text string, substrings ...string) bool {
	for _, substring := range substrings {
		if strings.Contains(text, substring) {
			return true
		}
	}
	return false
}

func readLinuxReleaseDisposition(t *testing.T) string {
	t.Helper()
	disposition, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "docs", "design", "21-v1-linux-release-disposition.md"))
	if err != nil {
		t.Fatalf("read Linux release disposition: %v", err)
	}
	return string(disposition)
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

func TestInstalledDatabaseAndCredentialKeyringStaySeparate(t *testing.T) {
	installer, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "packaging", "bundle", "install.sh"))
	if err != nil {
		t.Fatalf("read installer: %v", err)
	}
	contents := string(installer)
	for _, required := range []string{
		`data_dir=$(realpath -m "$data_dir")`,
		`data_prefix=${data_dir%/}`,
		`case "$install_root/etc" in`,
		`case "$data_dir" in`,
		`PGDATA=$data_dir`,
		`CREDENTIALS_DIR=$install_root/etc/credentials`,
	} {
		if !strings.Contains(contents, required) {
			t.Errorf("installer no longer enforces separate database and credential-keyring artifacts: missing %q", required)
		}
	}
}

func TestLegacyUpgradeBacksUpControlPlaneBeforeReplacingFiles(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	upgradePath := filepath.Join(root, "packaging", "bundle", "upgrade.sh")
	contents, err := os.ReadFile(upgradePath)
	if err != nil {
		t.Fatalf("read upgrade script: %v", err)
	}
	if output, err := exec.Command("sh", "-n", upgradePath).CombinedOutput(); err != nil {
		t.Fatalf("upgrade script syntax: %v\n%s", err, output)
	}
	script := string(contents)
	for _, required := range []string{
		"systemctl stop dbs-monitor-server.service",
		"--format=custom",
		"--exclude-table-data=public.metric_sample*",
		`/pg_restore" --list`,
		"systemctl stop dbs-monitor-postgres.service",
		"systemctl start dbs-monitor-postgres.service",
		"systemctl start dbs-monitor-server.service",
		"PostgreSQL major-version upgrades require a separate migration",
	} {
		if !strings.Contains(script, required) {
			t.Errorf("upgrade script is missing %q", required)
		}
	}
	stopServerIndex := strings.Index(script, "systemctl stop dbs-monitor-server.service")
	backupIndex := strings.Index(script, "--format=custom")
	stopPostgresIndex := strings.Index(script, "systemctl stop dbs-monitor-postgres.service")
	startPostgresIndex := strings.Index(script, "systemctl start dbs-monitor-postgres.service")
	startServerIndex := strings.Index(script, "systemctl start dbs-monitor-server.service")
	if stopServerIndex >= backupIndex || backupIndex >= stopPostgresIndex ||
		stopPostgresIndex >= startPostgresIndex || startPostgresIndex >= startServerIndex {
		t.Error("upgrade order must be stop server, back up, replace/restart PostgreSQL, then start server")
	}

	packagerContents, err := os.ReadFile(filepath.Join(root, "scripts", "package-linux.sh"))
	if err != nil {
		t.Fatalf("read Linux packager: %v", err)
	}
	if !strings.Contains(string(packagerContents), "packaging/bundle/upgrade.sh") {
		t.Error("legacy Linux archive does not include upgrade.sh")
	}
}

func TestV1ReleaseGateExcludesLegacyLinuxPackaging(t *testing.T) {
	projectRoot := filepath.Join(internalRoot(t), "..")
	makefile, err := os.ReadFile(filepath.Join(projectRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefileContents := string(makefile)

	checkFull := requireMakeTarget(t, makefileContents, "check-full")
	if strings.Contains(checkFull, "GOOS=linux") {
		t.Error("check-full must remain host-neutral; deferred Linux builds cannot gate the v1 release")
	}
	if strings.Contains(checkFull, "legacy-package-") {
		t.Error("check-full must not invoke deferred Linux packaging targets")
	}
	if containsAnySubstring(checkFull, "scripts/rt-c", "RT_C_") {
		t.Error("check-full must not turn the historical Linux RT-C reproduction into a v1 release gate")
	}

	for _, target := range []string{
		"legacy-package-binaries-linux-amd64",
		"legacy-package-binaries-linux-arm64",
		"legacy-package-linux-amd64",
		"legacy-package-linux-arm64",
	} {
		targetDeclaration := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:`)
		if !targetDeclaration.MatchString(makefileContents) {
			t.Errorf("deferred Linux packaging target must be explicitly marked legacy: missing %q", target)
		}
	}
	if regexp.MustCompile(`(?m)^package-(?:binaries-)?linux-(?:amd64|arm64):`).MatchString(makefileContents) {
		t.Error("unqualified Linux package targets make the deferred release path appear active")
	}

	workflowsDir := filepath.Join(projectRoot, ".github", "workflows")
	workflows, err := os.ReadDir(workflowsDir)
	if err != nil {
		t.Fatalf("read workflows: %v", err)
	}
	legacyEntryPoints := []string{
		"legacy-package-",
		"scripts/package-linux.sh",
		"packaging/bundle/install.sh",
		"packaging/bundle/upgrade.sh",
		"packaging/systemd/",
		"scripts/rt-c",
		"RT_C_",
	}
	for _, workflow := range workflows {
		workflowContents, err := os.ReadFile(filepath.Join(workflowsDir, workflow.Name()))
		if err != nil {
			t.Fatalf("read workflow %s: %v", workflow.Name(), err)
		}
		workflowText := string(workflowContents)
		for _, legacyEntryPoint := range legacyEntryPoints {
			if strings.Contains(workflowText, legacyEntryPoint) {
				t.Errorf("workflow %s must not invoke deferred Linux release entry point %q", workflow.Name(), legacyEntryPoint)
			}
		}
	}
}

func TestIssue96IsRetiredWithTheLinuxReleaseScope(t *testing.T) {
	dispositionText := readLinuxReleaseDisposition(t)
	for _, required := range []string{
		"#96",
		"Linux arm64",
		"darwin/arm64",
		"453.6M",
		"不得以缩减参数",
		"新 PRD",
	} {
		if !strings.Contains(dispositionText, required) {
			t.Errorf("Linux release disposition does not retire issue #96 completely: missing %q", required)
		}
	}
}

func TestIssue97IsRetiredWithoutDroppingV1ReleaseRequirements(t *testing.T) {
	dispositionText := readLinuxReleaseDisposition(t)
	for _, required := range []string{
		"#97",
		"Unavailability 13 码",
		"API",
		"01-appendix-implemented.md",
		"make gen",
		"PG13–17",
		"升级窗口",
		"外部探测",
		"干净 Mac",
		"Release assets",
		"不得勾选",
	} {
		if !strings.Contains(dispositionText, required) {
			t.Errorf("Linux release disposition does not retire issue #97 completely: missing %q", required)
		}
	}
}

func TestCheckFullWiresDatabaseCompatibilityGates(t *testing.T) {
	root := filepath.Join(internalRoot(t), "..")
	makefile, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	checkFull := requireMakeTarget(t, string(makefile), "check-full")
	for _, required := range []string{"$(MAKE) check-pg-matrix", "$(MAKE) check-sqlc-vet"} {
		if !strings.Contains(checkFull, required) {
			t.Errorf("check-full is missing %q", required)
		}
	}

	workflow, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "check-full.yml"))
	if err != nil {
		t.Fatalf("read check-full workflow: %v", err)
	}
	contents := string(workflow)
	for _, required := range []string{"name: check-full", "      - main", "  workflow_dispatch:", "      - run: make check-full"} {
		if !strings.Contains(contents, required) {
			t.Errorf("check-full workflow is missing %q", required)
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
