package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestQueryStatisticsContractPreservesQueryIDAndExcludesSQLText(t *testing.T) {
	entryType := reflect.TypeOf(api.QueryStatisticsEntry{})
	queryIDField, exists := entryType.FieldByName("Queryid")
	if !exists || queryIDField.Type.Kind() != reflect.String {
		t.Fatalf("queryid field = %+v, want string identifier", queryIDField)
	}
	for index := 0; index < entryType.NumField(); index++ {
		name := strings.Split(entryType.Field(index).Tag.Get("json"), ",")[0]
		switch name {
		case "query", "sql", "query_text", "sql_text":
			t.Fatalf("query statistics entry exposes SQL text field %q", name)
		}
	}

	snapshotType := reflect.TypeOf(api.QueryStatisticsSnapshot{})
	unavailabilityField, exists := snapshotType.FieldByName("Unavailability")
	if !exists || unavailabilityField.Type != reflect.TypeOf((*api.Unavailability)(nil)) {
		t.Fatalf("unavailability field = %+v, want *api.Unavailability", unavailabilityField)
	}
}
