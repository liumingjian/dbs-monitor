package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/liumingjian/dbs-monitor/internal/db"
	"github.com/liumingjian/dbs-monitor/internal/platformdb"
	"github.com/liumingjian/dbs-monitor/internal/platformhealth"
	"github.com/liumingjian/dbs-monitor/migrations"
)

type switchableHandler struct {
	current atomic.Pointer[handlerSlot]
}

// handlerSlot lets atomic.Pointer store handlers with different concrete types.
type handlerSlot struct {
	handler http.Handler
}

func newSwitchableHandler(initial http.Handler) *switchableHandler {
	handler := &switchableHandler{}
	handler.Switch(initial)
	return handler
}

func (handler *switchableHandler) Switch(next http.Handler) {
	handler.current.Store(&handlerSlot{handler: next})
}

func (handler *switchableHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	handler.current.Load().handler.ServeHTTP(writer, request)
}

func preparePlatformDatabase(
	ctx context.Context,
	platform *db.Pool,
	connectionString string,
	credentialDirectory string,
	lockWaitTimeout time.Duration,
	recovering bool,
	health *platformhealth.Store,
	logger *log.Logger,
) (bool, error) {
	report, err := platformdb.Check(ctx, platform)
	if err != nil {
		health.Update(time.Now().UTC(), platformhealth.DatabaseSource(err))
		return false, nil
	}
	preflightReady, err := handlePlatformDatabasePreflight(
		report, recovering, health, logger, time.Now().UTC(),
	)
	if err != nil || !preflightReady {
		return false, err
	}

	migrationDB, err := migrations.Open(connectionString)
	if err != nil {
		return false, err
	}
	_, migrationErr := migrations.UpWithLockTimeout(ctx, migrationDB, credentialDirectory, lockWaitTimeout)
	closeErr := migrationDB.Close()
	if migrationErr == nil && closeErr != nil {
		migrationErr = fmt.Errorf("close migration database: %w", closeErr)
	}
	if migrationErr == nil {
		reportPlatformDatabasePreflight(report, health, logger, time.Now().UTC())
		return true, nil
	}

	pingContext, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	defer cancelPing()
	if pingErr := platform.Ping(pingContext); pingErr != nil {
		health.Update(time.Now().UTC(), platformhealth.DatabaseSource(pingErr))
		return false, nil
	}
	return false, fmt.Errorf("platform database migration failed; refusing to start: %w", migrationErr)
}

// Combining the database OID with fixed lock IDs keeps locks independent across
// databases on shared PostgreSQL clusters.
const (
	serverProcessLockID        int64 = 0x50524f43 // "PROC"
	databaseAdvisoryTryLockSQL       = `SELECT pg_try_advisory_lock(
		((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
	)`
	databaseAdvisoryUnlockSQL = `SELECT pg_advisory_unlock(
		((SELECT oid::bigint FROM pg_database WHERE datname = current_database()) << 32) | $1
	)`
)

var errServerProcessLockUnavailable = errors.New("another monitor-server instance is already running")

type databaseAdvisoryLock struct {
	connection *pgxpool.Conn
	lockID     int64
	name       string
}

func acquireServerProcessLock(ctx context.Context, pool *pgxpool.Pool) (*databaseAdvisoryLock, error) {
	return acquireDatabaseAdvisoryLock(ctx, pool, serverProcessLockID, "server process", errServerProcessLockUnavailable)
}

func acquireDatabaseAdvisoryLock(
	ctx context.Context,
	pool *pgxpool.Pool,
	lockID int64,
	name string,
	unavailableError error,
) (*databaseAdvisoryLock, error) {
	connection, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("reserve %s lock connection: %w", name, err)
	}
	var acquired bool
	if err := connection.QueryRow(ctx, databaseAdvisoryTryLockSQL, lockID).Scan(&acquired); err != nil {
		connection.Release()
		return nil, fmt.Errorf("acquire %s advisory lock: %w", name, err)
	}
	if !acquired {
		connection.Release()
		return nil, unavailableError
	}
	return &databaseAdvisoryLock{connection: connection, lockID: lockID, name: name}, nil
}

func (lock *databaseAdvisoryLock) Release() error {
	if lock == nil || lock.connection == nil {
		return nil
	}
	connection := lock.connection
	lock.connection = nil
	defer connection.Release()

	ctx := context.Background()
	var unlocked bool
	if err := connection.QueryRow(ctx, databaseAdvisoryUnlockSQL, lock.lockID).Scan(&unlocked); err != nil {
		_ = connection.Conn().Close(ctx)
		return fmt.Errorf("release %s advisory lock: %w", lock.name, err)
	}
	if !unlocked {
		return fmt.Errorf("release %s advisory lock: lock was not held", lock.name)
	}
	return nil
}
