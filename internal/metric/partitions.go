package metric

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	DefaultPartitionSpan = 24 * time.Hour
	partitionPrebuild    = 7
	partitionRetention   = 30
)

func EnsurePartitions(ctx context.Context, db DBTX, now time.Time) error {
	return EnsurePartitionsWithSpan(ctx, db, now, DefaultPartitionSpan)
}

func EnsurePartitionsWithSpan(ctx context.Context, db DBTX, now time.Time, span time.Duration) error {
	return EnsurePartitionRange(ctx, db, now, now, span)
}

func EnsurePartitionRange(ctx context.Context, db DBTX, from, through time.Time, span time.Duration) error {
	if span < time.Second {
		return errors.New("partition span must be at least one second")
	}
	start := partitionBoundary(from, span)
	end := partitionBoundary(through, span).Add(partitionPrebuild * span)
	for boundary := start; !boundary.After(end); boundary = boundary.Add(span) {
		to := boundary.Add(span)
		name := partitionName(boundary, span)
		statement := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF metric_sample FOR VALUES FROM ('%s') TO ('%s')",
			name, boundary.Format(time.RFC3339), to.Format(time.RFC3339),
		)
		if _, err := db.Exec(ctx, statement); err != nil {
			return fmt.Errorf("create partition %s: %w", name, err)
		}
	}
	return nil
}

func DropExpiredPartitions(ctx context.Context, db DBTX, now time.Time) error {
	return DropExpiredPartitionsWithSpan(ctx, db, now, DefaultPartitionSpan)
}

func DropExpiredPartitionsWithSpan(ctx context.Context, db DBTX, now time.Time, span time.Duration) error {
	if span < time.Second {
		return errors.New("partition span must be at least one second")
	}
	cutoff := partitionBoundary(now, span).Add(-partitionRetention * span)
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
		date, err := partitionTime(name)
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

func partitionBoundary(value time.Time, span time.Duration) time.Time {
	return time.Unix(0, value.UnixNano()/span.Nanoseconds()*span.Nanoseconds()).UTC()
}

func partitionName(from time.Time, span time.Duration) string {
	format := "20060102_150405"
	if span == DefaultPartitionSpan {
		format = "20060102"
	}
	return "metric_sample_" + from.UTC().Format(format)
}

func partitionTime(name string) (time.Time, error) {
	value := strings.TrimPrefix(name, "metric_sample_")
	for _, format := range []string{"20060102", "20060102_150405"} {
		if parsed, err := time.Parse(format, value); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized metric partition %q", name)
}

func IsMissingPartition(err error) bool {
	var pgError *pgconn.PgError
	return errors.As(err, &pgError) && pgError.Code == "23514"
}
