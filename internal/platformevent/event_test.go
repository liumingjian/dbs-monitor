package platformevent

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

func TestSecurityEventContractIsClosedAndSecretFree(t *testing.T) {
	wantKinds := []string{
		"LOGIN_SUCCEEDED",
		"LOGIN_FAILED",
		"USER_CREATED",
		"USER_STATUS_CHANGED",
		"USER_ROLE_CHANGED",
		"USER_PASSWORD_RESET",
		"INSTANCE_CREDENTIAL_UPDATED",
		"INSTANCE_REMOVED",
		"MASTER_KEY_ROTATED",
	}
	gotKinds := append([]string(nil), Kinds...)
	sort.Strings(gotKinds)
	sort.Strings(wantKinds)
	if strings.Join(gotKinds, "\n") != strings.Join(wantKinds, "\n") {
		t.Fatalf("platform event kinds = %v, want %v", gotKinds, wantKinds)
	}

	wantForbidden := []string{
		"initial_password",
		"new_password",
		"old_password",
		"password",
		"password_ciphertext",
		"password_hash",
	}
	gotForbidden := append([]string(nil), ForbiddenFieldNames...)
	sort.Strings(gotForbidden)
	sort.Strings(wantForbidden)
	if strings.Join(gotForbidden, "\n") != strings.Join(wantForbidden, "\n") {
		t.Fatalf("platform event forbidden fields = %v, want %v", gotForbidden, wantForbidden)
	}
}

func TestMigrationsUsePlatformEventsWithoutAuditLogOrSecretColumns(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test filename")
	}
	migrationRoot := filepath.Join(filepath.Dir(filename), "..", "..", "migrations")
	entries, err := os.ReadDir(migrationRoot)
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var allMigrations strings.Builder
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join(migrationRoot, entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		allMigrations.Write(contents)
		allMigrations.WriteByte('\n')
	}
	migrations := allMigrations.String()
	if regexp.MustCompile(`(?i)create\s+table\s+audit_log\b`).MatchString(migrations) {
		t.Fatal("migrations create forbidden audit_log table")
	}
	tableMatch := regexp.MustCompile(`(?is)create\s+table\s+platform_event\s*\((.*?)\);`).FindStringSubmatch(migrations)
	if tableMatch == nil {
		t.Fatal("migrations do not create platform_event")
	}
	for _, field := range ForbiddenFieldNames {
		fieldPattern := regexp.MustCompile(`(?mi)^\s*` + regexp.QuoteMeta(field) + `\s+`)
		if fieldPattern.MatchString(tableMatch[1]) {
			t.Errorf("platform_event contains forbidden field %q", field)
		}
	}
}
