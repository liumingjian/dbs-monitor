package evaluator

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

type Service struct {
	platform               *db.Pool
	clock                  clock.Clock
	withSnapshotConnection func(context.Context, alerting.GetEvaluationTargetRow, func(*monitorpg.TargetConn) error) error
}

type metricEvaluation struct {
	outcome        alerting.Evaluation
	currentValue   pgtype.Float8
	unavailability pgtype.Text
}

func New(
	platform *db.Pool,
	currentClock clock.Clock,
	withSnapshotConnection func(context.Context, alerting.GetEvaluationTargetRow, func(*monitorpg.TargetConn) error) error,
) *Service {
	return &Service{platform: platform, clock: currentClock, withSnapshotConnection: withSnapshotConnection}
}

func (service *Service) RunOnce(ctx context.Context) error {
	now := service.clock.Now().UTC()
	targets, err := alerting.New(service.platform).ListEvaluationTargets(ctx, pgtype.Timestamptz{Time: now, Valid: true})
	if err != nil {
		return fmt.Errorf("list evaluation targets: %w", err)
	}
	for start := 0; start < len(targets); {
		end := start + 1
		for end < len(targets) && targets[end].InstanceID == targets[start].InstanceID {
			end++
		}
		if err := service.evaluateInstance(ctx, targets[start:end], now); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func (service *Service) evaluateInstance(ctx context.Context, targets []alerting.ListEvaluationTargetsRow, now time.Time) error {
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		for _, target := range targets {
			if err := service.evaluateRule(ctx, queries, target, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (service *Service) evaluateRule(
	ctx context.Context,
	queries *alerting.Queries,
	scheduledTarget alerting.ListEvaluationTargetsRow,
	now time.Time,
) error {
	evaluationTarget, err := queries.GetEvaluationTarget(ctx, alerting.GetEvaluationTargetParams{
		RuleID:             scheduledTarget.RuleID,
		InstanceID:         scheduledTarget.InstanceID,
		MetricDimensionKey: scheduledTarget.MetricDimensionKey,
	})
	if err != nil {
		return fmt.Errorf("read evaluation target: %w", err)
	}

	currentSnapshot := snapshotFromEvaluationTarget(evaluationTarget)
	previousState := currentSnapshot.State
	evaluatedAt := pgtype.Timestamptz{Time: now, Valid: true}
	evaluationCheckpoint := alerting.MarkAlertRuleEvaluatedParams{
		RuleID:             evaluationTarget.RuleID,
		InstanceID:         evaluationTarget.InstanceID,
		MetricDimensionKey: scheduledTarget.MetricDimensionKey,
		LastEvaluatedAt:    evaluatedAt,
	}
	if structurallyNotApplicable(
		metric.MetricID(evaluationTarget.MetricID),
		evaluationTarget.LastErrorCode,
		evaluationTarget.AgentMetricsEnabled,
	) {
		return queries.MarkAlertRuleEvaluated(ctx, evaluationCheckpoint)
	}

	metricResult, err := evaluateMetric(ctx, queries, evaluationTarget, scheduledTarget.MetricDimensionKey, now)
	if err != nil {
		return err
	}
	if metricResult.outcome == alerting.Missing && evaluationTarget.NoDataPolicy == "ignore" {
		if evaluationTarget.AlertInstanceID.Valid && currentSnapshot.State != alerting.RECOVERED {
			nextSnapshot := alerting.Step(
				currentSnapshot,
				alerting.MissingIgnored,
				int(evaluationTarget.ConsecutiveCount),
				int(evaluationTarget.RecoveryConsecutiveCount),
			)
			if err := queries.ResetIgnoredMissingAlert(ctx, alerting.ResetIgnoredMissingAlertParams{
				ID:            evaluationTarget.AlertInstanceID,
				RuleVersion:   evaluationTarget.Version,
				Severity:      evaluationTarget.Severity,
				RuleSnapshot:  evaluationTarget.RuleSnapshot,
				BreachCount:   int32(nextSnapshot.BreachCount),
				RecoveryCount: int32(nextSnapshot.RecoveryCount),
				NoDataCount:   int32(nextSnapshot.NoDataCount),
				UpdatedAt:     evaluatedAt,
			}); err != nil {
				return fmt.Errorf("reset alert counters for ignored missing data: %w", err)
			}
		}
		return queries.MarkAlertRuleEvaluated(ctx, evaluationCheckpoint)
	}
	if currentSnapshot.State == alerting.RECOVERED && metricResult.outcome != alerting.Breaching {
		return queries.MarkAlertRuleEvaluated(ctx, evaluationCheckpoint)
	}

	nextSnapshot := alerting.Step(
		currentSnapshot,
		metricResult.outcome,
		int(evaluationTarget.ConsecutiveCount),
		int(evaluationTarget.RecoveryConsecutiveCount),
	)
	stateBeforeNoData := pgtype.Text{}
	if nextSnapshot.StateBeforeNoData != "" {
		stateBeforeNoData = pgtype.Text{String: string(nextSnapshot.StateBeforeNoData), Valid: true}
	}
	if nextSnapshot.State != alerting.NO_DATA {
		metricResult.unavailability = pgtype.Text{}
	}
	firstTriggeredAt := pgtype.Timestamptz{}
	firstRuleVersion := pgtype.Int4{}
	var firstRuleSnapshot []byte
	firstFiring := previousState != alerting.FIRING &&
		!(previousState == alerting.NO_DATA && currentSnapshot.StateBeforeNoData == alerting.FIRING) &&
		nextSnapshot.State == alerting.FIRING
	if firstFiring {
		firstTriggeredAt = evaluatedAt
		firstRuleVersion = pgtype.Int4{Int32: evaluationTarget.Version, Valid: true}
		firstRuleSnapshot = evaluationTarget.RuleSnapshot
	}
	recoveredAt := pgtype.Timestamptz{}
	if previousState != alerting.RECOVERED && nextSnapshot.State == alerting.RECOVERED {
		recoveredAt = evaluatedAt
	}
	var alertInstanceID pgtype.UUID
	if nextSnapshot.State == alerting.RECOVERED {
		alertInstanceID, err = queries.RecoverAlertSnapshot(ctx, alerting.RecoverAlertSnapshotParams{
			ID:            evaluationTarget.AlertInstanceID,
			MetricID:      evaluationTarget.MetricID,
			RuleVersion:   evaluationTarget.Version,
			Severity:      evaluationTarget.Severity,
			CurrentValue:  metricResult.currentValue,
			RuleSnapshot:  evaluationTarget.RuleSnapshot,
			BreachCount:   int32(nextSnapshot.BreachCount),
			RecoveryCount: int32(nextSnapshot.RecoveryCount),
			NoDataCount:   int32(nextSnapshot.NoDataCount),
			UpdatedAt:     recoveredAt,
		})
	} else {
		alertInstanceID, err = queries.SaveAlertSnapshot(ctx, alerting.SaveAlertSnapshotParams{
			RuleID:             evaluationTarget.RuleID,
			InstanceID:         evaluationTarget.InstanceID,
			MetricID:           evaluationTarget.MetricID,
			MetricDimensionKey: scheduledTarget.MetricDimensionKey,
			Status:             string(nextSnapshot.State),
			RuleVersion:        evaluationTarget.Version,
			Severity:           evaluationTarget.Severity,
			CurrentValue:       metricResult.currentValue,
			RuleSnapshot:       evaluationTarget.RuleSnapshot,
			BreachCount:        int32(nextSnapshot.BreachCount),
			RecoveryCount:      int32(nextSnapshot.RecoveryCount),
			NoDataCount:        int32(nextSnapshot.NoDataCount),
			StateBeforeNoData:  stateBeforeNoData,
			Unavailability:     metricResult.unavailability,
			UpdatedAt:          evaluatedAt,
			FirstTriggeredAt:   firstTriggeredAt,
			FirstRuleVersion:   firstRuleVersion,
			FirstRuleSnapshot:  firstRuleSnapshot,
			RecoveredAt:        recoveredAt,
		})
	}
	if err != nil {
		return fmt.Errorf("save alert state: %w", err)
	}
	var triggerSnapshotID pgtype.UUID
	_, triggerSnapshotApplicable := alerting.TriggerSnapshotScopeForMetric(evaluationTarget.MetricID)
	if firstFiring && triggerSnapshotApplicable {
		capture := service.captureTriggerSnapshot(ctx, evaluationTarget)
		triggerSnapshotID, err = queries.CreateTriggerSnapshot(ctx, alerting.CreateTriggerSnapshotParams{
			AlertInstanceID: alertInstanceID, CapturedAt: evaluatedAt,
			Result: capture.result, OriginalMatchCount: capture.originalMatchCount,
			Truncated: capture.truncated, FailureReason: capture.failureReason,
		})
		if err != nil {
			return fmt.Errorf("save trigger snapshot: %w", err)
		}
		for _, session := range capture.sessions {
			if err := queries.CreateTriggerSnapshotSession(ctx, alerting.CreateTriggerSnapshotSessionParams{
				SnapshotID: triggerSnapshotID, Pid: session.PID,
				Username: session.Username, DatabaseName: session.DatabaseName,
				ClientAddress: session.ClientAddress, State: session.State,
				QueryStartedAt: session.QueryStartedAt, TransactionStartedAt: session.TransactionStartedAt,
				QueryDurationMs: session.QueryDurationMS, TransactionDurationMs: session.TransactionDurationMS,
				WaitEventType: session.WaitEventType, WaitEvent: session.WaitEvent,
				BlockingPids: session.BlockingPIDs,
			}); err != nil {
				return fmt.Errorf("save trigger snapshot session: %w", err)
			}
		}
	}
	for _, kind := range alerting.StateEvents(previousState, nextSnapshot.State) {
		if kind == alerting.EventUpdated && !metricResult.currentValue.Valid {
			continue
		}
		eventTriggerSnapshotID := pgtype.UUID{}
		if kind == alerting.EventFired {
			eventTriggerSnapshotID = triggerSnapshotID
		}
		if err := queries.CreateAlertEvent(ctx, alerting.CreateAlertEventParams{
			AlertInstanceID:   alertInstanceID,
			RuleID:            evaluationTarget.RuleID,
			RuleVersion:       evaluationTarget.Version,
			Kind:              string(kind),
			FromState:         string(previousState),
			ToState:           string(nextSnapshot.State),
			CurrentValue:      metricResult.currentValue,
			Unavailability:    metricResult.unavailability,
			RuleSnapshot:      evaluationTarget.RuleSnapshot,
			EvaluatedAt:       evaluatedAt,
			TriggerSnapshotID: eventTriggerSnapshotID,
		}); err != nil {
			return fmt.Errorf("save alert event: %w", err)
		}
	}
	if err := queries.MarkAlertRuleEvaluated(ctx, evaluationCheckpoint); err != nil {
		return fmt.Errorf("mark alert rule evaluated: %w", err)
	}
	return nil
}

func snapshotFromEvaluationTarget(target alerting.GetEvaluationTargetRow) alerting.Snapshot {
	snapshot := alerting.Snapshot{State: alerting.OK}
	if !target.AlertInstanceID.Valid {
		return snapshot
	}

	snapshot.State = alerting.State(target.Status)
	snapshot.StateBeforeNoData = alerting.State(target.StateBeforeNoData.String)
	if target.EvaluatedRuleVersion == target.Version {
		snapshot.BreachCount = int(target.BreachCount)
		snapshot.RecoveryCount = int(target.RecoveryCount)
		snapshot.NoDataCount = int(target.NoDataCount)
	}
	return snapshot
}

func evaluateMetric(
	ctx context.Context,
	queries *alerting.Queries,
	target alerting.GetEvaluationTargetRow,
	metricDimensionKey string,
	now time.Time,
) (metricEvaluation, error) {
	result := metricEvaluation{
		outcome:        alerting.Missing,
		unavailability: pgtype.Text{String: "NO_SAMPLES_YET", Valid: true},
	}
	if strings.HasPrefix(target.MetricID, "pg.") && target.LastErrorCode.Valid {
		result.unavailability = target.LastErrorCode
		return result, nil
	}

	window := time.Duration(target.WindowSeconds) * time.Second
	samples, err := queries.SamplesInRuleWindow(ctx, alerting.SamplesInRuleWindowParams{
		InstanceID:         target.InstanceID,
		MetricID:           target.MetricID,
		MetricDimensionKey: metricDimensionKey,
		WindowStart:        pgtype.Timestamptz{Time: now.Add(-window), Valid: true},
		WindowEnd:          pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return metricEvaluation{}, fmt.Errorf("read rule samples: %w", err)
	}

	points := make([]alerting.Point, 0, len(samples))
	for _, sample := range samples {
		points = append(points, alerting.Point{
			Timestamp: sample.Ts.Time,
			Value:     sample.Value,
		})
	}
	value, ok := alerting.AggregateWindow(points, now, window, target.Aggregation)
	if !ok {
		return result, nil
	}

	result.currentValue = pgtype.Float8{Float64: value, Valid: true}
	result.unavailability = pgtype.Text{}
	result.outcome = alerting.Evaluate(
		value,
		target.Operator,
		target.Threshold,
		target.RecoveryOperator,
		target.RecoveryThreshold,
	)
	return result, nil
}

func structurallyNotApplicable(metricID metric.MetricID, lastErrorCode pgtype.Text, agentMetricsEnabled bool) bool {
	if lastErrorCode.Valid && lastErrorCode.String == "NOT_APPLICABLE_ROLE" {
		return true
	}
	if agentMetricsEnabled {
		return false
	}
	if metricID == metric.MetricAgentStatus {
		return true
	}
	for _, definition := range metric.Metrics {
		if definition.ID == metricID {
			return definition.Producer == metric.ProducerAgent
		}
	}
	return false
}

func (service *Service) Run(ctx context.Context, interval time.Duration) {
	ticks, stop := service.clock.Ticker(interval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticks:
			if err := service.RunOnce(ctx); err != nil {
				log.Printf("evaluation cycle failed: %v", err)
			}
		}
	}
}
