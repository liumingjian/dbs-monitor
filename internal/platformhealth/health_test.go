package platformhealth

import (
	"bytes"
	"encoding/json"
	"log"
	"strings"
	"testing"
	"time"
)

func TestAggregateStatusPrecedence(t *testing.T) {
	tests := []struct {
		name     string
		statuses []Status
		want     Status
	}{
		{name: "empty is unknown", want: StatusUnknown},
		{name: "all okay", statuses: []Status{StatusOK, StatusOK}, want: StatusOK},
		{name: "degraded outranks okay", statuses: []Status{StatusOK, StatusDegraded}, want: StatusDegraded},
		{name: "unknown outranks degraded", statuses: []Status{StatusDegraded, StatusUnknown}, want: StatusUnknown},
		{name: "failed outranks unknown", statuses: []Status{StatusUnknown, StatusFailed}, want: StatusFailed},
		{name: "invalid fact is unknown", statuses: []Status{StatusOK, Status("INVALID")}, want: StatusUnknown},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := AggregateStatus(test.statuses); got != test.want {
				t.Fatalf("AggregateStatus(%v) = %s, want %s", test.statuses, got, test.want)
			}
		})
	}
}

func TestSourceClassifications(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		got    SourceSnapshot
		source Source
		status Status
		code   string
	}{
		{
			name:   "scheduler healthy",
			got:    SchedulerSource(SchedulerFacts{ProbeCapacity: 32, QueryCapacity: 32}),
			source: SourceCollectionScheduler, status: StatusOK, code: "SCHEDULER_RUNNING",
		},
		{
			name:   "scheduler saturation preserves detail",
			got:    SchedulerSource(SchedulerFacts{ProbeCapacity: 1, ProbeActive: 1, Pending: 3, SkippedBackpressure: 7}),
			source: SourceCollectionScheduler, status: StatusDegraded, code: "SCHEDULER_BACKPRESSURE",
		},
		{
			name:   "scheduler refresh stopped",
			got:    SchedulerSource(SchedulerFacts{RefreshFailed: true}),
			source: SourceCollectionScheduler, status: StatusFailed, code: "SCHEDULER_REFRESH_FAILED",
		},
		{
			name:   "certificate unavailable",
			got:    CertificateSource(now, nil),
			source: SourceTLSCertificate, status: StatusUnknown, code: "CERTIFICATE_UNAVAILABLE",
		},
		{
			name:   "certificate warning window",
			got:    CertificateSource(now, timePointer(now.Add(30*24*time.Hour))),
			source: SourceTLSCertificate, status: StatusDegraded, code: "CERTIFICATE_EXPIRING",
		},
		{
			name:   "certificate expired",
			got:    CertificateSource(now, timePointer(now.Add(-time.Second))),
			source: SourceTLSCertificate, status: StatusFailed, code: "CERTIFICATE_EXPIRED",
		},
		{
			name:   "credential keyring unavailable",
			got:    CredentialSource(CredentialFacts{}),
			source: SourceCredentialKeyring, status: StatusUnknown, code: "CREDENTIAL_KEYRING_UNAVAILABLE",
		},
		{
			name:   "credential keyring ready",
			got:    CredentialSource(CredentialFacts{Available: true}),
			source: SourceCredentialKeyring, status: StatusOK, code: "CREDENTIAL_KEYRING_READY",
		},
		{
			name:   "credential keyring failed",
			got:    CredentialSource(CredentialFacts{Available: true, FailureCode: "UNKNOWN_KEY_VERSION"}),
			source: SourceCredentialKeyring, status: StatusFailed, code: "UNKNOWN_KEY_VERSION",
		},
		{
			name:   "partitions ready",
			got:    PartitionSource(PartitionFacts{PrebuildDaysRemaining: 7}),
			source: SourcePartitionMaintenance, status: StatusOK, code: "PARTITIONS_READY",
		},
		{
			name:   "partition maintenance failed",
			got:    PartitionSource(PartitionFacts{ConsecutiveFailures: 1, PrebuildDaysRemaining: 6}),
			source: SourcePartitionMaintenance, status: StatusDegraded, code: "PARTITION_MAINTENANCE_FAILED",
		},
		{
			name:   "partition write failed",
			got:    PartitionSource(PartitionFacts{ConsecutiveFailures: 1, WriteFailed: true}),
			source: SourcePartitionMaintenance, status: StatusFailed, code: "PARTITION_WRITE_FAILED",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if test.got.Source != test.source || test.got.Status != test.status || test.got.Code != test.code {
				t.Fatalf("source = %+v, want source/status/code %s/%s/%s", test.got, test.source, test.status, test.code)
			}
		})
	}

	saturated := SchedulerSource(SchedulerFacts{Pending: 3, SkippedBackpressure: 7})
	if saturated.Pending == nil || *saturated.Pending != 3 || saturated.SkippedBackpressure == nil || *saturated.SkippedBackpressure != 7 {
		t.Fatalf("scheduler detail = %+v, want pending=3 skipped_backpressure=7", saturated)
	}
}

func TestStoreWritesStructuredSecretFreeJournalEvents(t *testing.T) {
	var output bytes.Buffer
	logger := log.New(&output, "", 0)
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	store := NewStore("3.0.0", now.Add(-time.Hour), logger)

	store.Update(now, SchedulerSource(SchedulerFacts{Pending: 2, SkippedBackpressure: 4}))
	store.PublishSummary(now)

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("journal lines = %d, want change and summary: %q", len(lines), output.String())
	}
	for _, line := range lines {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("journal line is not JSON: %v: %q", err, line)
		}
		if event["event"] == nil || event["assembled_at"] == nil || event["status"] == nil {
			t.Fatalf("journal event missing stable fields: %v", event)
		}
		lower := strings.ToLower(line)
		for _, forbidden := range []string{"password", "ciphertext", "master_key", "token", "authorization", "dsn", "request_body", "raw_sql"} {
			if strings.Contains(lower, forbidden) {
				t.Fatalf("journal event contains forbidden secret marker %q: %s", forbidden, line)
			}
		}
	}
}

func TestPublishSummaryEmitsCertificateLevelChange(t *testing.T) {
	var output bytes.Buffer
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	store := NewStore("3.0.0", now.Add(-time.Hour), log.New(&output, "", 0))
	store.Update(now, CertificateSource(now, timePointer(now.Add(31*24*time.Hour))))
	output.Reset()

	store.PublishSummary(now.Add(2 * 24 * time.Hour))

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 || !strings.Contains(lines[0], `"event":"platform_health_change"`) ||
		!strings.Contains(lines[0], `"source":"TLS_CERTIFICATE"`) || !strings.Contains(lines[0], `"status":"DEGRADED"`) {
		t.Fatalf("certificate transition journal = %q, want change event followed by summary", output.String())
	}
}

func timePointer(value time.Time) *time.Time { return &value }
