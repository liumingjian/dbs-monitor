package capability

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

type probeResult struct {
	capability metric.Capability
	present    bool
	err        error
}

func ProbeAndStoreSnapshot(targetCtx, platformCtx context.Context, platform db.DBTX, conn *monitorpg.TargetConn, instanceID pgtype.UUID, observedAt time.Time) (bool, error) {
	states, complete := ProbeSnapshot(targetCtx, conn)
	if err := StoreSnapshot(platformCtx, platform, instanceID, observedAt, states); err != nil {
		return false, err
	}
	return complete, nil
}

func ProbeSnapshot(targetCtx context.Context, conn *monitorpg.TargetConn) (map[metric.CapabilityID]metric.CapabilityStatus, bool) {
	results := make([]probeResult, 0, len(metric.Capabilities))
	for _, declaration := range metric.Capabilities {
		var present bool
		err := conn.QueryRow(targetCtx, declaration.Probe).Scan(&present)
		results = append(results, probeResult{capability: declaration, present: present, err: err})
		if err != nil {
			break
		}
	}
	return snapshotFromProbeResults(results)
}

func StoreUnknown(ctx context.Context, platform db.DBTX, instanceID pgtype.UUID, observedAt time.Time) error {
	return StoreSnapshot(ctx, platform, instanceID, observedAt, metric.UnknownCapabilityStates())
}

func StoreSnapshot(ctx context.Context, platform db.DBTX, instanceID pgtype.UUID, observedAt time.Time, states map[metric.CapabilityID]metric.CapabilityStatus) error {
	encoded, err := json.Marshal(states)
	if err != nil {
		return fmt.Errorf("encode capability snapshot: %w", err)
	}
	_, err = platform.Exec(ctx, `INSERT INTO instance_capability_snapshot (instance_id, observed_at, states)
		VALUES ($1, $2, $3)
		ON CONFLICT (instance_id) DO UPDATE SET observed_at = EXCLUDED.observed_at, states = EXCLUDED.states`,
		instanceID, observedAt.UTC(), encoded)
	if err != nil {
		return fmt.Errorf("store capability snapshot: %w", err)
	}
	return nil
}

func snapshotFromProbeResults(results []probeResult) (map[metric.CapabilityID]metric.CapabilityStatus, bool) {
	if !completeProbeResults(results) {
		return metric.UnknownCapabilityStates(), false
	}
	states := make(map[metric.CapabilityID]metric.CapabilityStatus, len(results))
	for _, result := range results {
		switch {
		case result.present:
			states[result.capability.ID] = metric.CapabilityPresent
		case result.capability.Class == metric.CapabilityClassFixable:
			states[result.capability.ID] = metric.CapabilityMissing
		default:
			states[result.capability.ID] = metric.CapabilityNotApplicable
		}
	}
	return states, true
}

func completeProbeResults(results []probeResult) bool {
	if len(results) != len(metric.Capabilities) {
		return false
	}
	seen := make(map[metric.CapabilityID]struct{}, len(results))
	for _, result := range results {
		if result.err != nil {
			return false
		}
		seen[result.capability.ID] = struct{}{}
	}
	return len(seen) == len(metric.Capabilities)
}
