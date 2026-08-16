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

const (
	defaultUnavailabilityOwner = "slice-01"
	collectionPauseOwner       = "slice-07"
	versionUnsupportedOwner    = "slice-08"
	issue60EvidenceReference   = "internal/httpapi/issue60_integration_test.go::TestIssue60DerivedMetricsAndRealUnavailabilityProducers"
)

type unavailabilityReconciliation struct {
	code                api.Unavailability
	owner               string
	producerDescription string
	producerReference   string
	evidenceReference   string
}

var unavailabilityReconciliations = []unavailabilityReconciliation{
	{
		code:                api.NOSAMPLESYET,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "metric series has not received its first sample",
		producerReference:   "internal/httpapi/handler.go::api.NOSAMPLESYET",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.NODATAINRANGE,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "metric series has no sample in the requested range",
		producerReference:   "internal/httpapi/handler.go::api.NODATAINRANGE",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.STALE,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "latest metric sample is older than its freshness boundary",
		producerReference:   "internal/httpapi/handler.go::api.STALE",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.COLLECTIONFAILED,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "the producing collection task failed",
		producerReference:   "internal/httpapi/handler.go::api.COLLECTIONFAILED",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.DBUNREACHABLE,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "the probe task reports that the monitored database is unreachable",
		producerReference:   "internal/httpapi/handler.go::api.DBUNREACHABLE",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.AGENTOFFLINE,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "the expected Agent has crossed its offline boundary",
		producerReference:   "internal/httpapi/handler.go::api.AGENTOFFLINE",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.PERMISSIONDENIED,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "the collection capability or Agent permission is missing",
		producerReference:   "internal/httpapi/handler.go::api.PERMISSIONDENIED",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.EXTENSIONMISSING,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "the required pg_stat_statements extension is absent",
		producerReference:   "internal/httpapi/query_statistics.go::api.EXTENSIONMISSING",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.FEATUREDISABLED,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "the requested Agent or query-statistics feature is disabled",
		producerReference:   "internal/httpapi/query_statistics.go::api.FEATUREDISABLED",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.NOTAPPLICABLEROLE,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "the metric does not apply to the instance role or topology",
		producerReference:   "internal/httpapi/handler.go::api.NOTAPPLICABLEROLE",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.COUNTERRESET,
		owner:               defaultUnavailabilityOwner,
		producerDescription: "a cumulative source counter decreased between samples",
		producerReference:   "internal/httpapi/handler.go::api.COUNTERRESET",
		evidenceReference:   issue60EvidenceReference,
	},
	{
		code:                api.COLLECTIONPAUSED,
		owner:               collectionPauseOwner,
		producerDescription: "the instance collection pause switch takes precedence over metric state",
		producerReference:   "internal/httpapi/handler.go::api.COLLECTIONPAUSED",
		evidenceReference:   "internal/httpapi/collection_pause_integration_test.go::COLLECTION_PAUSED",
	},
	{
		code:                api.VERSIONUNSUPPORTED,
		owner:               versionUnsupportedOwner,
		producerDescription: "onboarding rejects a real PostgreSQL server older than 13",
		producerReference:   "internal/httpapi/onboarding.go::api.ONBOARDINGVERSIONUNSUPPORTED",
		evidenceReference:   "test/acceptance/matrix.yaml::AC-08-F3",
	},
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
	openAPICodes := make(map[string]bool, len(schema.Value.Enum))
	for _, value := range schema.Value.Enum {
		code, ok := value.(string)
		if !ok {
			t.Fatalf("Unavailability enum value %v is not a string", value)
		}
		openAPICodes[code] = true
	}

	reconciledCodes := make(map[string]bool, len(unavailabilityReconciliations))
	for _, reconciliation := range unavailabilityReconciliations {
		code := string(reconciliation.code)
		if reconciledCodes[code] {
			t.Errorf("Unavailability %s is reconciled more than once", code)
		}
		reconciledCodes[code] = true
		wantOwner := expectedUnavailabilityOwner(reconciliation.code)
		if reconciliation.owner != wantOwner {
			t.Errorf("Unavailability %s owner = %s, want %s", code, reconciliation.owner, wantOwner)
		}
		if reconciliation.producerDescription == "" {
			t.Errorf("Unavailability %s has no named producer", code)
		}
		assertSourceReference(t, reconciliation.producerReference)
		assertSourceReference(t, reconciliation.evidenceReference)
	}
	if !reflect.DeepEqual(reconciledCodes, openAPICodes) {
		t.Fatalf("reconciled Unavailability codes = %v, want OpenAPI set %v", mapKeys(reconciledCodes), mapKeys(openAPICodes))
	}
}

func expectedUnavailabilityOwner(code api.Unavailability) string {
	switch code {
	case api.COLLECTIONPAUSED:
		return collectionPauseOwner
	case api.VERSIONUNSUPPORTED:
		return versionUnsupportedOwner
	default:
		return defaultUnavailabilityOwner
	}
}

func assertSourceReference(t *testing.T, reference string) {
	t.Helper()
	referenceParts := strings.SplitN(reference, "::", 2)
	if len(referenceParts) != 2 || referenceParts[0] == "" || referenceParts[1] == "" {
		t.Fatalf("invalid source reference %q", reference)
	}
	sourcePath, searchText := referenceParts[0], referenceParts[1]
	contents, err := os.ReadFile(filepath.Join(projectRoot(t), filepath.FromSlash(sourcePath)))
	if err != nil {
		t.Fatalf("read source reference %s: %v", reference, err)
	}
	if !strings.Contains(string(contents), searchText) {
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
