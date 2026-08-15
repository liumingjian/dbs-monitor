package alerting

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

func DeleteRecoveredAlertHistory(ctx context.Context, database DBTX, now time.Time, retention time.Duration) (int64, error) {
	cutoff := now.UTC().Add(-retention)
	return New(database).DeleteRecoveredAlertHistoryBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
}
