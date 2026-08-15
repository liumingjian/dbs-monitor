package platformdb

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestEvaluateRejectsEachFatalPrerequisiteWithADistinctCode(t *testing.T) {
	valid := validPreflightFacts()
	tests := []struct {
		name string
		edit func(*Facts)
		code Code
	}{
		{name: "major version", edit: func(facts *Facts) { facts.ServerVersionNum = 160010 }, code: CodeVersionUnsupported},
		{name: "encoding", edit: func(facts *Facts) { facts.Encoding = "LATIN1" }, code: CodeEncodingUnsupported},
		{name: "schema create permission", edit: func(facts *Facts) { facts.CanCreateSchema = false }, code: CodeSchemaCreateDenied},
		{name: "schema cleanliness", edit: func(facts *Facts) { facts.UnexpectedObjects = []string{"customer_table"} }, code: CodeSchemaNotClean},
		{name: "TLS", edit: func(facts *Facts) { facts.TLSActive = false }, code: CodeTLSInactive},
	}

	gotCodes := make([]Code, 0, len(tests))
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			facts := valid
			test.edit(&facts)
			report := Evaluate(facts)
			if len(report.Fatal) != 1 || report.Fatal[0].Code != test.code {
				t.Fatalf("fatal findings = %+v, want only %s", report.Fatal, test.code)
			}
			if report.Fatal[0].Message == "" {
				t.Fatal("fatal finding has no actionable message")
			}
			gotCodes = append(gotCodes, report.Fatal[0].Code)
		})
	}

	wantCodes := []Code{
		CodeVersionUnsupported,
		CodeEncodingUnsupported,
		CodeSchemaCreateDenied,
		CodeSchemaNotClean,
		CodeTLSInactive,
	}
	if !slices.Equal(gotCodes, wantCodes) {
		t.Fatalf("fatal codes = %v, want distinct codes %v", gotCodes, wantCodes)
	}
}

func TestEvaluateEnumeratesEachAdvisoryPrerequisite(t *testing.T) {
	facts := validPreflightFacts()
	facts.ServerVersionNum = 170009
	facts.LocaleProvider = "i"
	facts.Collation = "zh-CN-x-icu"
	facts.CharacterType = "zh-CN-x-icu"
	facts.TimeZone = "Asia/Shanghai"
	facts.OtherDatabaseCount = 2
	report := Evaluate(facts)

	if len(report.Fatal) != 0 {
		t.Fatalf("advisory facts produced fatal findings: %+v", report.Fatal)
	}
	want := []Code{
		CodeLocaleNonstandard,
		CodeTimeZoneNonUTC,
		CodeInstanceShared,
		CodeMinorVersionOutdated,
	}
	got := make([]Code, 0, len(report.Warnings))
	for _, warning := range report.Warnings {
		got = append(got, warning.Code)
		if warning.Message == "" {
			t.Fatalf("warning %s has no actionable message", warning.Code)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("warning codes = %v, want %v", got, want)
	}
}

func TestPlatformObjectAllowlistCoversMigrationObjects(t *testing.T) {
	allowedRelations := nameSet(platformRelations)
	allowedFunctions := nameSet(platformFunctions)
	allowedTriggers := nameSet(platformTriggers)
	for _, name := range []string{
		"goose_db_version", "goose_db_version_id_seq", "metric_series_series_id_seq",
		"alert_event_id_seq", "notification_attempt_id_seq", "notification_policy_channel_id_seq",
	} {
		if !allowedRelations[name] {
			t.Errorf("generated platform relation %q is absent from preflight allowlist", name)
		}
	}

	entries, err := os.ReadDir(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	patterns := []struct {
		kind    string
		pattern *regexp.Regexp
		allowed map[string]bool
	}{
		{kind: "relation", pattern: regexp.MustCompile(`(?m)^CREATE TABLE ([a-z_][a-z0-9_]*)`), allowed: allowedRelations},
		{kind: "function", pattern: regexp.MustCompile(`(?m)^CREATE FUNCTION ([a-z_][a-z0-9_]*)`), allowed: allowedFunctions},
		{kind: "trigger", pattern: regexp.MustCompile(`(?m)^CREATE TRIGGER ([a-z_][a-z0-9_]*)`), allowed: allowedTriggers},
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", entry.Name()))
		if err != nil {
			t.Fatalf("read migration %s: %v", entry.Name(), err)
		}
		for _, object := range patterns {
			for _, match := range object.pattern.FindAllSubmatch(contents, -1) {
				name := string(match[1])
				if !object.allowed[name] {
					t.Errorf("migration %s creates %s %q absent from preflight allowlist", entry.Name(), object.kind, name)
				}
			}
		}
	}
}

func validPreflightFacts() Facts {
	return Facts{
		ServerVersionNum: 170010,
		Encoding:         "UTF8",
		CanCreateSchema:  true,
		TLSActive:        true,
		LocaleProvider:   "c",
		Collation:        "C",
		CharacterType:    "C",
		TimeZone:         "UTC",
	}
}

func nameSet(names []string) map[string]bool {
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}
