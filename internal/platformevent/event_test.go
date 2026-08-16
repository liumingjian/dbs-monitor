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

var forbiddenFieldNames = []string{
	"password",
	"old_password",
	"new_password",
	"initial_password",
	"password_hash",
	"password_ciphertext",
}

func TestPlatformEventKindsAreClosed(t *testing.T) {
	wantKinds := []string{
		"LOGIN_SUCCEEDED",
		"LOGIN_FAILED",
		"USER_CREATED",
		"USER_STATUS_CHANGED",
		"USER_STATUS_CHANGE_REJECTED",
		"USER_ROLE_CHANGED",
		"USER_PASSWORD_RESET",
		"INSTANCE_CREDENTIAL_UPDATED",
		"INSTANCE_REMOVED",
		"MASTER_KEY_ROTATED",
	}
	gotKinds := append([]string(nil), eventKinds...)
	sort.Strings(gotKinds)
	sort.Strings(wantKinds)
	if strings.Join(gotKinds, "\n") != strings.Join(wantKinds, "\n") {
		t.Fatalf("platform event kinds = %v, want %v", gotKinds, wantKinds)
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
	for _, field := range forbiddenFieldNames {
		fieldPattern := regexp.MustCompile(`(?mi)^\s*` + regexp.QuoteMeta(field) + `\s+`)
		if fieldPattern.MatchString(tableMatch[1]) {
			t.Errorf("platform_event contains forbidden field %q", field)
		}
	}
}
