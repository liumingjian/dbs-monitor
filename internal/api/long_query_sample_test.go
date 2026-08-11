package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestLongQuerySampleNeverExposesSQLText(t *testing.T) {
	typeOfSample := reflect.TypeOf(api.LongQuerySample{})
	for index := 0; index < typeOfSample.NumField(); index++ {
		name := strings.Split(typeOfSample.Field(index).Tag.Get("json"), ",")[0]
		switch name {
		case "query", "sql", "query_text", "sql_text":
			t.Fatalf("long query sample exposes SQL text field %q", name)
		}
	}
}
