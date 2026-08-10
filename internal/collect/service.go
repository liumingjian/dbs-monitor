package collect

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	pgxconn "github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/clock"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/instance"
	"github.com/liumingjian/dbs-monitor/internal/metric"
	monitorpg "github.com/liumingjian/dbs-monitor/internal/pgconn"
)

const (
	defaultProbeConcurrency = 32
	defaultQueryConcurrency = 32
)

var statActivityMetricIDs = [...]metric.MetricID{
	metric.MetricConnectionTotal,
	metric.MetricConnectionActive,
	metric.MetricConnectionIdleInTransaction,
	metric.MetricLongTransactionCount,
	metric.MetricMaxTransactionDurationSec,
	metric.MetricLockWaitingCount,
	metric.MetricBlockedSessionCount,
	metric.MetricLongRunningQueryCount,
}

type Collector interface {
	RunOnce(context.Context) error
}

type Config struct {
	ProbeConcurrency int
	QueryConcurrency int
}

func DefaultConfig() Config {
	return Config{ProbeConcurrency: defaultProbeConcurrency, QueryConcurrency: defaultQueryConcurrency}
}

func (config Config) Validate() error {
	if config.ProbeConcurrency < 1 || config.ProbeConcurrency > 50 {
		return errors.New("probe concurrency must be between 1 and 50")
	}
	if config.QueryConcurrency < 1 || config.QueryConcurrency > 50 {
		return errors.New("query concurrency must be between 1 and 50")
	}
	return nil
}

type cachedConnection struct {
	credentialVersion instance.CredentialVersion
	conn              *monitorpg.TargetConn
}

type Service struct {
	platform *db.Pool
	dialer   monitorpg.Dialer
	clock    clock.Clock
	config   Config
	keyring  *instance.CredentialKeyring

	queryConnectionMu       sync.Mutex
	queryConnections        map[string]cachedConnection
	queryConnectionRebuilds int64
}

func New(platform *db.Pool, dialer monitorpg.Dialer, currentClock clock.Clock, keyring *instance.CredentialKeyring) *Service {
	service, err := NewWithConfig(platform, dialer, currentClock, keyring, DefaultConfig())
	if err != nil {
		panic(err)
	}
	return service
}

func NewWithConfig(platform *db.Pool, dialer monitorpg.Dialer, currentClock clock.Clock, keyring *instance.CredentialKeyring, config Config) (*Service, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &Service{
		platform:         platform,
		dialer:           dialer,
		clock:            currentClock,
		config:           config,
		keyring:          keyring,
		queryConnections: map[string]cachedConnection{},
	}, nil
}

func (service *Service) RunOnce(ctx context.Context) error {
	targets, err := instance.New(service.platform).ListCollectionTargets(ctx)
	if err != nil {
		return fmt.Errorf("list collection targets: %w", err)
	}
	now := service.clock.Now().UTC()
	for _, target := range targets {
		if err := service.ensureTaskStates(ctx, target.ID); err != nil {
			return err
		}
		intervals, err := service.taskIntervals(ctx, target.ID)
		if err != nil {
			return err
		}
		for _, task := range scheduledTasks() {
			run := newScheduledRun(target, task, intervals[task.ID], now)
			eligible, err := service.nextEligible(ctx, run)
			if err != nil {
				return err
			}
			if !eligible.IsZero() && now.Before(eligible) {
				if err := service.recordUnmet(ctx, run, resultBackoff, eligible); err != nil {
					return err
				}
				continue
			}
			outcome := service.executeTask(ctx, run)
			if outcome.err != nil {
				return outcome.err
			}
		}
	}
	return nil
}

func (service *Service) executeTask(ctx context.Context, run scheduledRun) executionOutcome {
	run.startedAt = service.clock.Now().UTC()
	startedWall := time.Now()
	outcome := executionOutcome{run: run, result: resultFailed}
	if err := service.recordStarted(ctx, run); err != nil {
		outcome.err = err
		return outcome
	}

	if run.task.Kind == metric.TaskKindProbe {
		taskCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		config, err := service.connectionConfig(run.target)
		if err != nil {
			outcome.err = fmt.Errorf("read instance credential: %w", err)
			return outcome
		}
		conn, err := service.dialer.Dial(taskCtx, config)
		if err == nil {
			var one int
			err = conn.QueryRow(taskCtx, run.task.SQL).Scan(&one)
			closeConnection(conn)
		}
		if err != nil {
			outcome.result, outcome.err = service.finishFailure(ctx, run, taskCtx, true)
			outcome.duration = time.Since(startedWall)
			return outcome
		}
		latency := float64(time.Since(startedWall).Microseconds()) / 1000
		outcome.err = service.recordSuccess(ctx, run, []collectedSample{
			{metricID: metric.MetricAvailabilityReachable, value: metric.NonNumericMetricEncodings[metric.MetricAvailabilityReachable.String()]["reachable"]},
			{metricID: metric.MetricProbeLatencyMS, value: latency},
		})
		outcome.result = resultSuccess
		outcome.duration = time.Since(startedWall)
		return outcome
	}

	timeout := taskTimeout(run.interval)
	taskCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := service.queryConnection(taskCtx, run.target)
	dialFailure := err != nil
	if err == nil {
		var configured string
		err = conn.QueryRow(taskCtx, "SELECT set_config('statement_timeout', $1, false)",
			strconv.FormatInt(timeout.Milliseconds(), 10)+"ms").Scan(&configured)
	}
	values := make([]float64, len(statActivityMetricIDs))
	if err == nil {
		err = conn.QueryRow(taskCtx, run.task.SQL).Scan(
			&values[0], &values[1], &values[2], &values[3],
			&values[4], &values[5], &values[6], &values[7],
		)
	}
	if err != nil {
		connectionFailure := dialFailure || isConnectionFailure(err)
		if connectionFailure || errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
			service.invalidateQueryConnection(run.target.ID)
		}
		outcome.result, outcome.err = service.finishFailure(ctx, run, taskCtx, connectionFailure)
		outcome.duration = time.Since(startedWall)
		return outcome
	}
	samples := make([]collectedSample, len(statActivityMetricIDs))
	for index, metricID := range statActivityMetricIDs {
		samples[index] = collectedSample{metricID: metricID, value: values[index]}
	}
	outcome.err = service.recordSuccess(ctx, run, samples)
	outcome.result = resultSuccess
	outcome.duration = time.Since(startedWall)
	return outcome
}

func (service *Service) finishFailure(ctx context.Context, run scheduledRun, taskCtx context.Context, connectionFailure bool) (taskResult, error) {
	result := resultFailed
	code := errorCodeQueryFailed
	if connectionFailure {
		code = errorCodeConnectionFailed
	}
	if errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
		result = resultTimedOut
		code = errorCodeTimeout
	}
	return result, service.recordFailure(ctx, run, result, code, connectionFailure)
}

func (service *Service) queryConnection(ctx context.Context, target instance.ListCollectionTargetsRow) (*monitorpg.TargetConn, error) {
	key := uuid.UUID(target.ID.Bytes).String()
	credentialVersion := instance.CredentialVersion(target.CredentialVersion)
	service.queryConnectionMu.Lock()
	cached, exists := service.queryConnections[key]
	if exists && cached.credentialVersion == credentialVersion && !cached.conn.IsClosed() {
		service.queryConnectionMu.Unlock()
		return cached.conn, nil
	}
	if exists {
		delete(service.queryConnections, key)
		service.queryConnectionRebuilds++
	}
	service.queryConnectionMu.Unlock()
	if exists {
		closeConnection(cached.conn)
	}
	config, err := service.connectionConfig(target)
	if err != nil {
		return nil, fmt.Errorf("read instance credential: %w", err)
	}
	conn, err := service.dialer.Dial(ctx, config)
	if err != nil {
		return nil, err
	}
	service.queryConnectionMu.Lock()
	service.queryConnections[key] = cachedConnection{credentialVersion: credentialVersion, conn: conn}
	service.queryConnectionMu.Unlock()
	return conn, nil
}

func (service *Service) invalidateQueryConnection(targetID pgtype.UUID) {
	key := uuid.UUID(targetID.Bytes).String()
	service.queryConnectionMu.Lock()
	cached, exists := service.queryConnections[key]
	if exists {
		delete(service.queryConnections, key)
		service.queryConnectionRebuilds++
	}
	service.queryConnectionMu.Unlock()
	if exists {
		closeConnection(cached.conn)
	}
}

func (service *Service) queryConnectionSummary(active int) (idle int, rebuilds int64) {
	service.queryConnectionMu.Lock()
	defer service.queryConnectionMu.Unlock()
	idle = len(service.queryConnections) - active
	if idle < 0 {
		idle = 0
	}
	rebuilds = service.queryConnectionRebuilds
	service.queryConnectionRebuilds = 0
	return idle, rebuilds
}

func (service *Service) closeQueryConnections() {
	service.queryConnectionMu.Lock()
	connections := make([]cachedConnection, 0, len(service.queryConnections))
	for key, cached := range service.queryConnections {
		connections = append(connections, cached)
		delete(service.queryConnections, key)
	}
	service.queryConnectionMu.Unlock()
	for _, cached := range connections {
		closeConnection(cached.conn)
	}
}

func closeConnection(conn *monitorpg.TargetConn) {
	if conn == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = conn.Close(ctx)
}

func (service *Service) taskIntervals(ctx context.Context, targetID pgtype.UUID) (map[metric.TaskID]time.Duration, error) {
	rows, err := metric.New(service.platform).ListTaskIntervals(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("list collection task intervals: %w", err)
	}
	intervals := make(map[metric.TaskID]time.Duration, len(rows))
	for _, row := range rows {
		intervals[metric.TaskID(row.TaskID)] = time.Duration(row.IntervalSeconds) * time.Second
	}
	return intervals, nil
}

func scheduledTasks() []metric.Task {
	tasks := make([]metric.Task, 0, 2)
	for _, task := range metric.Tasks {
		switch task.ID {
		case metric.TaskProbe, metric.TaskStatActivity:
			tasks = append(tasks, task)
		}
	}
	return tasks
}

func taskTimeout(interval time.Duration) time.Duration {
	timeout := interval * 4 / 5
	if timeout > 10*time.Second {
		return 10 * time.Second
	}
	return timeout
}

func newScheduledRun(target instance.ListCollectionTargetsRow, task metric.Task, configured time.Duration, dueAt time.Time) scheduledRun {
	interval := task.Interval
	if configured > 0 {
		interval = configured
	}
	instanceID := uuid.UUID(target.ID.Bytes).String()
	return scheduledRun{
		key:      taskKey{instanceID: instanceID, taskID: task.ID},
		dueAt:    dueAt.UTC(),
		target:   target,
		task:     task,
		interval: interval,
	}
}

func (service *Service) withPartitionRepair(ctx context.Context, observedAt time.Time, write func() error) error {
	if err := write(); err != nil {
		if !metric.IsMissingPartition(err) {
			return err
		}
		if err := metric.EnsurePartitions(ctx, service.platform, observedAt); err != nil {
			return err
		}
		return write()
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

func (service *Service) connectionConfig(target instance.ListCollectionTargetsRow) (*pgx.ConnConfig, error) {
	password, err := service.keyring.DecryptPassword(uuid.UUID(target.ID.Bytes), target.PasswordCiphertext, target.PasswordKeyVersion)
	if err != nil {
		return nil, err
	}
	return targetConnectionConfig(target, password)
}

func targetConnectionConfig(target instance.ListCollectionTargetsRow, password string) (*pgx.ConnConfig, error) {
	config, err := pgx.ParseConfig("postgres://localhost/?sslmode=disable")
	if err != nil {
		return nil, err
	}
	config.Host = target.Host
	config.Port = uint16(target.Port)
	config.Database = target.DatabaseName
	config.User = target.Username
	config.Password = password
	config.RuntimeParams["application_name"] = "dbs-monitor"
	return config, nil
}

func (service *Service) Run(ctx context.Context, interval time.Duration) {
	defer service.closeQueryConnections()
	scheduler := newCentralScheduler(service)
	if err := scheduler.refresh(ctx, service.clock.Now().UTC()); err != nil {
		log.Printf("collection scheduler refresh failed: %v", err)
	}
	ticks, stop := service.clock.Ticker(interval)
	defer stop()
	for {
		select {
		case <-ctx.Done():
			return
		case outcome := <-scheduler.completed:
			scheduler.complete(outcome)
			scheduler.dispatch(ctx)
		case <-ticks:
			now := service.clock.Now().UTC()
			if err := scheduler.refresh(ctx, now); err != nil {
				log.Printf("collection scheduler refresh failed: %v", err)
				continue
			}
			scheduler.accrue(ctx, now)
			scheduler.dispatch(ctx)
			scheduler.logSummary(now)
		}
	}
}
