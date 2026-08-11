package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestQueryStatisticsContractPreservesQueryIDAndExcludesSQLText(t *testing.T) {
	typeOfEntry := reflect.TypeOf(api.QueryStatisticsEntry{})
	queryID, exists := typeOfEntry.FieldByName("Queryid")
	if !exists || queryID.Type.Kind() != reflect.String {
		t.Fatalf("queryid field = %+v, want string identifier", queryID)
	}
	for index := 0; index < typeOfEntry.NumField(); index++ {
		name := strings.Split(typeOfEntry.Field(index).Tag.Get("json"), ",")[0]
		switch name {
		case "query", "sql", "query_text", "sql_text":
			t.Fatalf("query statistics entry exposes SQL text field %q", name)
		}
	}

	typeOfResponse := reflect.TypeOf(api.QueryStatisticsSnapshot{})
	unavailability, exists := typeOfResponse.FieldByName("Unavailability")
	if !exists || unavailability.Type != reflect.TypeOf((*api.Unavailability)(nil)) {
		t.Fatalf("unavailability field = %+v, want *api.Unavailability", unavailability)
	}
}
