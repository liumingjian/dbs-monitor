package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

type taskResult string

const (
	resultSuccess             taskResult = "SUCCESS"
	resultFailed              taskResult = "FAILED"
	resultTimedOut            taskResult = "TIMED_OUT"
	resultSkippedBackpressure taskResult = "SKIPPED_BACKPRESSURE"
	resultBackoff             taskResult = "BACKOFF"
)

const (
	errorCodeConnectionFailed = "CONNECTION_FAILED"
	errorCodeQueryFailed      = "QUERY_FAILED"
	errorCodeTimeout          = "TIMEOUT"
	errorCodeCounterReset     = string(metric.ResetCounter)
	errorCodeDiskEmergency    = "DISK_EMERGENCY_WATERMARK"
)

type collectedSample struct {
	metricID metric.MetricID
	value    float64
	labels   map[string]string
}

type collectedBatch struct {
	samples              []collectedSample
	statActivitySnapshot *statActivitySnapshot
	counterReset         bool
}

func (service *Service) ensureTaskStates(ctx context.Context, targetID pgtype.UUID) error {
	for _, task := range scheduledTasks() {
		if _, err := service.platform.Exec(ctx, `INSERT INTO instance_collection_task_state (instance_id, task_id)
			VALUES ($1, $2) ON CONFLICT (instance_id, task_id) DO NOTHING`, targetID, task.ID); err != nil {
			return fmt.Errorf("initialize collection task state: %w", err)
		}
	}
	return nil
}

func (service *Service) recordStarted(ctx context.Context, run scheduledRun) (bool, error) {
	recorded := false
	err := service.platform.InTx(ctx, func(tx pgx.Tx) error {
		collectionActive, err := lockInstanceAndCheckCollectionActive(ctx, tx, run.target.ID)
		if err != nil {
			return err
		}
		if !collectionActive {
			return nil
		}
		if _, err := tx.Exec(ctx, `INSERT INTO instance_collection_task_state
			(instance_id, task_id, last_due_at, last_started_at)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (instance_id, task_id) DO UPDATE
			SET last_due_at = EXCLUDED.last_due_at, last_started_at = EXCLUDED.last_started_at`,
			run.target.ID, run.task.ID, run.dueAt, run.startedAt); err != nil {
			return err
		}
		recorded = true
		return nil
	})
	if err != nil {
		return false, fmt.Errorf("record collection task start: %w", err)
	}
	return recorded, nil
}

func (service *Service) recordUnmet(ctx context.Context, run scheduledRun, result taskResult, nextEligible time.Time) error {
	now := service.clock.Now().UTC()
	code := string(result)
	message := collectionErrorMessage(code)
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		collectionActive, err := lockInstanceAndCheckCollectionActive(ctx, tx, run.target.ID)
		if err != nil {
			return err
		}
		if !collectionActive {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO instance_collection_task_state
			(instance_id, task_id, last_due_at, last_finished_at, last_result, next_eligible_at, last_error_code, last_error_message)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (instance_id, task_id) DO UPDATE SET
			last_due_at = EXCLUDED.last_due_at,
			last_finished_at = EXCLUDED.last_finished_at,
			last_result = EXCLUDED.last_result,
			next_eligible_at = CASE
				WHEN EXCLUDED.last_result = 'BACKOFF' THEN instance_collection_task_state.next_eligible_at
				ELSE EXCLUDED.next_eligible_at
			END,
			last_error_code = EXCLUDED.last_error_code,
			last_error_message = EXCLUDED.last_error_message`,
			run.target.ID, run.task.ID, run.dueAt, now, result, nullableTimestamp(nextEligible), code, message)
		if err != nil {
			return err
		}
		return setSourceFailure(ctx, tx, run.target.ID, "COLLECTION_FAILED", message)
	})
}

func (service *Service) recordCapabilityBlocked(ctx context.Context, run scheduledRun, reason metric.CapabilityBlockReason) error {
	finished := service.clock.Now().UTC()
	code := string(reason)
	message := collectionErrorMessage(code)
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		collectionActive, err := lockInstanceAndCheckCollectionActive(ctx, tx, run.target.ID)
		if err != nil {
			return err
		}
		if !collectionActive {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO instance_collection_task_state
			(instance_id, task_id, last_due_at, last_finished_at, last_result, consecutive_failures,
			 next_eligible_at, last_error_code, last_error_message)
			VALUES ($1, $2, $3, $4, 'FAILED', 0, NULL, $5, $6)
			ON CONFLICT (instance_id, task_id) DO UPDATE SET
			last_due_at = EXCLUDED.last_due_at,
			last_finished_at = EXCLUDED.last_finished_at,
			last_result = EXCLUDED.last_result,
			consecutive_failures = 0,
			next_eligible_at = NULL,
			last_error_code = EXCLUDED.last_error_code,
			last_error_message = EXCLUDED.last_error_message`,
			run.target.ID, run.task.ID, run.dueAt, finished, code, message)
		if err != nil {
			return err
		}
		return setSourceFailure(ctx, tx, run.target.ID, code, message)
	})
}

func (service *Service) recordCapabilityNotApplicable(ctx context.Context, run scheduledRun, reason metric.CapabilityBlockReason) error {
	finished := service.clock.Now().UTC()
	code := string(reason)
	message := collectionErrorMessage(code)
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		collectionActive, err := lockInstanceAndCheckCollectionActive(ctx, tx, run.target.ID)
		if err != nil {
			return err
		}
		if !collectionActive {
			return nil
		}
		_, err = tx.Exec(ctx, `INSERT INTO instance_collection_task_state
			(instance_id, task_id, last_due_at, last_finished_at, last_success_at, last_result,
			 consecutive_failures, next_eligible_at, last_error_code, last_error_message)
			VALUES ($1, $2, $3, $4, $4, 'SUCCESS', 0, NULL, $5, $6)
			ON CONFLICT (instance_id, task_id) DO UPDATE SET
			last_due_at = EXCLUDED.last_due_at,
			last_finished_at = EXCLUDED.last_finished_at,
			last_success_at = EXCLUDED.last_success_at,
			last_result = EXCLUDED.last_result,
			consecutive_failures = 0,
			next_eligible_at = NULL,
			last_error_code = EXCLUDED.last_error_code,
			last_error_message = EXCLUDED.last_error_message`,
			run.target.ID, run.task.ID, run.dueAt, finished, code, message)
		if err != nil {
			return err
		}
		return advanceCollectionWatermarkIfComplete(ctx, tx, run.target.ID, finished)
	})
}

func (service *Service) recordFailure(ctx context.Context, run scheduledRun, result taskResult, code string, connectionFailure bool) error {
	finished := service.clock.Now().UTC()
	message := collectionErrorMessage(code)
	write := func() error {
		return service.platform.InTx(ctx, func(tx pgx.Tx) error {
			collectionActive, err := lockInstanceAndCheckCollectionActive(ctx, tx, run.target.ID)
			if err != nil {
				return err
			}
			if !collectionActive {
				return nil
			}
			failures, err := taskFailureCount(ctx, tx, run.target.ID, run.task.ID)
			if err != nil {
				return err
			}
			failures++
			var taskNextEligible time.Time
			if run.task.Kind != metric.TaskKindProbe && !connectionFailure {
				taskNextEligible = finished.Add(failureBackoff(run.task.Kind, run.interval, failures))
			}
			if connectionFailure {
				connectionFailures, err := connectionFailureCount(ctx, tx, run.target.ID)
				if err != nil {
					return err
				}
				connectionFailures++
				connectionNextEligible := finished.Add(failureBackoff(metric.TaskKindSQL, run.interval, connectionFailures))
				if _, err := tx.Exec(ctx, `INSERT INTO instance_collection_connection_state
					(instance_id, consecutive_failures, next_eligible_at, last_error_code, last_error_message)
					VALUES ($1, $2, $3, $4, $5)
					ON CONFLICT (instance_id) DO UPDATE SET
					consecutive_failures = EXCLUDED.consecutive_failures,
					next_eligible_at = EXCLUDED.next_eligible_at,
					last_error_code = EXCLUDED.last_error_code,
					last_error_message = EXCLUDED.last_error_message`,
					run.target.ID, connectionFailures, connectionNextEligible, code, message); err != nil {
					return err
				}
			}
			_, err = tx.Exec(ctx, `INSERT INTO instance_collection_task_state
				(instance_id, task_id, last_due_at, last_started_at, last_finished_at, last_result,
				 consecutive_failures, next_eligible_at, last_error_code, last_error_message)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
				ON CONFLICT (instance_id, task_id) DO UPDATE SET
				last_due_at = EXCLUDED.last_due_at,
				last_started_at = EXCLUDED.last_started_at,
				last_finished_at = EXCLUDED.last_finished_at,
				last_result = EXCLUDED.last_result,
				consecutive_failures = EXCLUDED.consecutive_failures,
				next_eligible_at = EXCLUDED.next_eligible_at,
				last_error_code = EXCLUDED.last_error_code,
				last_error_message = EXCLUDED.last_error_message`,
				run.target.ID, run.task.ID, run.dueAt, run.startedAt, finished, result,
				failures, nullableTimestamp(taskNextEligible), code, message)
			if err != nil {
				return err
			}
			if run.task.Kind == metric.TaskKindProbe {
				if err := insertSample(ctx, tx, run.target.ID, metric.MetricAvailabilityReachable,
					metric.NonNumericMetricEncodings[metric.MetricAvailabilityReachable.String()]["unreachable"], finished); err != nil {
					return err
				}
				return setSourceFailure(ctx, tx, run.target.ID, "DB_UNREACHABLE", message)
			}
			return setSourceFailure(ctx, tx, run.target.ID, "COLLECTION_FAILED", message)
		})
	}
	return service.withPartitionRepair(ctx, finished, write)
}

func (service *Service) recordSuccess(ctx context.Context, run scheduledRun, batch collectedBatch) error {
	finished := service.clock.Now().UTC()
	write := func() error {
		return service.platform.InTx(ctx, func(tx pgx.Tx) error {
			collectionActive, err := lockInstanceAndCheckCollectionActive(ctx, tx, run.target.ID)
			if err != nil {
				return err
			}
			if !collectionActive {
				return nil
			}
			for _, sample := range batch.samples {
				if err := insertCollectedSample(ctx, tx, run.target.ID, sample, finished); err != nil {
					return err
				}
			}
			if batch.statActivitySnapshot != nil {
				if err := persistStatActivitySnapshot(ctx, tx, run.target.ID, finished, *batch.statActivitySnapshot); err != nil {
					return err
				}
			}
			lastErrorCode := pgtype.Text{}
			lastErrorMessage := pgtype.Text{}
			if batch.counterReset {
				lastErrorCode = pgtype.Text{String: errorCodeCounterReset, Valid: true}
				lastErrorMessage = pgtype.Text{String: collectionErrorMessage(errorCodeCounterReset), Valid: true}
			}
			_, err = tx.Exec(ctx, `UPDATE instance_collection_task_state SET
				last_due_at = $3,
				last_started_at = $4,
				last_finished_at = $5,
				last_success_at = $5,
				last_result = 'SUCCESS',
				consecutive_failures = 0,
				next_eligible_at = NULL,
				last_error_code = $6,
				last_error_message = $7
				WHERE instance_id = $1 AND task_id = $2`,
				run.target.ID, run.task.ID, run.dueAt, run.startedAt, finished, lastErrorCode, lastErrorMessage)
			if err != nil {
				return err
			}
			if batch.counterReset {
				if err := setSourceFailure(ctx, tx, run.target.ID, lastErrorCode.String, lastErrorMessage.String); err != nil {
					return err
				}
			}
			if run.task.Kind == metric.TaskKindProbe {
				if _, err := tx.Exec(ctx, `INSERT INTO instance_collection_connection_state (instance_id)
					VALUES ($1) ON CONFLICT (instance_id) DO UPDATE SET
					consecutive_failures = 0, next_eligible_at = NULL,
					last_error_code = NULL, last_error_message = NULL`, run.target.ID); err != nil {
					return err
				}
			}
			return advanceCollectionWatermarkIfComplete(ctx, tx, run.target.ID, finished)
		})
	}
	return service.withPartitionRepair(ctx, finished, write)
}

func advanceCollectionWatermarkIfComplete(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID, finished time.Time) error {
	var complete bool
	if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (
		SELECT 1 FROM instance_collection_task_state
		WHERE instance_id = $1
		  AND (last_due_at IS NULL OR last_success_at IS NULL OR last_success_at < last_due_at)
	)`, instanceID).Scan(&complete); err != nil {
		return err
	}
	if !complete {
		return nil
	}
	var counterResetPending bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM instance_collection_task_state
		WHERE instance_id = $1 AND last_error_code = $2
	)`, instanceID, errorCodeCounterReset).Scan(&counterResetPending); err != nil {
		return err
	}
	if counterResetPending {
		_, err := tx.Exec(ctx, `INSERT INTO instance_collect_state (instance_id, source, last_success_at)
			VALUES ($1, 'SERVER_DIRECT', $2)
			ON CONFLICT (instance_id, source) DO UPDATE SET last_success_at = EXCLUDED.last_success_at`, instanceID, finished)
		return err
	}
	return instance.New(tx).SetCollectSuccess(ctx, instance.SetCollectSuccessParams{
		InstanceID:    instanceID,
		LastSuccessAt: pgtype.Timestamptz{Time: finished, Valid: true},
	})
}

func (service *Service) recordDiskEmergency(ctx context.Context, run scheduledRun) error {
	finished := service.clock.Now().UTC()
	message := collectionErrorMessage(errorCodeDiskEmergency)
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		collectionActive, err := lockInstanceAndCheckCollectionActive(ctx, tx, run.target.ID)
		if err != nil {
			return err
		}
		if !collectionActive {
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE instance_collection_task_state SET
			last_due_at = $3,
			last_started_at = $4,
			last_finished_at = $5,
			last_result = 'FAILED',
			consecutive_failures = consecutive_failures + 1,
			next_eligible_at = NULL,
			last_error_code = $6,
			last_error_message = $7
			WHERE instance_id = $1 AND task_id = $2`,
			run.target.ID, run.task.ID, run.dueAt, run.startedAt, finished, errorCodeDiskEmergency, message); err != nil {
			return err
		}
		return setSourceFailure(ctx, tx, run.target.ID, errorCodeDiskEmergency, message)
	})
}

func (service *Service) nextEligible(ctx context.Context, run scheduledRun) (time.Time, error) {
	if run.task.Kind == metric.TaskKindProbe || isCapabilitySnapshotTask(run.task) {
		return time.Time{}, nil
	}
	var taskNext, connectionNext pgtype.Timestamptz
	err := service.platform.QueryRow(ctx, `SELECT task.next_eligible_at, connection.next_eligible_at
		FROM instance_collection_task_state task
		LEFT JOIN instance_collection_connection_state connection ON connection.instance_id = task.instance_id
		WHERE task.instance_id = $1 AND task.task_id = $2`, run.target.ID, run.task.ID).Scan(&taskNext, &connectionNext)
	if errors.Is(err, pgx.ErrNoRows) {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	if taskNext.Valid && (!connectionNext.Valid || taskNext.Time.After(connectionNext.Time)) {
		return taskNext.Time.UTC(), nil
	}
	if connectionNext.Valid {
		return connectionNext.Time.UTC(), nil
	}
	return time.Time{}, nil
}

func insertSample(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID, metricID metric.MetricID, value float64, observedAt time.Time) error {
	return insertCollectedSample(ctx, tx, instanceID, collectedSample{metricID: metricID, value: value}, observedAt)
}

func insertCollectedSample(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID, sample collectedSample, observedAt time.Time) error {
	labels := sample.labels
	if labels == nil {
		labels = map[string]string{}
	}
	encodedLabels, err := json.Marshal(labels)
	if err != nil {
		return fmt.Errorf("encode metric labels: %w", err)
	}
	seriesID, err := metric.New(tx).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: instanceID, MetricID: string(sample.metricID), Labels: encodedLabels, LabelsKey: string(encodedLabels),
		LastSeen: pgtype.Timestamptz{Time: observedAt, Valid: true},
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, observedAt, sample.value)
	return err
}

func setSourceFailure(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID, code, message string) error {
	return instance.New(tx).SetCollectFailure(ctx, instance.SetCollectFailureParams{
		InstanceID:       instanceID,
		LastErrorCode:    pgtype.Text{String: code, Valid: true},
		LastErrorMessage: pgtype.Text{String: message, Valid: true},
	})
}

func lockInstance(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID) error {
	var locked pgtype.UUID
	return tx.QueryRow(ctx, "SELECT id FROM instance WHERE id = $1 FOR UPDATE", instanceID).Scan(&locked)
}

func lockInstanceAndCheckCollectionActive(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID) (bool, error) {
	if err := lockInstance(ctx, tx, instanceID); err != nil {
		return false, err
	}
	var paused bool
	if err := tx.QueryRow(ctx, `SELECT collection_paused FROM instance_collection_config
		WHERE instance_id = $1 FOR SHARE`, instanceID).Scan(&paused); err != nil {
		return false, err
	}
	return !paused, nil
}

func taskFailureCount(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID, taskID metric.TaskID) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT consecutive_failures FROM instance_collection_task_state
		WHERE instance_id = $1 AND task_id = $2 FOR UPDATE`, instanceID, taskID).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return count, err
}

func connectionFailureCount(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID) (int, error) {
	var count int
	err := tx.QueryRow(ctx, `SELECT consecutive_failures FROM instance_collection_connection_state
		WHERE instance_id = $1 FOR UPDATE`, instanceID).Scan(&count)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return count, err
}

func nullableTimestamp(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{
		Time:  value,
		Valid: !value.IsZero(),
	}
}

func collectionErrorMessage(code string) string {
	switch code {
	case errorCodeConnectionFailed:
		return "target connection failed"
	case errorCodeQueryFailed:
		return "collection query failed"
	case errorCodeTimeout:
		return "collection deadline exceeded"
	case errorCodeCounterReset:
		return "database statistics counters reset"
	case errorCodeDiskEmergency:
		return "sample writes rejected at disk emergency watermark"
	case string(metric.CapabilityBlockPermissionDenied):
		return "required database role is missing"
	case string(metric.CapabilityBlockExtensionMissing):
		return "required database extension is missing"
	case string(metric.CapabilityBlockFeatureDisabled):
		return "required database feature is not enabled"
	case string(metric.CapabilityBlockNotApplicableRole):
		return "collection task does not apply to this database role or topology"
	case string(resultSkippedBackpressure):
		return "collection skipped because scheduler capacity was unavailable"
	case string(resultBackoff):
		return "collection deferred by failure backoff"
	default:
		return "collection failed"
	}
}
