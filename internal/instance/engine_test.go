package instance_test

import (
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/dbengine"
	"github.com/liumingjian/dbs-monitor/internal/instance"
)

func TestResolveBootstrapDatabase(t *testing.T) {
	tests := []struct {
		name      string
		engine    instance.Engine
		requested string
		want      string
	}{
		{"填了就用填的", instance.EnginePostgreSQL, "orders", "orders"},
		{"两头空白不算填", instance.EnginePostgreSQL, "  orders  ", "orders"},
		{"PostgreSQL 留空退回 postgres", instance.EnginePostgreSQL, "", "postgres"},
		{"PostgreSQL 只填空白也退回 postgres", instance.EnginePostgreSQL, "   ", "postgres"},
		{"未知引擎没有默认库", instance.Engine("MYSQL"), "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := instance.ResolveBootstrapDatabase(tt.engine, tt.requested); got != tt.want {
				t.Fatalf("ResolveBootstrapDatabase(%q, %q) = %q, want %q", tt.engine, tt.requested, got, tt.want)
			}
		})
	}
}

func TestEngineValidity(t *testing.T) {
	if !instance.EnginePostgreSQL.ValidForInstance() {
		t.Fatal("POSTGRESQL should be a known engine")
	}
	for _, unknown := range []instance.Engine{"", "MYSQL", "postgresql"} {
		if unknown.ValidForInstance() {
			t.Fatalf("engine %q should not be known yet", unknown)
		}
	}
	// AGNOSTIC 是指标目录侧的取值：目录里有「与引擎无关」的行，实例却总是连到某个具体产品。
	if dbengine.Agnostic.ValidForInstance() {
		t.Fatal("AGNOSTIC must never be a valid instance engine")
	}
}

func TestBootstrapDatabaseColumnKeepsEmptyAsNull(t *testing.T) {
	if column := instance.BootstrapDatabaseColumn(""); column.Valid {
		t.Fatalf("empty bootstrap database should be NULL, got %q", column.String)
	}
	column := instance.BootstrapDatabaseColumn("postgres")
	if !column.Valid || column.String != "postgres" {
		t.Fatalf("bootstrap database column = %+v, want valid postgres", column)
	}
}
