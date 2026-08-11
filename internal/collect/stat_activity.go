package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

const (
	longQuerySampleLimit = 100
	sessionSnapshotLimit = 500
	activityRetention    = 30 * 24 * time.Hour
)

type statActivitySession struct {
	PID                   int32      `json:"pid"`
	Username              *string    `json:"username"`
	DatabaseName          *string    `json:"database_name"`
	ClientAddress         *string    `json:"client_address"`
	State                 *string    `json:"state"`
	QueryStartedAt        *time.Time `json:"query_started_at"`
	TransactionStartedAt  *time.Time `json:"transaction_started_at"`
	QueryDurationMS       *int64     `json:"query_duration_ms"`
	TransactionDurationMS *int64     `json:"transaction_duration_ms"`
	WaitEventType         *string    `json:"wait_event_type"`
	WaitEvent             *string    `json:"wait_event"`
	BlockingPIDs          []int32    `json:"blocking_pids"`
}

type statActivitySnapshot struct {
	sessions             []statActivitySession
	sessionCount         int64
	sessionsTruncated    bool
	longQuerySamples     []statActivitySession
	longQuerySampleCount int64
	longQueriesTruncated bool
}

func decodeStatActivitySnapshot(
	sessionsJSON []byte,
	sessionCount int64,
	sessionsTruncated bool,
	longQueriesJSON []byte,
	longQueryCount int64,
	longQueriesTruncated bool,
) (statActivitySnapshot, error) {
	var sessions, longQueries []statActivitySession
	if err := json.Unmarshal(sessionsJSON, &sessions); err != nil {
		return statActivitySnapshot{}, fmt.Errorf("decode session snapshot: %w", err)
	}
	if err := json.Unmarshal(longQueriesJSON, &longQueries); err != nil {
		return statActivitySnapshot{}, fmt.Errorf("decode long query samples: %w", err)
	}
	if len(sessions) > sessionSnapshotLimit || len(longQueries) > longQuerySampleLimit {
		return statActivitySnapshot{}, errors.New("pg_stat_activity snapshot exceeds collection limits")
	}
	for _, sample := range longQueries {
		if sample.QueryStartedAt == nil || sample.QueryDurationMS == nil {
			return statActivitySnapshot{}, errors.New("long query sample is missing query timing")
		}
	}
	return statActivitySnapshot{
		sessions:             sessions,
		sessionCount:         sessionCount,
		sessionsTruncated:    sessionsTruncated,
		longQuerySamples:     longQueries,
		longQuerySampleCount: longQueryCount,
		longQueriesTruncated: longQueriesTruncated,
	}, nil
}

func persistStatActivitySnapshot(ctx context.Context, tx pgx.Tx, instanceID pgtype.UUID, observedAt time.Time, snapshot statActivitySnapshot) error {
	if _, err := tx.Exec(ctx, `INSERT INTO long_query_sample_snapshot
		(instance_id, sampled_at, original_count, truncated)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (instance_id, sampled_at) DO UPDATE SET
		original_count = EXCLUDED.original_count, truncated = EXCLUDED.truncated`,
		instanceID, observedAt, snapshot.longQuerySampleCount, snapshot.longQueriesTruncated); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM long_query_sample WHERE instance_id = $1 AND sampled_at = $2", instanceID, observedAt); err != nil {
		return err
	}
	longQueryRows := make([][]any, 0, len(snapshot.longQuerySamples))
	for _, sample := range snapshot.longQuerySamples {
		longQueryRows = append(longQueryRows, []any{
			instanceID, observedAt, sample.PID, sample.Username, sample.DatabaseName,
			sample.ClientAddress, sample.State, sample.QueryStartedAt, sample.TransactionStartedAt,
			sample.QueryDurationMS, sample.TransactionDurationMS, sample.WaitEventType,
			sample.WaitEvent, sample.BlockingPIDs,
		})
	}
	if len(longQueryRows) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"long_query_sample"}, []string{
			"instance_id", "sampled_at", "pid", "username", "database_name", "client_address", "state",
			"query_started_at", "transaction_started_at", "query_duration_ms", "transaction_duration_ms",
			"wait_event_type", "wait_event", "blocking_pids",
		}, pgx.CopyFromRows(longQueryRows)); err != nil {
			return err
		}
	}

	var accepted time.Time
	err := tx.QueryRow(ctx, `INSERT INTO instance_session_snapshot
		(instance_id, sampled_at, original_count, truncated)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (instance_id) DO UPDATE SET
		sampled_at = EXCLUDED.sampled_at,
		original_count = EXCLUDED.original_count,
		truncated = EXCLUDED.truncated
		WHERE instance_session_snapshot.sampled_at <= EXCLUDED.sampled_at
		RETURNING sampled_at`, instanceID, observedAt, snapshot.sessionCount, snapshot.sessionsTruncated).Scan(&accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "DELETE FROM instance_session_snapshot_entry WHERE instance_id = $1", instanceID); err != nil {
		return err
	}
	sessionRows := make([][]any, 0, len(snapshot.sessions))
	for _, session := range snapshot.sessions {
		sessionRows = append(sessionRows, []any{
			instanceID, session.PID, session.Username, session.DatabaseName, session.ClientAddress,
			session.State, session.QueryStartedAt, session.TransactionStartedAt, session.QueryDurationMS,
			session.TransactionDurationMS, session.WaitEventType, session.WaitEvent, session.BlockingPIDs,
		})
	}
	if len(sessionRows) > 0 {
		if _, err := tx.CopyFrom(ctx, pgx.Identifier{"instance_session_snapshot_entry"}, []string{
			"instance_id", "pid", "username", "database_name", "client_address", "state",
			"query_started_at", "transaction_started_at", "query_duration_ms", "transaction_duration_ms",
			"wait_event_type", "wait_event", "blocking_pids",
		}, pgx.CopyFromRows(sessionRows)); err != nil {
			return err
		}
	}
	return nil
}

func DropExpiredStatActivitySnapshots(ctx context.Context, database interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, now time.Time) error {
	_, err := database.Exec(ctx, "DELETE FROM long_query_sample_snapshot WHERE sampled_at < $1", now.UTC().Add(-activityRetention))
	return err
}
