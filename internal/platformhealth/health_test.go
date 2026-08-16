package platformhealth

import (
	"bytes"
	"encoding/json"
	"errors"
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

func TestClassifyDiskLevel(t *testing.T) {
	thresholds := DefaultDiskThresholds()
	tests := []struct {
		name     string
		usage    float64
		previous DiskLevel
		want     DiskLevel
	}{
		{name: "below warning", usage: 79.9, previous: DiskNormal, want: DiskNormal},
		{name: "warning threshold", usage: 80, previous: DiskNormal, want: DiskWarning},
		{name: "critical threshold", usage: 90, previous: DiskWarning, want: DiskCritical},
		{name: "emergency threshold", usage: 95, previous: DiskCritical, want: DiskEmergency},
		{name: "direct emergency jump", usage: 96, previous: DiskNormal, want: DiskEmergency},
		{name: "emergency held inside hysteresis", usage: 93, previous: DiskEmergency, want: DiskEmergency},
		{name: "emergency clears below hysteresis", usage: 92.9, previous: DiskEmergency, want: DiskCritical},
		{name: "critical held inside hysteresis", usage: 88, previous: DiskCritical, want: DiskCritical},
		{name: "critical clears below hysteresis", usage: 87.9, previous: DiskCritical, want: DiskWarning},
		{name: "warning held inside hysteresis", usage: 78, previous: DiskWarning, want: DiskWarning},
		{name: "warning clears below hysteresis", usage: 77.9, previous: DiskWarning, want: DiskNormal},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ClassifyDiskLevel(test.usage, test.previous, thresholds); got != test.want {
				t.Fatalf("ClassifyDiskLevel(%v, %s) = %s, want %s", test.usage, test.previous, got, test.want)
			}
		})
	}
}

func TestDiskSourceTransitionsAreVisible(t *testing.T) {
	var output bytes.Buffer
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	store := NewStore("3.0.0", now.Add(-time.Hour), log.New(&output, "", 0))
	thresholds := DefaultDiskThresholds()

	store.Update(now, DiskSource(80, store.DiskLevel(), thresholds))
	store.Update(now.Add(time.Minute), DiskSource(90, store.DiskLevel(), thresholds))
	store.Update(now.Add(2*time.Minute), DiskSource(95, store.DiskLevel(), thresholds))

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("disk journal lines = %d, want one per level transition: %q", len(lines), output.String())
	}
	for index, code := range []string{"DISK_WARNING_WATERMARK", "DISK_CRITICAL_WATERMARK", "DISK_EMERGENCY_WATERMARK"} {
		if !strings.Contains(lines[index], `"source":"DISK"`) || !strings.Contains(lines[index], `"code":"`+code+`"`) {
			t.Errorf("disk journal line %d = %q, want %s", index, lines[index], code)
		}
	}

	disk := store.Source(SourceDisk)
	if disk.Status != StatusFailed || disk.DiskLevel == nil || *disk.DiskLevel != DiskEmergency ||
		disk.DiskUsagePercent == nil || *disk.DiskUsagePercent != 95 || !store.RejectSampleWrites() {
		t.Fatalf("emergency disk source = %+v reject=%t", disk, store.RejectSampleWrites())
	}
	store.Update(now.Add(3*time.Minute), DiskUnavailableSource(store.DiskLevel()))
	if source := store.Source(SourceDisk); source.Status != StatusUnknown || !store.RejectSampleWrites() {
		t.Fatalf("unavailable disk sample source = %+v reject=%t, want UNKNOWN with emergency rejection latched", source, store.RejectSampleWrites())
	}
}

func TestStoreIncludesTenHealthSubsystems(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	store := NewStore("3.0.0", now.Add(-time.Hour), nil)
	want := []Source{
		SourceServerProcess,
		SourcePlatformDatabase,
		SourceCollectionScheduler,
		SourcePartitionMaintenance,
		SourceTLSCertificate,
		SourceAgentIngress,
		SourceDisk,
		SourceCredentialKeyring,
		SourceTLS,
		SourcePlatformDatabaseCapacity,
	}

	sources := store.Current().Sources
	if len(sources) != len(want) {
		t.Fatalf("health sources = %d, want %d: %+v", len(sources), len(want), sources)
	}
	for index, source := range sources {
		if source.Source != want[index] {
			t.Errorf("health source %d = %s, want %s", index, source.Source, want[index])
		}
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
			source: SourceTLSCertificate, status: StatusDegraded, code: "CERTIFICATE_UNAVAILABLE",
		},
		{
			name:   "certificate warning window",
			got:    CertificateSource(now, timePointer(now.Add(30*24*time.Hour))),
			source: SourceTLSCertificate, status: StatusDegraded, code: "CERTIFICATE_EXPIRING",
		},
		{
			name:   "certificate expired",
			got:    CertificateSource(now, timePointer(now.Add(-time.Second))),
			source: SourceTLSCertificate, status: StatusDegraded, code: "CERTIFICATE_EXPIRED",
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

func TestCertificateSourceReportsRemainingValidityWithoutFailedState(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name          string
		expiresAt     *time.Time
		wantRemaining *int
	}{
		{name: "unavailable"},
		{name: "valid", expiresAt: timePointer(now.Add(365 * 24 * time.Hour)), wantRemaining: intPointer(365)},
		{name: "expiring", expiresAt: timePointer(now.Add(20 * 24 * time.Hour)), wantRemaining: intPointer(20)},
		{name: "expired", expiresAt: timePointer(now.Add(-time.Second)), wantRemaining: intPointer(0)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := CertificateSource(now, test.expiresAt)
			if source.Status == StatusFailed {
				t.Fatalf("certificate status = %s, TLS facts must not use FAILED", source.Status)
			}
			if test.wantRemaining == nil {
				if source.ValidityDaysRemaining != nil {
					t.Fatalf("validity days remaining = %d, want unavailable", *source.ValidityDaysRemaining)
				}
				return
			}
			if source.ValidityDaysRemaining == nil {
				t.Fatalf("validity days remaining is unavailable, want %d", *test.wantRemaining)
			}
			if got := *source.ValidityDaysRemaining; got != *test.wantRemaining {
				t.Fatalf("validity days remaining = %d, want %d", got, *test.wantRemaining)
			}
		})
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

func TestStoreRecordsCredentialKeyGenerationEvent(t *testing.T) {
	const path = "/etc/dbs-monitor/credentials/master-key-v1"

	var output bytes.Buffer
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	store := NewStore("3.0.0", now.Add(-time.Minute), log.New(&output, "", 0))
	store.Update(now, CredentialSource(CredentialFacts{Available: true}))
	output.Reset()

	store.RecordCredentialKeyGenerated(now, 1, path)

	var event credentialKeyGeneratedEvent
	if err := json.Unmarshal(output.Bytes(), &event); err != nil {
		t.Fatalf("decode credential key generation event: %v", err)
	}
	if event.Event != "credential_key_generated" || !event.ObservedAt.Equal(now) ||
		event.KeyVersion != 1 || event.Path != path {
		t.Fatalf("credential key generation event = %+v", event)
	}
	keyring := store.Source(SourceCredentialKeyring)
	if keyring.Status != StatusOK || keyring.Code != "CREDENTIAL_KEYRING_READY" {
		t.Fatalf("credential keyring health = %+v, want OK after generation", keyring)
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
		!strings.Contains(lines[0], `"source":"TLS_CERTIFICATE"`) || !strings.Contains(lines[0], `"status":"DEGRADED"`) ||
		!strings.Contains(lines[0], `"validity_days_remaining":29`) {
		t.Fatalf("certificate transition journal = %q, want change event followed by summary", output.String())
	}
}

func TestStoreObservesNewFailedFactsOnce(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	store := NewStore("3.0.0", now.Add(-time.Hour), nil)
	var failures []FailureFact
	store.SetFailureObserver(func(failure FailureFact) {
		failures = append(failures, failure)
	})

	failed := DatabaseSource(errors.New("unavailable"))
	store.Update(now, failed)
	store.Update(now.Add(time.Second), failed)
	store.PublishSummary(now.Add(2 * time.Second))
	if len(failures) != 1 || failures[0].Source != SourcePlatformDatabase ||
		failures[0].Code != "PLATFORM_DATABASE_UNREACHABLE" || !failures[0].ObservedAt.Equal(now) {
		t.Fatalf("observed failures = %+v, want one platform database failure", failures)
	}

	store.Update(now.Add(3*time.Second), DatabaseSource(nil))
	store.Update(now.Add(4*time.Second), failed)
	if len(failures) != 2 || !failures[1].ObservedAt.Equal(now.Add(4*time.Second)) {
		t.Fatalf("observed failures after recovery = %+v, want a second transition", failures)
	}
}

func timePointer(value time.Time) *time.Time { return &value }
