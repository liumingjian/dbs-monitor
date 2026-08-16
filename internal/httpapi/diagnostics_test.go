package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/api"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
)

func TestFocusedDiagnosticsReadOnlyInMemorySources(t *testing.T) {
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	certificateExpiresAt := now.Add(15 * 24 * time.Hour)
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Hour), nil)
	health.Update(now, platformhealth.DiskSource(91, platformhealth.DiskNormal, platformhealth.DefaultDiskThresholds()))
	health.Update(now, platformhealth.PlatformDatabaseCapacitySource(96, 100, platformhealth.DiskNormal, platformhealth.DefaultDiskThresholds()))
	health.Update(now, platformhealth.SchedulerSource(platformhealth.SchedulerFacts{Pending: 2}))
	health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{PrebuildDaysRemaining: 6}))
	health.Update(now, platformhealth.CertificateSource(now, &certificateExpiresAt))
	health.Update(now, platformhealth.CredentialSource(platformhealth.CredentialFacts{Available: true}))
	handler := NewHandlerWithPlatformHealth(nil, nil, nil, nil, "3.0.0", health)
	ctx := context.Background()

	diskResponse, diskErr := handler.GetDiskDiagnostics(ctx, api.GetDiskDiagnosticsRequestObject{})
	schedulerResponse, schedulerErr := handler.GetSchedulerDiagnostics(ctx, api.GetSchedulerDiagnosticsRequestObject{})
	partitionResponse, partitionErr := handler.GetPartitionDiagnostics(ctx, api.GetPartitionDiagnosticsRequestObject{})
	certificateResponse, certificateErr := handler.GetCertificateDiagnostics(ctx, api.GetCertificateDiagnosticsRequestObject{})
	keyringResponse, keyringErr := handler.GetKeyringDiagnostics(ctx, api.GetKeyringDiagnosticsRequestObject{})
	platformResponse, platformErr := handler.GetPlatformDiagnostics(ctx, api.GetPlatformDiagnosticsRequestObject{})
	healthResponse, healthErr := handler.GetPlatformHealth(ctx, api.GetPlatformHealthRequestObject{})

	assertDiagnosticSource(t, "disk", diskResponse, diskErr, api.HealthSourceDisk)
	assertDiagnosticSource(t, "scheduler", schedulerResponse, schedulerErr, api.HealthSourceCollectionScheduler)
	assertDiagnosticSource(t, "partitions", partitionResponse, partitionErr, api.HealthSourcePartitionMaintenance)
	assertDiagnosticSource(t, "certificate", certificateResponse, certificateErr, api.HealthSourceTLSCertificate)
	assertDiagnosticSource(t, "keyring", keyringResponse, keyringErr, api.HealthSourceCredentialKeyring)
	assertDiagnosticSource(t, "platform", platformResponse, platformErr, api.HealthSourceServerProcess)
	if healthErr != nil {
		t.Fatalf("get platform health: %v", healthErr)
	}
	healthSnapshot := healthResponse.(api.GetPlatformHealth200JSONResponse)
	capacity := healthSnapshot.Sources[len(healthSnapshot.Sources)-1]
	if capacity.Source != api.HealthSourcePlatformDatabaseCapacity || capacity.CapacityUsagePercent == nil ||
		*capacity.CapacityUsagePercent != 96 || capacity.CapacityBudgetBytes == nil || *capacity.CapacityBudgetBytes != 100 {
		t.Fatalf("platform database capacity health facts = %+v", capacity)
	}
	certificate, ok := certificateResponse.(api.GetCertificateDiagnostics200JSONResponse)
	if !ok {
		t.Fatalf("get certificate diagnostics returned %T", certificateResponse)
	}
	if certificate.ValidityDaysRemaining == nil {
		t.Fatal("certificate validity days remaining is unavailable, want 15")
	}
	if got := *certificate.ValidityDaysRemaining; got != 15 {
		t.Fatalf("certificate validity days remaining = %d, want 15", got)
	}
}

func assertDiagnosticSource(t *testing.T, name string, response any, responseErr error, want api.PlatformHealthSource) {
	t.Helper()
	if responseErr != nil {
		t.Fatalf("get %s diagnostics: %v", name, responseErr)
	}

	var source api.PlatformHealthSourceSnapshot
	switch response := response.(type) {
	case api.GetDiskDiagnostics200JSONResponse:
		source = api.PlatformHealthSourceSnapshot(response)
	case api.GetSchedulerDiagnostics200JSONResponse:
		source = api.PlatformHealthSourceSnapshot(response)
	case api.GetPartitionDiagnostics200JSONResponse:
		source = api.PlatformHealthSourceSnapshot(response)
	case api.GetCertificateDiagnostics200JSONResponse:
		source = api.PlatformHealthSourceSnapshot(response)
	case api.GetKeyringDiagnostics200JSONResponse:
		source = api.PlatformHealthSourceSnapshot(response)
	case api.GetPlatformDiagnostics200JSONResponse:
		source = api.PlatformHealthSourceSnapshot(response)
	default:
		t.Fatalf("get %s diagnostics returned %T", name, response)
	}
	if source.Source != want {
		t.Errorf("%s diagnostic source = %s, want %s", name, source.Source, want)
	}
}
