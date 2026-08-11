package api_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/liumingjian/dbs-monitor/internal/api"
)

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
			api.HealthSourceDisk: "", api.HealthSourceCredentialKeyring: "",
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

func mapKeys[T ~string](values map[T]string) []string {
	keys := make([]string, 0, len(values))
	for value := range values {
		keys = append(keys, string(value))
	}
	return keys
}
