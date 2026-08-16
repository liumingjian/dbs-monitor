package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/notify"
	"github.com/liumingjian/dbs-monitor/internal/platformevent"
)

const localStorageReclaimerActor = "system:local-storage-reclaimer"

type localArtifactReclamationEvent struct {
	Kind         string    `json:"kind"`
	ArtifactName string    `json:"artifact_name"`
	Bytes        int64     `json:"bytes"`
	OccurredAt   time.Time `json:"occurred_at"`
}

type localArtifactReclamationRecorder func(localArtifactReclamationEvent) error

func reclaimLocalArtifacts(
	bundleDirectory string,
	bundleLimit int,
	snapshotStore *notify.ChannelSnapshotStore,
	snapshotLimit int,
	now time.Time,
	record localArtifactReclamationRecorder,
) error {
	if bundleLimit <= 0 || snapshotLimit <= 0 {
		return errors.New("local artifact retention limits must be positive")
	}
	if record == nil {
		return errors.New("local artifact reclamation requires an event recorder")
	}
	bundles, err := listDiagnosticBundles(bundleDirectory)
	if err != nil {
		return err
	}
	for _, bundle := range bundles[:max(0, len(bundles)-bundleLimit)] {
		if err := deleteDiagnosticBundle(bundleDirectory, bundle.Name, now, func(deletion diagnosticBundleDeletionEvent) error {
			return record(localArtifactReclamationEvent{
				Kind:         platformevent.DiagnosticBundleReclaimed,
				ArtifactName: deletion.BundleName,
				Bytes:        deletion.Bytes,
				OccurredAt:   deletion.DeletedAt,
			})
		}); err != nil {
			return err
		}
	}
	if snapshotStore == nil {
		return nil
	}
	snapshots, err := snapshotStore.ListArtifacts()
	if err != nil {
		return err
	}
	for _, snapshot := range snapshots[:max(0, len(snapshots)-snapshotLimit)] {
		if err := snapshotStore.DeleteArtifact(snapshot.Name, now, func(deletion notify.ChannelSnapshotDeletionEvent) error {
			return record(localArtifactReclamationEvent{
				Kind:         platformevent.NotificationSnapshotReclaimed,
				ArtifactName: deletion.SnapshotName,
				Bytes:        deletion.Bytes,
				OccurredAt:   deletion.DeletedAt,
			})
		}); err != nil {
			return err
		}
	}
	return nil
}

func databaseReclamationRecorder(ctx context.Context, database db.DBTX, logger *log.Logger) localArtifactReclamationRecorder {
	return func(event localArtifactReclamationEvent) error {
		if err := platformevent.Record(ctx, database, platformevent.Event{
			Kind:         event.Kind,
			OccurredAt:   event.OccurredAt,
			ActorSubject: localStorageReclaimerActor,
		}); err != nil {
			return fmt.Errorf("record local artifact reclamation platform event: %w", err)
		}
		if logger != nil {
			encoded, err := json.Marshal(event)
			if err == nil {
				logger.Print(string(encoded))
			}
		}
		return nil
	}
}
