package metric_test

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestPGStatDatabaseShapeMatrix(t *testing.T) {
	task, ok := taskByID(metric.TaskStatDatabase)
	if !ok {
		t.Fatalf("task %q is missing", metric.TaskStatDatabase)
	}
	expectedColumns := taskColumns(task)
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
			columns := make([]string, len(fields))
			for index, field := range fields {
				columns[index] = field.Name
				if field.DataTypeOID != pgtype.Float8OID {
					t.Errorf("column %s type OID = %d, want float8 (%d)", field.Name, field.DataTypeOID, pgtype.Float8OID)
				}
			}
			if !slices.Equal(columns, expectedColumns) {
				t.Fatalf("columns = %v, want Task.Yields columns %v", columns, expectedColumns)
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
					if _, isFloat64 := value.(float64); !isFloat64 {
						t.Errorf("column %s Go value type = %T, want float64", expectedColumns[index], value)
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
