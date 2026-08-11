package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestSessionSnapshotContractIsBoundedAndExcludesSQLText(t *testing.T) {
	entryType := reflect.TypeOf(api.SessionSnapshotEntry{})
	for index := 0; index < entryType.NumField(); index++ {
		name, _, _ := strings.Cut(entryType.Field(index).Tag.Get("json"), ",")
		switch name {
		case "query", "sql", "query_text", "sql_text":
			t.Fatalf("session snapshot exposes SQL text field %q", name)
		}
	}

	snapshotType := reflect.TypeOf(api.SessionSnapshot{})
	for _, field := range []string{"SampledAt", "Truncated", "Items"} {
		if _, exists := snapshotType.FieldByName(field); !exists {
			t.Fatalf("session snapshot is missing %s", field)
		}
	}
}
