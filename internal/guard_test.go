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
	makefile, err := os.ReadFile(filepath.Join(internalRoot(t), "..", "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	contents := string(makefile)

	checkFull := regexp.MustCompile(`(?m)^check-full:[^\n]*\n(?:\t[^\n]*\n)*`).FindString(contents)
	if checkFull == "" {
		t.Fatal("Makefile is missing the check-full target")
	}
	if strings.Contains(checkFull, "GOOS=linux") {
		t.Error("check-full must remain host-neutral; deferred Linux builds cannot gate the v1 release")
	}
	if strings.Contains(checkFull, "legacy-package-") {
		t.Error("check-full must not invoke deferred Linux packaging targets")
	}

	for _, target := range []string{
		"legacy-package-binaries-linux-amd64",
		"legacy-package-binaries-linux-arm64",
		"legacy-package-linux-amd64",
		"legacy-package-linux-arm64",
	} {
		targetDeclaration := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:`)
		if !targetDeclaration.MatchString(contents) {
			t.Errorf("deferred Linux packaging target must be explicitly marked legacy: missing %q", target)
		}
	}
	if regexp.MustCompile(`(?m)^package-(?:binaries-)?linux-(?:amd64|arm64):`).MatchString(contents) {
		t.Error("unqualified Linux package targets make the deferred release path appear active")
	}
}
