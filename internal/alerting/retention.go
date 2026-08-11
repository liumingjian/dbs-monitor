package alerting

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

const AlertHistoryRetention = 90 * 24 * time.Hour

func DeleteRecoveredAlertHistory(ctx context.Context, database DBTX, now time.Time) (int64, error) {
	cutoff := now.UTC().Add(-AlertHistoryRetention)
	return New(database).DeleteRecoveredAlertHistoryBefore(ctx, pgtype.Timestamptz{Time: cutoff, Valid: true})
}
