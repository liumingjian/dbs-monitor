package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	pgxconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

type Collector interface {
	RunOnce(context.Context) error
}

type Service struct {
	platform *db.Pool
	dialer   monitorpg.Dialer
	clock    clock.Clock
}

func New(platform *db.Pool, dialer monitorpg.Dialer, currentClock clock.Clock) *Service {
	return &Service{platform: platform, dialer: dialer, clock: currentClock}
}

func (service *Service) RunOnce(ctx context.Context) error {
	targets, err := instance.New(service.platform).ListCollectionTargets(ctx)
	if err != nil {
		return fmt.Errorf("list collection targets: %w", err)
	}
	for _, target := range targets {
		if err := service.collectTarget(ctx, target); err != nil {
			return err
		}
	}
	return nil
}

func (service *Service) collectTarget(ctx context.Context, target instance.ListCollectionTargetsRow) error {
	now := service.clock.Now().UTC()
	connectionString := targetConnectionString(target)
	conn, err := service.dialer.Dial(ctx, connectionString)
	if err != nil {
		writeFailure := func() error {
			return service.platform.InTx(ctx, func(tx pgx.Tx) error {
				queries := metric.New(tx)
				seriesID, writeErr := queries.UpsertSeries(ctx, metric.UpsertSeriesParams{
					InstanceID: target.ID,
					MetricID:   "pg.availability.reachable",
					Labels:     json.RawMessage(`{}`),
					LabelsKey:  "{}",
					LastSeen:   pgtype.Timestamptz{Time: now, Valid: true},
				})
				if writeErr != nil {
					return writeErr
				}
				if _, writeErr = tx.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, 0)", seriesID, now); writeErr != nil {
					return writeErr
				}
				return instance.New(tx).SetCollectFailure(ctx, instance.SetCollectFailureParams{
					InstanceID:       target.ID,
					LastErrorCode:    pgtype.Text{String: "DB_UNREACHABLE", Valid: true},
					LastErrorMessage: pgtype.Text{String: err.Error(), Valid: true},
				})
			})
		}
		if writeErr := writeFailure(); writeErr != nil {
			if !metric.IsMissingPartition(writeErr) {
				return fmt.Errorf("write collection failure: %w", writeErr)
			}
			if writeErr := metric.EnsurePartitions(ctx, service.platform, now); writeErr != nil {
				return writeErr
			}
			if writeErr := writeFailure(); writeErr != nil {
				return fmt.Errorf("retry collection failure: %w", writeErr)
			}
		}
		return nil
	}
	defer conn.Close(ctx)

	var connectionTotal float64
	if err := conn.QueryRow(ctx, "SELECT count(*)::double precision FROM pg_stat_activity").Scan(&connectionTotal); err != nil {
		if !isConnectionFailure(err) {
			return fmt.Errorf("collect pg.connection.total: %w", err)
		}
		return service.platform.InTx(ctx, func(tx pgx.Tx) error {
			return instance.New(tx).SetCollectFailure(ctx, instance.SetCollectFailureParams{
				InstanceID:       target.ID,
				LastErrorCode:    pgtype.Text{String: "DB_UNREACHABLE", Valid: true},
				LastErrorMessage: pgtype.Text{String: err.Error(), Valid: true},
			})
		})
	}

	write := func() error {
		return service.platform.InTx(ctx, func(tx pgx.Tx) error {
			queries := metric.New(tx)
			for _, sample := range []struct {
				metricID string
				value    float64
			}{
				{"pg.availability.reachable", 1},
				{"pg.connection.total", connectionTotal},
			} {
				seriesID, err := queries.UpsertSeries(ctx, metric.UpsertSeriesParams{
					InstanceID: target.ID,
					MetricID:   sample.metricID,
					Labels:     json.RawMessage(`{}`),
					LabelsKey:  "{}",
					LastSeen:   pgtype.Timestamptz{Time: now, Valid: true},
				})
				if err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, "INSERT INTO metric_sample (series_id, ts, value) VALUES ($1, $2, $3)", seriesID, now, sample.value); err != nil {
					return err
				}
			}
			return instance.New(tx).SetCollectSuccess(ctx, instance.SetCollectSuccessParams{
				InstanceID:    target.ID,
				LastSuccessAt: pgtype.Timestamptz{Time: now, Valid: true},
			})
		})
	}

	if err := write(); err != nil {
		if !metric.IsMissingPartition(err) {
			return fmt.Errorf("write collected samples: %w", err)
		}
		if err := metric.EnsurePartitions(ctx, service.platform, now); err != nil {
			return err
		}
		if err := write(); err != nil {
			return fmt.Errorf("retry collected samples: %w", err)
		}
	}
	return nil
}

func isConnectionFailure(err error) bool {
	var pgError *pgxconn.PgError
	if errors.As(err, &pgError) {
		return strings.HasPrefix(pgError.Code, "08") || pgError.Code == "57P01" || pgError.Code == "57P02" || pgError.Code == "57P03"
	}
	var networkError net.Error
	return errors.As(err, &networkError) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

func targetConnectionString(target instance.ListCollectionTargetsRow) string {
	connection := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(target.Username, target.Password),
		Host:   net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))),
		Path:   "/" + target.DatabaseName,
	}
	query := connection.Query()
	query.Set("sslmode", "disable")
	connection.RawQuery = query.Encode()
	return connection.String()
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
				log.Printf("collection cycle failed: %v", err)
			}
		}
	}
}
