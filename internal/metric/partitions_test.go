package metric

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPartitionSpanUsesMinuteBoundariesAndNames(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 34, 56, 0, time.UTC)
	boundary := partitionBoundary(now, time.Minute)
	if want := time.Date(2026, time.August, 15, 12, 34, 0, 0, time.UTC); boundary != want {
		t.Fatalf("minute boundary = %s, want %s", boundary, want)
	}
	if got, want := partitionName(boundary, time.Minute), "metric_sample_20260815_123400"; got != want {
		t.Fatalf("minute partition = %q, want %q", got, want)
	}
	parsed, err := partitionTime("metric_sample_20260815_123400")
	if err != nil || parsed != boundary {
		t.Fatalf("parse minute partition = %s, %v; want %s", parsed, err, boundary)
	}
}

func TestDefaultPartitionNamesRemainDaily(t *testing.T) {
	now := time.Date(2026, time.August, 15, 12, 34, 56, 0, time.UTC)
	boundary := partitionBoundary(now, DefaultPartitionSpan)
	if got, want := partitionName(boundary, DefaultPartitionSpan), "metric_sample_20260815"; got != want {
		t.Fatalf("daily partition = %q, want %q", got, want)
	}
}

func TestEnsurePartitionsWithSpanPrebuildsCurrentPlusSeven(t *testing.T) {
	database := &partitionRecordingDB{}
	now := time.Date(2026, time.August, 15, 12, 34, 56, 0, time.UTC)
	if err := EnsurePartitionsWithSpan(context.Background(), database, now, time.Minute); err != nil {
		t.Fatalf("ensure minute partitions: %v", err)
	}
	if len(database.statements) != 8 {
		t.Fatalf("created partitions = %d, want current + 7", len(database.statements))
	}
	for _, expected := range []string{"metric_sample_20260815_123400", "metric_sample_20260815_124100"} {
		found := false
		for _, statement := range database.statements {
			found = found || strings.Contains(statement, expected)
		}
		if !found {
			t.Errorf("partition %s was not created: %v", expected, database.statements)
		}
	}
}

func TestEnsurePartitionRangeCoversBackfillThroughCurrentPrebuild(t *testing.T) {
	database := &partitionRecordingDB{}
	through := time.Date(2026, time.August, 15, 12, 34, 56, 0, time.UTC)
	if err := EnsurePartitionRange(context.Background(), database, through.Add(-5*time.Minute), through, time.Minute); err != nil {
		t.Fatalf("ensure backfill partition range: %v", err)
	}
	if len(database.statements) != 13 {
		t.Fatalf("backfill partitions = %d, want 5 prior + current + 7 future", len(database.statements))
	}
}

type partitionRecordingDB struct{ statements []string }

func (database *partitionRecordingDB) Exec(_ context.Context, statement string, _ ...interface{}) (pgconn.CommandTag, error) {
	database.statements = append(database.statements, statement)
	return pgconn.CommandTag{}, nil
}

func (*partitionRecordingDB) Query(context.Context, string, ...interface{}) (pgx.Rows, error) {
	return nil, nil
}

func (*partitionRecordingDB) QueryRow(context.Context, string, ...interface{}) pgx.Row {
	return nil
}
