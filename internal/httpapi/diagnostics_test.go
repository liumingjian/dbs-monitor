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
	health := platformhealth.NewStore("3.0.0", now.Add(-time.Hour), nil)
	health.Update(now, platformhealth.DiskSource(91, platformhealth.DiskNormal, platformhealth.DefaultDiskThresholds()))
	health.Update(now, platformhealth.SchedulerSource(platformhealth.SchedulerFacts{Pending: 2}))
	health.Update(now, platformhealth.PartitionSource(platformhealth.PartitionFacts{PrebuildDaysRemaining: 6}))
	health.Update(now, platformhealth.CertificateSource(now, diagnosticTimePointer(now.Add(15*24*time.Hour))))
	health.Update(now, platformhealth.CredentialSource(platformhealth.CredentialFacts{Available: true}))
	handler := NewHandlerWithPlatformHealth(nil, nil, nil, nil, "3.0.0", health)

	diskResponse, _ := handler.GetDiskDiagnostics(context.Background(), api.GetDiskDiagnosticsRequestObject{})
	schedulerResponse, _ := handler.GetSchedulerDiagnostics(context.Background(), api.GetSchedulerDiagnosticsRequestObject{})
	partitionResponse, _ := handler.GetPartitionDiagnostics(context.Background(), api.GetPartitionDiagnosticsRequestObject{})
	certificateResponse, _ := handler.GetCertificateDiagnostics(context.Background(), api.GetCertificateDiagnosticsRequestObject{})
	keyringResponse, _ := handler.GetKeyringDiagnostics(context.Background(), api.GetKeyringDiagnosticsRequestObject{})
	platformResponse, _ := handler.GetPlatformDiagnostics(context.Background(), api.GetPlatformDiagnosticsRequestObject{})

	sources := []api.PlatformHealthSourceSnapshot{
		api.PlatformHealthSourceSnapshot(diskResponse.(api.GetDiskDiagnostics200JSONResponse)),
		api.PlatformHealthSourceSnapshot(schedulerResponse.(api.GetSchedulerDiagnostics200JSONResponse)),
		api.PlatformHealthSourceSnapshot(partitionResponse.(api.GetPartitionDiagnostics200JSONResponse)),
		api.PlatformHealthSourceSnapshot(certificateResponse.(api.GetCertificateDiagnostics200JSONResponse)),
		api.PlatformHealthSourceSnapshot(keyringResponse.(api.GetKeyringDiagnostics200JSONResponse)),
		api.PlatformHealthSourceSnapshot(platformResponse.(api.GetPlatformDiagnostics200JSONResponse)),
	}
	want := []api.PlatformHealthSource{
		api.HealthSourceDisk,
		api.HealthSourceCollectionScheduler,
		api.HealthSourcePartitionMaintenance,
		api.HealthSourceTLSCertificate,
		api.HealthSourceCredentialKeyring,
		api.HealthSourceServerProcess,
	}
	for index := range want {
		if sources[index].Source != want[index] {
			t.Errorf("diagnostic source %d = %s, want %s", index, sources[index].Source, want[index])
		}
	}
}

func diagnosticTimePointer(value time.Time) *time.Time { return &value }
