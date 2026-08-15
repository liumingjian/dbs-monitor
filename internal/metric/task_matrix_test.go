package metric_test

import (
	"context"
	"os"
	"regexp"
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

func TestPGStatStatementsDeclaration(t *testing.T) {
	task := requiredTask(t, metric.TaskQueryStatistics)
	if !slices.Equal(task.Requires, []metric.CapabilityID{metric.CapabilityExtensionPGStatStatements}) {
		t.Fatalf("pg_stat_statements requirements = %v, want ext.pg_stat_statements", task.Requires)
	}
	if len(task.Yields) != 0 {
		t.Fatalf("pg_stat_statements yields %d P0 metrics, want 0", len(task.Yields))
	}
	for _, fragment := range []string{
		"queryid", "dbid AS database_oid", "userid AS user_oid",
		"sum(calls)::bigint AS calls", "sum(total_exec_time)::double precision AS total_exec_time_ms",
		"GROUP BY queryid, dbid, userid", "LIMIT 500",
	} {
		if !strings.Contains(task.SQL, fragment) {
			t.Errorf("pg_stat_statements query is missing %q", fragment)
		}
	}
	if regexp.MustCompile(`(?i)\b(query|query_text|sql|sql_text)\b`).MatchString(task.SQL) {
		t.Fatal("pg_stat_statements query selects SQL text")
	}
}

func TestPGStatDatabaseShapeMatrix(t *testing.T) {
	task, ok := taskByID(metric.TaskStatDatabase)
	if !ok {
		t.Fatalf("task %q is missing", metric.TaskStatDatabase)
	}
	assertTaskShapeMatrix(t, task, metricColumnShapes(task))
}

func TestPGStatActivityShapeMatrix(t *testing.T) {
	task, ok := taskByID(metric.TaskStatActivity)
	if !ok {
		t.Fatalf("task %q is missing", metric.TaskStatActivity)
	}
	expected := metricColumnShapes(task)
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

func TestPGReplicationShapeMatrix(t *testing.T) {
	task := requiredTask(t, metric.TaskReplication)
	assertVariableRowsTaskShapeMatrix(t, task, []taskColumnShape{
		{name: "replica", oid: pgtype.TextOID},
		{name: "connection_state", oid: pgtype.TextOID},
		{name: "replay_lag_ms", oid: pgtype.Float8OID, nullable: true},
		{name: "wal_lag_bytes", oid: pgtype.Float8OID},
	})
}

func TestPGReplicationStandbyView(t *testing.T) {
	connectionURL := os.Getenv("PG17_REPLICA_URL")
	if connectionURL == "" {
		t.Skip("PG17_REPLICA_URL is not set; run make check-pg-matrix")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, connectionURL)
	if err != nil {
		t.Fatalf("connect to PostgreSQL 17 replica: %v", err)
	}
	defer conn.Close(context.Background())

	var role string
	if err := conn.QueryRow(ctx, requiredTask(t, metric.TaskRole).SQL).Scan(&role); err != nil {
		t.Fatalf("collect replica role: %v", err)
	}
	if role != "replica" {
		t.Fatalf("replica role = %q, want replica", role)
	}

	rows, err := conn.Query(ctx, requiredTask(t, metric.TaskReplication).SQL)
	if err != nil {
		t.Fatalf("collect standby replication view: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatalf("standby replication view has no row: %v", rows.Err())
	}
	values, err := rows.Values()
	if err != nil {
		t.Fatalf("read standby replication row: %v", err)
	}
	if len(values) != 4 || values[1] != "streaming" {
		t.Fatalf("standby replication row = %#v, want streaming state", values)
	}
	if _, ok := values[3].(float64); !ok {
		t.Fatalf("standby WAL lag = %T, want float64", values[3])
	}
	if rows.Next() {
		t.Fatal("standby replication view returned more than one row")
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate standby replication rows: %v", err)
	}
}

func TestPGReplicationSlotShapeMatrix(t *testing.T) {
	task := requiredTask(t, metric.TaskReplicationSlot)
	assertVariableRowsTaskShapeMatrix(t, task, []taskColumnShape{
		{name: "slot", oid: pgtype.TextOID},
		{name: "retained_wal_bytes", oid: pgtype.Float8OID},
	})
}

func TestPGPreparedXactsShapeMatrix(t *testing.T) {
	task := requiredTask(t, metric.TaskPreparedXacts)
	assertVariableRowsTaskShapeMatrix(t, task, []taskColumnShape{
		{name: "database", oid: pgtype.TextOID},
		{name: "prepared_xacts_count", oid: pgtype.Float8OID},
	})
}

func TestPGRoleShapeMatrix(t *testing.T) {
	task := requiredTask(t, metric.TaskRole)
	assertTaskShapeMatrix(t, task, []taskColumnShape{{name: "role", oid: pgtype.TextOID}})
}

func TestPGStatStatementsShapeMatrix(t *testing.T) {
	task := requiredTask(t, metric.TaskQueryStatistics)
	assertVariableRowsTaskShapeMatrix(t, task, []taskColumnShape{
		{name: "queryid", oid: pgtype.Int8OID},
		{name: "database_oid", oid: pgtype.OIDOID},
		{name: "user_oid", oid: pgtype.OIDOID},
		{name: "calls", oid: pgtype.Int8OID},
		{name: "total_exec_time_ms", oid: pgtype.Float8OID},
	})
}

type taskColumnShape struct {
	name     string
	oid      uint32
	nullable bool
}

func metricColumnShapes(task metric.Task) []taskColumnShape {
	columns := taskColumns(task)
	shapes := make([]taskColumnShape, 0, len(columns))
	for _, name := range columns {
		shapes = append(shapes, taskColumnShape{name: name, oid: pgtype.Float8OID})
	}
	return shapes
}

func assertTaskShapeMatrix(t *testing.T, task metric.Task, expected []taskColumnShape) {
	assertTaskShapeMatrixWithRowCount(t, task, expected, true)
}

func assertVariableRowsTaskShapeMatrix(t *testing.T, task metric.Task, expected []taskColumnShape) {
	assertTaskShapeMatrixWithRowCount(t, task, expected, false)
}

func assertTaskShapeMatrixWithRowCount(t *testing.T, task metric.Task, expected []taskColumnShape, requireSingleRow bool) {
	t.Helper()
	expectedColumns := make([]string, len(expected))
	for index, column := range expected {
		expectedColumns[index] = column.name
	}
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
					if value == nil && expected[index].nullable {
						continue
					}
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
			if requireSingleRow && rowCount != 1 {
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

func requiredTask(t *testing.T, id metric.TaskID) metric.Task {
	t.Helper()
	task, ok := taskByID(id)
	if !ok {
		t.Fatalf("task %q is missing", id)
	}
	return task
}
