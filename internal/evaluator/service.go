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
)

type Service struct {
	platform *db.Pool
	clock    clock.Clock
}

func New(platform *db.Pool, currentClock clock.Clock) *Service {
	return &Service{platform: platform, clock: currentClock}
}

func (service *Service) RunOnce(ctx context.Context) error {
	targets, err := alerting.New(service.platform).ListEvaluationTargets(ctx)
	if err != nil {
		return fmt.Errorf("list evaluation targets: %w", err)
	}
	for start := 0; start < len(targets); {
		end := start + 1
		for end < len(targets) && targets[end].InstanceID == targets[start].InstanceID {
			end++
		}
		if err := service.evaluateInstance(ctx, targets[start:end]); err != nil {
			return err
		}
		start = end
	}
	return nil
}

func (service *Service) evaluateInstance(ctx context.Context, targets []alerting.ListEvaluationTargetsRow) error {
	now := service.clock.Now().UTC()
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		for _, target := range targets {
			if err := service.evaluateRule(ctx, queries, target.RuleID, target.InstanceID, now); err != nil {
				return err
			}
		}
		return nil
	})
}

func (service *Service) evaluateRule(ctx context.Context, queries *alerting.Queries, ruleID, instanceID pgtype.UUID, now time.Time) error {
	target, err := queries.GetEvaluationTarget(ctx, alerting.GetEvaluationTargetParams{
		RuleID:     ruleID,
		InstanceID: instanceID,
	})
	if err != nil {
		return fmt.Errorf("read evaluation target: %w", err)
	}

	current := alerting.Snapshot{State: alerting.OK}
	if target.Status.Valid {
		current.State = alerting.State(target.Status.String)
		if target.EvaluatedRuleVersion.Int32 == target.Version {
			current.BreachCount = int(target.BreachCount.Int32)
			current.RecoveryCount = int(target.RecoveryCount.Int32)
			current.NoDataCount = int(target.NoDataCount.Int32)
		}
		current.StateBeforeNoData = alerting.State(target.StateBeforeNoData.String)
	}
	previousState := current.State

	evaluation := alerting.Missing
	unavailability := pgtype.Text{String: "NO_SAMPLES_YET", Valid: true}
	currentValue := pgtype.Float8{}
	if strings.HasPrefix(target.MetricID, "pg.") && target.LastErrorCode.Valid {
		unavailability = target.LastErrorCode
	} else {
		window := time.Duration(target.WindowSeconds) * time.Second
		samples, err := queries.SamplesInRuleWindow(ctx, alerting.SamplesInRuleWindowParams{
			InstanceID:  target.InstanceID,
			MetricID:    target.MetricID,
			WindowStart: pgtype.Timestamptz{Time: now.Add(-window), Valid: true},
			WindowEnd:   pgtype.Timestamptz{Time: now, Valid: true},
		})
		if err != nil {
			return fmt.Errorf("read rule samples: %w", err)
		}
		points := make([]alerting.Point, 0, len(samples))
		for _, sample := range samples {
			points = append(points, alerting.Point{
				Timestamp: sample.Ts.Time,
				Value:     sample.Value,
			})
		}
		if value, ok := alerting.AggregateWindow(points, now, window, target.Aggregation); ok {
			currentValue = pgtype.Float8{Float64: value, Valid: true}
			unavailability = pgtype.Text{}
			evaluation = alerting.Evaluate(
				value,
				target.Operator,
				target.Threshold,
				target.RecoveryOperator,
				target.RecoveryThreshold,
			)
		}
	}
	if evaluation == alerting.Missing && target.NoDataPolicy == "ignore" {
		return nil
	}

	next := alerting.Step(current, evaluation, int(target.ConsecutiveCount), int(target.RecoveryConsecutiveCount))
	stateBeforeNoData := pgtype.Text{}
	if next.StateBeforeNoData != "" {
		stateBeforeNoData = pgtype.Text{String: string(next.StateBeforeNoData), Valid: true}
	}
	if next.State != alerting.NO_DATA {
		unavailability = pgtype.Text{}
	}
	alertInstanceID, err := queries.SaveAlertSnapshot(ctx, alerting.SaveAlertSnapshotParams{
		RuleID:            target.RuleID,
		InstanceID:        target.InstanceID,
		MetricID:          target.MetricID,
		Status:            string(next.State),
		RuleVersion:       target.Version,
		Severity:          target.Severity,
		CurrentValue:      currentValue,
		RuleSnapshot:      target.RuleSnapshot,
		BreachCount:       int32(next.BreachCount),
		RecoveryCount:     int32(next.RecoveryCount),
		NoDataCount:       int32(next.NoDataCount),
		StateBeforeNoData: stateBeforeNoData,
		Unavailability:    unavailability,
		UpdatedAt:         pgtype.Timestamptz{Time: now, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("save alert state: %w", err)
	}
	for _, kind := range alerting.StateEvents(previousState, next.State) {
		if kind == alerting.EventUpdated && !currentValue.Valid {
			continue
		}
		if err := queries.CreateAlertEvent(ctx, alerting.CreateAlertEventParams{
			AlertInstanceID: alertInstanceID,
			RuleID:          target.RuleID,
			RuleVersion:     target.Version,
			Kind:            string(kind),
			FromState:       string(previousState),
			ToState:         string(next.State),
			CurrentValue:    currentValue,
			Unavailability:  unavailability,
			RuleSnapshot:    target.RuleSnapshot,
			EvaluatedAt:     pgtype.Timestamptz{Time: now, Valid: true},
		}); err != nil {
			return fmt.Errorf("save alert event: %w", err)
		}
	}
	return nil
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
