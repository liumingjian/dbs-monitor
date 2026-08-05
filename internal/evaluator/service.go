package evaluator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/alerting"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
)

const (
	triggerThreshold  = 20.0
	triggerCount      = 2
	recoveryThreshold = 15.0
	recoveryCount     = 2
)

type Service struct {
	platform *db.Pool
	clock    clock.Clock
}

func New(platform *db.Pool, currentClock clock.Clock) *Service {
	return &Service{platform: platform, clock: currentClock}
}

func (service *Service) RunOnce(ctx context.Context) error {
	targetIDs, err := alerting.New(service.platform).ListEvaluationTargetIDs(ctx)
	if err != nil {
		return fmt.Errorf("list evaluation targets: %w", err)
	}
	for _, targetID := range targetIDs {
		if err := service.evaluateTarget(ctx, targetID); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) evaluateTarget(ctx context.Context, targetID pgtype.UUID) error {
	now := service.clock.Now().UTC()
	return service.platform.InTx(ctx, func(tx pgx.Tx) error {
		queries := alerting.New(tx)
		target, err := queries.GetEvaluationTarget(ctx, targetID)
		if err != nil {
			return fmt.Errorf("read evaluation target: %w", err)
		}
		current := alerting.Snapshot{State: alerting.OK}
		if target.Status.Valid {
			current.State = alerting.State(target.Status.String)
			current.BreachCount = int(target.BreachCount.Int32)
			current.RecoveryCount = int(target.RecoveryCount.Int32)
			current.NoDataCount = int(target.NoDataCount.Int32)
			current.StateBeforeNoData = alerting.State(target.StateBeforeNoData.String)
		}

		evaluation := alerting.Missing
		unavailability := pgtype.Text{}
		if target.LastErrorCode.Valid {
			unavailability = target.LastErrorCode
		} else {
			point, err := queries.LatestConnectionPoint(ctx, alerting.LatestConnectionPointParams{
				InstanceID: target.ID,
				Ts:         pgtype.Timestamptz{Time: now.Add(-5 * time.Second), Valid: true},
			})
			if err == nil {
				if point.Value >= triggerThreshold {
					evaluation = alerting.Breaching
				} else if point.Value < recoveryThreshold {
					evaluation = alerting.Recovering
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("read latest sample: %w", err)
			}
		}

		next := alerting.Step(current, evaluation, triggerCount, recoveryCount)
		stateBeforeNoData := pgtype.Text{}
		if next.StateBeforeNoData != "" {
			stateBeforeNoData = pgtype.Text{String: string(next.StateBeforeNoData), Valid: true}
		}
		if next.State != alerting.NO_DATA {
			unavailability = pgtype.Text{}
		}
		return queries.SaveAlertSnapshot(ctx, alerting.SaveAlertSnapshotParams{
			InstanceID:        target.ID,
			Status:            string(next.State),
			BreachCount:       int32(next.BreachCount),
			RecoveryCount:     int32(next.RecoveryCount),
			NoDataCount:       int32(next.NoDataCount),
			StateBeforeNoData: stateBeforeNoData,
			Unavailability:    unavailability,
			UpdatedAt:         pgtype.Timestamptz{Time: now, Valid: true},
		})
	})
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
