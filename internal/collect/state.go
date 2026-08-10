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

type collectedSample struct {
	metricID metric.MetricID
	value    float64
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

func (service *Service) recordStarted(ctx context.Context, run scheduledRun, started time.Time) error {
	_, err := service.platform.Exec(ctx, `INSERT INTO instance_collection_task_state
		(instance_id, task_id, last_due_at, last_started_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (instance_id, task_id) DO UPDATE
		SET last_due_at = EXCLUDED.last_due_at, last_started_at = EXCLUDED.last_started_at`,
		run.target.ID, run.task.ID, run.dueAt, started)
	if err != nil {
		return fmt.Errorf("record collection task start: %w", err)
	}
	return nil
}

func (service *Service) recordUnmet(ctx context.Context, run scheduledRun, result taskResult, nextEligible time.Time) error {
	now := service.clock.Now().UTC()
	code := string(result)
	message := safeErrorMessage(code, nil)
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		if err := lockInstance(ctx, tx, run.target.ID); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO instance_collection_task_state
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
			run.target.ID, run.task.ID, run.dueAt, now, result, nullableTime(nextEligible), code, message)
		if err != nil {
			return err
		}
		return setSourceFailure(ctx, tx, run.target.ID, "COLLECTION_FAILED", message)
	})
}

func (service *Service) recordFailure(ctx context.Context, run scheduledRun, result taskResult, code string, cause error, connectionFailure bool) error {
	finished := service.clock.Now().UTC()
	message := safeErrorMessage(code, cause)
	write := func() error {
		return service.platform.InTx(ctx, func(tx pgx.Tx) error {
			if err := lockInstance(ctx, tx, run.target.ID); err != nil {
				return err
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
			if run.task.Kind == metric.TaskKindProbe {
				taskNextEligible = time.Time{}
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
				failures, nullableTime(taskNextEligible), code, message)
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

func (service *Service) recordSuccess(ctx context.Context, run scheduledRun, samples []collectedSample) error {
	finished := service.clock.Now().UTC()
	write := func() error {
		return service.platform.InTx(ctx, func(tx pgx.Tx) error {
			if err := lockInstance(ctx, tx, run.target.ID); err != nil {
				return err
			}
			for _, sample := range samples {
				if err := insertSample(ctx, tx, run.target.ID, sample.metricID, sample.value, finished); err != nil {
					return err
				}
			}
			_, err := tx.Exec(ctx, `UPDATE instance_collection_task_state SET
				last_due_at = $3,
				last_started_at = $4,
				last_finished_at = $5,
				last_success_at = $5,
				last_result = 'SUCCESS',
				consecutive_failures = 0,
				next_eligible_at = NULL,
				last_error_code = NULL,
				last_error_message = NULL
				WHERE instance_id = $1 AND task_id = $2`,
				run.target.ID, run.task.ID, run.dueAt, run.startedAt, finished)
			if err != nil {
				return err
			}
			if run.task.Kind == metric.TaskKindProbe {
				if _, err := tx.Exec(ctx, `INSERT INTO instance_collection_connection_state (instance_id)
					VALUES ($1) ON CONFLICT (instance_id) DO UPDATE SET
					consecutive_failures = 0, next_eligible_at = NULL,
					last_error_code = NULL, last_error_message = NULL`, run.target.ID); err != nil {
					return err
				}
			}
			var complete bool
			if err := tx.QueryRow(ctx, `SELECT NOT EXISTS (
				SELECT 1 FROM instance_collection_task_state
				WHERE instance_id = $1
				  AND (last_due_at IS NULL OR last_success_at IS NULL OR last_success_at < last_due_at)
			)`, run.target.ID).Scan(&complete); err != nil {
				return err
			}
			if !complete {
				return nil
			}
			return instance.New(tx).SetCollectSuccess(ctx, instance.SetCollectSuccessParams{
				InstanceID:    run.target.ID,
				LastSuccessAt: pgtype.Timestamptz{Time: finished, Valid: true},
			})
		})
	}
	return service.withPartitionRepair(ctx, finished, write)
}

func (service *Service) nextEligible(ctx context.Context, run scheduledRun) (time.Time, error) {
	if run.task.Kind == metric.TaskKindProbe {
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
	seriesID, err := metric.New(tx).UpsertSeries(ctx, metric.UpsertSeriesParams{
		InstanceID: instanceID, MetricID: string(metricID), Labels: json.RawMessage(`{}`), LabelsKey: "{}",
		LastSeen: pgtype.Timestamptz{Time: observedAt, Valid: true},
	})
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, observedAt, value)
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

func nullableTime(value time.Time) any {
	if value.IsZero() {
		return nil
	}
	return value
}
