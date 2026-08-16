package internal_test

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"
)

func TestB11AcceptanceAndE2EScriptsDoNotWriteBusinessTables(t *testing.T) {
	root := filepath.Dir(internalRoot(t))
	violations, err := findBusinessTableWrites(root)
	if err != nil {
		t.Fatalf("scan acceptance and E2E data setup: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("B11 forbids direct business-table writes: %s", violation)
	}
}

func TestB12CoveredAcceptanceReferencesResolve(t *testing.T) {
	root := filepath.Dir(internalRoot(t))
	violations, err := findBrokenAcceptanceReferences(root)
	if err != nil {
		t.Fatalf("scan covered acceptance references: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("B12 acceptance coverage drift: %s", violation)
	}
}

func TestB14PlatformDatabaseImagesUsePostgres17(t *testing.T) {
	root := filepath.Dir(internalRoot(t))
	violations, err := findPlatformDatabaseImageViolations(root)
	if err != nil {
		t.Fatalf("scan platform database images: %v", err)
	}
	for _, violation := range violations {
		t.Errorf("B14 platform database image drift: %s", violation)
	}
}

func TestB14PlatformDatabaseRuntimeCompatibility(t *testing.T) {
	majorVersion, localeProvider, err := platformDatabaseCompatibility(context.Background())
	if err != nil {
		t.Fatalf("query platform database compatibility: %v", err)
	}
	if majorVersion != 17 {
		t.Errorf("platform database major version = %d, want 17", majorVersion)
	}
	if localeProvider == "i" {
		t.Error("platform database uses the unsupported ICU locale provider")
	}
}

func TestProductionGuardsRejectDrift(t *testing.T) {
	t.Run("B11 business table write", func(t *testing.T) {
		root := t.TempDir()
		writeGuardFixture(t, root, "migrations/00001.sql", "CREATE TABLE metric_sample (id bigint);\n")
		writeGuardFixture(t, root, "scripts/check-e2e.sh", "psql -c 'INSERT INTO metric_sample VALUES (1)'\n")
		writeGuardFixture(t, root, "test/placeholder.go", "package placeholder\n")
		writeGuardFixture(t, root, "web/e2e/placeholder.ts", "export {};\n")
		violations, err := findBusinessTableWrites(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 1 || !strings.Contains(violations[0], "metric_sample") {
			t.Fatalf("violations = %v, want the metric_sample write", violations)
		}
	})

	t.Run("B12 matching entry ID", func(t *testing.T) {
		root := t.TempDir()
		writeGuardFixture(t, root, "test/acceptance/matrix.yaml", `entries:
  - id: AC-01-S1
    status: covered
    test_ref: "TestAcceptance_AC_01_S1"
`)
		writeGuardFixture(t, root, "test/acceptance/example_test.go", "package acceptance\n\nfunc TestAcceptance_AC_01_S1() {}\n")
		violations, err := findBrokenAcceptanceReferences(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 0 {
			t.Fatalf("violations = %v, want none", violations)
		}
	})

	t.Run("B12 renamed test", func(t *testing.T) {
		root := t.TempDir()
		writeGuardFixture(t, root, "test/acceptance/matrix.yaml", `entries:
  - id: AC-01-S1
    status: covered
    test_ref: "TestAcceptance_AC_01_S1"
`)
		writeGuardFixture(t, root, "test/acceptance/example_test.go", "package acceptance\n\nfunc TestAcceptance_AC_01_F1() {}\n")
		violations, err := findBrokenAcceptanceReferences(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 1 || !strings.Contains(violations[0], "absent from test source") {
			t.Fatalf("violations = %v, want the renamed test", violations)
		}
	})

	t.Run("B12 annotated Go test reference", func(t *testing.T) {
		root := t.TempDir()
		writeGuardFixture(t, root, "test/acceptance/matrix.yaml", `entries:
  - id: SEC-1
    status: covered
    test_ref: "test/acceptance/security_test.go::TestAcceptance_SEC_1 [SEC-1]"
`)
		writeGuardFixture(t, root, "test/acceptance/security_test.go", "package acceptance\nfunc TestAcceptance_SEC_1() {}\n")
		violations, err := findBrokenAcceptanceReferences(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 0 {
			t.Fatalf("violations = %v, want annotated Go test reference to resolve", violations)
		}
	})

	t.Run("B14 non-fixture platform image", func(t *testing.T) {
		root := t.TempDir()
		writeGuardFixture(t, root, "compose.yaml", `services:
  postgres:
    image: postgres:16
  platform-pg16:
    image: postgres:16
`)
		writeGuardFixture(t, root, ".github/workflows/check.yml", "jobs: {}\n")
		violations, err := findPlatformDatabaseImageViolations(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 1 || !strings.Contains(violations[0], "service postgres uses postgres:16") {
			t.Fatalf("violations = %v, want only the non-fixture platform service", violations)
		}
	})

	t.Run("B14 yaml workflow platform image", func(t *testing.T) {
		root := t.TempDir()
		writeGuardFixture(t, root, "compose.yaml", "services:\n  postgres:\n    image: postgres:17\n")
		writeGuardFixture(t, root, ".github/workflows/check.yaml", `jobs:
  check:
    services:
      postgres:
        image: postgres:16
`)
		violations, err := findPlatformDatabaseImageViolations(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(violations) != 1 || !strings.Contains(violations[0], "workflow check.yaml") {
			t.Fatalf("violations = %v, want the workflow platform service", violations)
		}
	})
}

func writeGuardFixture(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture %s: %v", relativePath, err)
	}
}

var (
	businessWritePattern  = regexp.MustCompile(`(?i)\b(?:INSERT\s+INTO|UPDATE|DELETE\s+FROM)\s+(?:dbsmon\.)?"?([a-z_][a-z0-9_]*)"?`)
	migrationTablePattern = regexp.MustCompile(`(?im)^CREATE TABLE(?: IF NOT EXISTS)?\s+([a-z_][a-z0-9_]*)`)
)

func findBusinessTableWrites(root string) ([]string, error) {
	businessTables, err := migrationTableNames(filepath.Join(root, "migrations"))
	if err != nil {
		return nil, err
	}
	targets := []string{
		filepath.Join(root, "scripts", "check-e2e.sh"),
		filepath.Join(root, "test"),
		filepath.Join(root, "web", "e2e"),
	}
	var violations []string
	for _, target := range targets {
		err := filepath.WalkDir(target, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !isGuardSource(path) {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, match := range businessWritePattern.FindAllSubmatchIndex(contents, -1) {
				table := strings.ToLower(string(contents[match[2]:match[3]]))
				if !businessTables[table] {
					continue
				}
				line := 1 + strings.Count(string(contents[:match[0]]), "\n")
				relative, _ := filepath.Rel(root, path)
				violations = append(violations, fmt.Sprintf("%s:%d writes %s", filepath.ToSlash(relative), line, table))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return violations, nil
}

func migrationTableNames(migrationDirectory string) (map[string]bool, error) {
	tables := make(map[string]bool)
	entries, err := os.ReadDir(migrationDirectory)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(migrationDirectory, entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, match := range migrationTablePattern.FindAllSubmatch(contents, -1) {
			tables[strings.ToLower(string(match[1]))] = true
		}
	}
	return tables, nil
}

func isGuardSource(path string) bool {
	switch filepath.Ext(path) {
	case ".go", ".sh", ".sql", ".ts", ".tsx":
		return true
	default:
		return false
	}
}

func findBrokenAcceptanceReferences(root string) ([]string, error) {
	contents, err := os.ReadFile(filepath.Join(root, "test", "acceptance", "matrix.yaml"))
	if err != nil {
		return nil, err
	}
	var matrix struct {
		Entries []struct {
			ID      string  `yaml:"id"`
			Status  string  `yaml:"status"`
			TestRef *string `yaml:"test_ref"`
		} `yaml:"entries"`
	}
	if err := yaml.Unmarshal(contents, &matrix); err != nil {
		return nil, err
	}
	testSources, err := readAcceptanceTestSources(root)
	if err != nil {
		return nil, err
	}

	var violations []string
	for _, entry := range matrix.Entries {
		if entry.Status != "covered" {
			continue
		}
		if entry.TestRef == nil || *entry.TestRef == "" {
			violations = append(violations, entry.ID+" is covered without test_ref")
			continue
		}
		testReference := strings.TrimSpace(*entry.TestRef)
		referenceParts := strings.Split(testReference, "::")
		if len(referenceParts) == 1 {
			underscoredID := strings.ReplaceAll(entry.ID, "-", "_")
			if !strings.Contains(testReference, entry.ID) && !strings.Contains(testReference, underscoredID) {
				violations = append(violations, fmt.Sprintf("%s test_ref %q does not carry its entry ID", entry.ID, testReference))
				continue
			}
			if !strings.Contains(testSources, testReference) {
				violations = append(violations, fmt.Sprintf("%s test_ref %q is absent from test source", entry.ID, testReference))
			}
			continue
		}
		if len(referenceParts) != 2 || referenceParts[0] == "" || referenceParts[1] == "" {
			violations = append(violations, fmt.Sprintf("%s has malformed test_ref %q", entry.ID, testReference))
			continue
		}
		referencedPath, referencedSymbol := referenceParts[0], referenceParts[1]
		if !strings.Contains(referencedSymbol, entry.ID) && !strings.Contains(referencedSymbol, strings.ReplaceAll(entry.ID, "-", "_")) {
			violations = append(violations, fmt.Sprintf("%s test_ref %q does not carry its entry ID", entry.ID, testReference))
		}
		testPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(referencedPath)))
		relativePath, err := filepath.Rel(root, testPath)
		if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
			violations = append(violations, fmt.Sprintf("%s test_ref escapes the repository: %q", entry.ID, testReference))
			continue
		}
		testSource, err := os.ReadFile(testPath)
		if err != nil {
			violations = append(violations, fmt.Sprintf("%s test_ref path %q cannot be read", entry.ID, referencedPath))
			continue
		}
		if strings.HasSuffix(referencedPath, ".go") {
			referencedSymbol, _, _ = strings.Cut(referencedSymbol, " ")
		}
		if !strings.Contains(string(testSource), referencedSymbol) {
			violations = append(violations, fmt.Sprintf("%s test_ref symbol %q is absent from %s", entry.ID, referencedSymbol, referencedPath))
		}
	}
	return violations, nil
}

func readAcceptanceTestSources(root string) (string, error) {
	var sources strings.Builder
	for _, sourceRoot := range []string{filepath.Join(root, "test"), filepath.Join(root, "web", "e2e")} {
		err := filepath.WalkDir(sourceRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || !isGuardSource(path) {
				return nil
			}
			contents, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sources.Write(contents)
			sources.WriteByte('\n')
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return "", err
		}
	}
	return sources.String(), nil
}

type imageService struct {
	Image string `yaml:"image"`
}

func findPlatformDatabaseImageViolations(root string) ([]string, error) {
	var compose struct {
		Services map[string]imageService `yaml:"services"`
	}
	if err := readYAML(filepath.Join(root, "compose.yaml"), &compose); err != nil {
		return nil, err
	}
	allowedComposeFixtures := map[string]string{
		"monitored-pg12": "postgres:12",
		"monitored-pg13": "postgres:13",
		"monitored-pg14": "postgres:14",
		"monitored-pg15": "postgres:15",
		"monitored-pg16": "postgres:16",
		"platform-pg16":  "postgres:16",
	}
	var violations []string
	for name, service := range compose.Services {
		if !strings.HasPrefix(service.Image, "postgres:") || service.Image == "postgres:17" {
			continue
		}
		if allowedComposeFixtures[name] != service.Image {
			violations = append(violations, fmt.Sprintf("compose service %s uses %s", name, service.Image))
		}
	}

	workflowDirectory := filepath.Join(root, ".github", "workflows")
	workflows, err := os.ReadDir(workflowDirectory)
	if err != nil {
		return nil, err
	}
	for _, workflow := range workflows {
		if workflow.IsDir() {
			continue
		}
		switch filepath.Ext(workflow.Name()) {
		case ".yml", ".yaml":
		default:
			continue
		}
		var workflowDocument struct {
			Jobs map[string]struct {
				Services map[string]imageService `yaml:"services"`
			} `yaml:"jobs"`
		}
		if err := readYAML(filepath.Join(workflowDirectory, workflow.Name()), &workflowDocument); err != nil {
			return nil, err
		}
		for jobName, job := range workflowDocument.Jobs {
			for serviceName, service := range job.Services {
				if !strings.HasPrefix(service.Image, "postgres:") || service.Image == "postgres:17" {
					continue
				}
				if serviceName != "postgres12" || service.Image != "postgres:12" {
					violations = append(violations, fmt.Sprintf("workflow %s job %s service %s uses %s", workflow.Name(), jobName, serviceName, service.Image))
				}
			}
		}
	}
	return violations, nil
}

func readYAML(path string, target any) error {
	contents, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := yaml.Unmarshal(contents, target); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func platformDatabaseCompatibility(parentContext context.Context) (int, string, error) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
			envOr("PGUSER", "dbs_monitor"), envOr("PGPASSWORD", "dbs_monitor"),
			envOr("PGHOST", "localhost"), envOr("PGPORT", "55432"), envOr("PGDATABASE", "dbs_monitor"))
	}
	queryContext, cancel := context.WithTimeout(parentContext, 5*time.Second)
	defer cancel()
	connection, err := pgx.Connect(queryContext, databaseURL)
	if err != nil {
		return 0, "", err
	}
	defer connection.Close(context.Background())
	var majorVersion int
	var localeProvider string
	err = connection.QueryRow(queryContext, `SELECT current_setting('server_version_num')::integer / 10000, datlocprovider::text
		FROM pg_database WHERE datname = current_database()`).Scan(&majorVersion, &localeProvider)
	return majorVersion, localeProvider, err
}

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
