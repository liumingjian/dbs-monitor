package metric_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestPGStatActivityUsesSingleSnapshot(t *testing.T) {
	task, ok := taskByID(metric.TaskStatActivity)
	if !ok {
		t.Fatalf("task %q is missing", metric.TaskStatActivity)
	}
	if count := strings.Count(task.SQL, "FROM pg_stat_activity"); count != 1 {
		t.Fatalf("pg_stat_activity scans = %d, want 1", count)
	}
	for _, column := range []string{
		"snapshot_at", "sessions", "sessions_truncated",
		"long_query_samples", "long_query_samples_truncated",
	} {
		if !strings.Contains(task.SQL, " AS "+column) {
			t.Errorf("shared snapshot query does not expose %s", column)
		}
	}
	if strings.Contains(task.SQL, "query::text") || strings.Contains(task.SQL, "query AS") {
		t.Fatal("shared snapshot query selects SQL text")
	}
	if !strings.Contains(task.SQL, "LIMIT 100") || !strings.Contains(task.SQL, "LIMIT 500") {
		t.Fatal("shared snapshot query does not enforce sample and session limits")
	}
}

func TestPGStatDatabaseShapeMatrix(t *testing.T) {
	task, ok := taskByID(metric.TaskStatDatabase)
	if !ok {
		t.Fatalf("task %q is missing", metric.TaskStatDatabase)
	}
	expected := make([]taskColumnShape, 0, len(task.Yields))
	for _, name := range taskColumns(task) {
		expected = append(expected, taskColumnShape{name: name, oid: pgtype.Float8OID})
	}
	assertTaskShapeMatrix(t, task, expected)
}

func TestPGStatActivityShapeMatrix(t *testing.T) {
	task, ok := taskByID(metric.TaskStatActivity)
	if !ok {
		t.Fatalf("task %q is missing", metric.TaskStatActivity)
	}
	expected := make([]taskColumnShape, 0, 15)
	for _, name := range taskColumns(task) {
		expected = append(expected, taskColumnShape{name: name, oid: pgtype.Float8OID})
	}
	expected = append(expected,
		taskColumnShape{name: "snapshot_at", oid: pgtype.TimestamptzOID},
		taskColumnShape{name: "sessions", oid: pgtype.JSONBOID},
		taskColumnShape{name: "session_count", oid: pgtype.Int8OID},
		taskColumnShape{name: "sessions_truncated", oid: pgtype.BoolOID},
		taskColumnShape{name: "long_query_samples", oid: pgtype.JSONBOID},
		taskColumnShape{name: "long_query_sample_count", oid: pgtype.Int8OID},
		taskColumnShape{name: "long_query_samples_truncated", oid: pgtype.BoolOID},
	)
	assertTaskShapeMatrix(t, task, expected)
}

type taskColumnShape struct {
	name string
	oid  uint32
}

func assertTaskShapeMatrix(t *testing.T, task metric.Task, expected []taskColumnShape) {
	t.Helper()
	targets := []struct {
		version string
		urlEnv  string
	}{
		{version: "13", urlEnv: "PG13_URL"},
		{version: "14", urlEnv: "PG14_URL"},
		{version: "15", urlEnv: "PG15_URL"},
		{version: "16", urlEnv: "PG16_URL"},
		{version: "17", urlEnv: "PG17_URL"},
	}

	for _, target := range targets {
		t.Run("PG"+target.version, func(t *testing.T) {
			connectionURL := os.Getenv(target.urlEnv)
			if connectionURL == "" {
				t.Skipf("%s is not set; run make check-pg-matrix", target.urlEnv)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			conn, err := pgx.Connect(ctx, connectionURL)
			if err != nil {
				t.Fatalf("connect to PostgreSQL %s: %v", target.version, err)
			}
			defer conn.Close(context.Background())

			rows, err := conn.Query(ctx, task.SQL)
			if err != nil {
				t.Fatalf("execute %s: %v", task.ID, err)
			}
			defer rows.Close()
			fields := rows.FieldDescriptions()
			if len(fields) != len(expected) {
				t.Fatalf("column count = %d, want %d", len(fields), len(expected))
			}
			columns := make([]string, len(fields))
			for index, field := range fields {
				columns[index] = field.Name
				if field.DataTypeOID != expected[index].oid {
					t.Errorf("column %s type OID = %d, want %d", field.Name, field.DataTypeOID, expected[index].oid)
				}
			}
			expectedColumns := make([]string, len(expected))
			for index, column := range expected {
				expectedColumns[index] = column.name
			}
			if !slices.Equal(columns, expectedColumns) {
				t.Fatalf("columns = %v, want task output columns %v", columns, expectedColumns)
			}

			rowCount := 0
			for rows.Next() {
				rowCount++
				values, err := rows.Values()
				if err != nil {
					t.Fatalf("read result row: %v", err)
				}
				if len(values) != len(expectedColumns) {
					t.Fatalf("row width = %d, want %d", len(values), len(expectedColumns))
				}
				for index, value := range values {
					if expected[index].oid == pgtype.Float8OID {
						if _, isFloat64 := value.(float64); !isFloat64 {
							t.Errorf("column %s Go value type = %T, want float64", expectedColumns[index], value)
						}
					}
				}
			}
			if err := rows.Err(); err != nil {
				t.Fatalf("iterate result: %v", err)
			}
			if rowCount != 1 {
				t.Fatalf("row count = %d, want 1", rowCount)
			}
		})
	}
}

func taskColumns(task metric.Task) []string {
	seen := make(map[string]struct{})
	columns := make([]string, 0, len(task.Yields))
	for _, yield := range task.Yields {
		for _, column := range yield.Columns {
			if _, exists := seen[column]; exists {
				continue
			}
			seen[column] = struct{}{}
			columns = append(columns, column)
		}
	}
	return columns
}
