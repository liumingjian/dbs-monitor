package api_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

type unavailabilityAccount struct {
	code        api.Unavailability
	owner       string
	producer    string
	producerRef string
	evidenceRef string
}

var unavailabilityAccounts = []unavailabilityAccount{
	{api.NOSAMPLESYET, "slice-01", "metric series has not received its first sample", "internal/httpapi/handler.go::api.NOSAMPLESYET", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.NODATAINRANGE, "slice-01", "metric series has no sample in the requested range", "internal/httpapi/handler.go::api.NODATAINRANGE", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.STALE, "slice-01", "latest metric sample is older than its freshness boundary", "internal/httpapi/handler.go::api.STALE", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.COLLECTIONFAILED, "slice-01", "the producing collection task failed", "internal/httpapi/handler.go::api.COLLECTIONFAILED", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.DBUNREACHABLE, "slice-01", "the probe task reports that the monitored database is unreachable", "internal/httpapi/handler.go::api.DBUNREACHABLE", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.AGENTOFFLINE, "slice-01", "the expected Agent has crossed its offline boundary", "internal/httpapi/handler.go::api.AGENTOFFLINE", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.PERMISSIONDENIED, "slice-01", "the collection capability or Agent permission is missing", "internal/httpapi/handler.go::api.PERMISSIONDENIED", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.EXTENSIONMISSING, "slice-01", "the required pg_stat_statements extension is absent", "internal/httpapi/query_statistics.go::api.EXTENSIONMISSING", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.FEATUREDISABLED, "slice-01", "the requested Agent or query-statistics feature is disabled", "internal/httpapi/query_statistics.go::api.FEATUREDISABLED", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.NOTAPPLICABLEROLE, "slice-01", "the metric does not apply to the instance role or topology", "internal/httpapi/handler.go::api.NOTAPPLICABLEROLE", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.COUNTERRESET, "slice-01", "a cumulative source counter decreased between samples", "internal/httpapi/handler.go::api.COUNTERRESET", "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"},
	{api.COLLECTIONPAUSED, "slice-07", "the instance collection pause switch takes precedence over metric state", "internal/httpapi/handler.go::api.COLLECTIONPAUSED", "internal/httpapi/collection_pause_integration_test.go::COLLECTION_PAUSED"},
	{api.VERSIONUNSUPPORTED, "slice-08", "onboarding rejects a real PostgreSQL server older than 13", "internal/httpapi/onboarding.go::api.ONBOARDINGVERSIONUNSUPPORTED", "test/acceptance/matrix.yaml::AC-08-F3"},
}

func TestRegisteredEnumsMatchSpec(t *testing.T) {
	doc := loadSpec(t)

	tests := []struct {
		name string
		got  []string
	}{
		{"Unavailability", mapKeys(map[api.Unavailability]string{
			api.NOSAMPLESYET: "", api.NODATAINRANGE: "", api.STALE: "",
			api.COLLECTIONPAUSED: "", api.COLLECTIONFAILED: "", api.DBUNREACHABLE: "",
			api.AGENTOFFLINE: "", api.PERMISSIONDENIED: "", api.EXTENSIONMISSING: "",
			api.FEATUREDISABLED: "", api.VERSIONUNSUPPORTED: "", api.NOTAPPLICABLEROLE: "",
			api.COUNTERRESET: "",
		})},
		{"AlertStatus", mapKeys(map[api.AlertStatus]string{
			api.OK: "", api.PENDING: "", api.FIRING: "", api.NODATA: "", api.RECOVERED: "",
		})},
		{"HealthStatus", mapKeys(map[api.HealthStatus]string{
			api.HealthCritical: "", api.HealthWarning: "", api.HealthUnknown: "", api.HealthHealthy: "", api.HealthPaused: "",
		})},
		{"InstanceAgentStatus", mapKeys(map[api.InstanceAgentStatus]string{
			api.InstanceAgentOffline: "", api.InstanceAgentOnline: "", api.InstanceAgentNotInstalled: "",
			api.InstanceAgentPermissionDenied: "", api.InstanceAgentError: "",
		})},
		{"CapabilityStatus", mapKeys(map[api.CapabilityStatus]string{
			api.PRESENT: "", api.MISSING: "", api.NOTAPPLICABLE: "", api.UNKNOWN: "",
		})},
		{"AlertAggregation", mapKeys(map[api.AlertAggregation]string{
			api.Latest: "", api.Avg: "", api.Max: "", api.Min: "", api.Sum: "", api.Count: "",
		})},
		{"AlertOperator", mapKeys(map[api.AlertOperator]string{
			api.GreaterThan: "", api.GreaterThanEqual: "", api.LessThan: "", api.LessThanEqual: "",
			api.Equal: "", api.NotEqual: "",
		})},
		{"AlertSeverity", mapKeys(map[api.AlertSeverity]string{
			api.Critical: "", api.Warning: "", api.Info: "",
		})},
		{"NoDataPolicy", mapKeys(map[api.NoDataPolicy]string{
			api.Ignore: "", api.MarkNoData: "",
		})},
		{"AlertRuleScope", mapKeys(map[api.AlertRuleScope]string{
			api.ALL: "", api.INSTANCES: "",
		})},
		{"PlatformHealthStatus", mapKeys(map[api.PlatformHealthStatus]string{
			api.PlatformHealthOK: "", api.PlatformHealthDegraded: "", api.PlatformHealthFailed: "", api.PlatformHealthUnknown: "",
		})},
		{"PlatformHealthSource", mapKeys(map[api.PlatformHealthSource]string{
			api.HealthSourceServerProcess: "", api.HealthSourcePlatformDatabase: "", api.HealthSourceCollectionScheduler: "",
			api.HealthSourcePartitionMaintenance: "", api.HealthSourceTLSCertificate: "", api.HealthSourceAgentIngress: "",
			api.HealthSourceDisk: "", api.HealthSourceCredentialKeyring: "", api.HealthSourceTLS: "",
			api.HealthSourcePlatformDatabaseCapacity: "",
		})},
		{"AlertDisposition", mapKeys(map[api.AlertDisposition]string{
			api.AlertDispositionNONE: "", api.AlertDispositionACKED: "", api.AlertDispositionIGNORED: "",
		})},
		{"IgnoreReasonCode", mapKeys(map[api.IgnoreReasonCode]string{
			api.KNOWNISSUE: "", api.FALSEPOSITIVE: "", api.DUPLICATE: "",
			api.IMPACTACCEPTABLE: "", api.OTHER: "",
		})},
		{"AlertTriggerSnapshotResult", mapKeys(map[api.AlertTriggerSnapshotResult]string{
			api.TriggerSnapshotSuccess: "", api.TriggerSnapshotFailed: "", api.TriggerSnapshotNotApplicable: "",
		})},
		{"PerformanceEventType", mapKeys(map[api.PerformanceEventType]string{
			api.EventLockBlocking: "", api.EventLongTransaction: "", api.EventIdleInTransaction: "",
			api.EventActiveSessionsHigh: "", api.EventReplicationLag: "", api.EventTempFilesSurge: "",
		})},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schema := doc.Components.Schemas[tt.name]
			if schema == nil || schema.Value == nil {
				t.Fatalf("schema %q not found", tt.name)
			}
			want := make([]string, 0, len(schema.Value.Enum))
			for _, value := range schema.Value.Enum {
				text, ok := value.(string)
				if !ok {
					t.Fatalf("%s enum value %v is not a string", tt.name, value)
				}
				want = append(want, text)
			}
			sort.Strings(want)
			sort.Strings(tt.got)
			if !reflect.DeepEqual(tt.got, want) {
				t.Fatalf("Go mapping = %v, spec = %v", tt.got, want)
			}
		})
	}
}

func TestUnavailabilityProducerReconciliation(t *testing.T) {
	doc := loadSpec(t)
	schema := doc.Components.Schemas["Unavailability"]
	if schema == nil || schema.Value == nil {
		t.Fatal("Unavailability schema not found")
	}
	want := make(map[string]bool, len(schema.Value.Enum))
	for _, value := range schema.Value.Enum {
		code, ok := value.(string)
		if !ok {
			t.Fatalf("Unavailability enum value %v is not a string", value)
		}
		want[code] = true
	}

	seen := make(map[string]bool, len(unavailabilityAccounts))
	for _, account := range unavailabilityAccounts {
		code := string(account.code)
		if seen[code] {
			t.Errorf("Unavailability %s is reconciled more than once", code)
		}
		seen[code] = true
		wantOwner := "slice-01"
		if account.code == api.COLLECTIONPAUSED {
			wantOwner = "slice-07"
		} else if account.code == api.VERSIONUNSUPPORTED {
			wantOwner = "slice-08"
		}
		if account.owner != wantOwner {
			t.Errorf("Unavailability %s owner = %s, want %s", code, account.owner, wantOwner)
		}
		if account.producer == "" {
			t.Errorf("Unavailability %s has no named producer", code)
		}
		assertSourceReference(t, account.producerRef)
		assertSourceReference(t, account.evidenceRef)
	}
	if !reflect.DeepEqual(seen, want) {
		t.Fatalf("reconciled Unavailability codes = %v, want OpenAPI set %v", mapKeys(seen), mapKeys(want))
	}
}

func assertSourceReference(t *testing.T, reference string) {
	t.Helper()
	parts := strings.SplitN(reference, "::", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		t.Fatalf("invalid source reference %q", reference)
	}
	contents, err := os.ReadFile(filepath.Join(projectRoot(t), filepath.FromSlash(parts[0])))
	if err != nil {
		t.Fatalf("read source reference %s: %v", reference, err)
	}
	if !strings.Contains(string(contents), parts[1]) {
		t.Errorf("source reference %s does not resolve", reference)
	}
}

func loadSpec(t *testing.T) *openapi3.T {
	t.Helper()
	path := filepath.Join(projectRoot(t), "api", "openapi.bundled.yaml")
	doc, err := openapi3.NewLoader().LoadFromFile(path)
	if err != nil {
		t.Fatalf("load OpenAPI spec: %v", err)
	}
	return doc
}

func projectRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working directory: %v", err)
	}
	return filepath.Clean(filepath.Join(cwd, "..", ".."))
}

func mapKeys[T ~string, V any](values map[T]V) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, string(value))
	}
	return keys
}
