package metric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func EnsurePartitions(ctx context.Context, db DBTX, now time.Time) error {
	start := dayUTC(now)
	for offset := 0; offset <= 7; offset++ {
		from := start.AddDate(0, 0, offset)
		to := from.AddDate(0, 0, 1)
		name := fmt.Sprintf("metric_sample_%s", from.Format("20060102"))
		statement := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF metric_sample FOR VALUES FROM ('%s') TO ('%s')",
			name, from.Format(time.RFC3339), to.Format(time.RFC3339),
		)
		if _, err := db.Exec(ctx, statement); err != nil {
			return fmt.Errorf("create partition %s: %w", name, err)
		}
	}
	return nil
}

func DropExpiredPartitions(ctx context.Context, db DBTX, now time.Time) error {
	cutoff := dayUTC(now).AddDate(0, 0, -31)
	rows, err := db.Query(ctx, `
		SELECT child.relname
		FROM pg_inherits
		JOIN pg_class parent ON parent.oid = inhparent
		JOIN pg_class child ON child.oid = inhrelid
		WHERE parent.relname = 'metric_sample'`)
	if err != nil {
		return fmt.Errorf("list metric partitions: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		date, err := time.Parse("20060102", strings.TrimPrefix(name, "metric_sample_"))
		if err != nil {
			continue
		}
		if date.Before(cutoff) {
			if _, err := db.Exec(ctx, "DROP TABLE "+name); err != nil {
				return fmt.Errorf("drop partition %s: %w", name, err)
			}
		}
	}
	return rows.Err()
}

func IsMissingPartition(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23514"
}

func dayUTC(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}
