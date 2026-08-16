package api_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/api"
)

func TestLongQuerySampleNeverExposesSQLText(t *testing.T) {
	sampleType := reflect.TypeOf(api.LongQuerySample{})
	for index := 0; index < sampleType.NumField(); index++ {
		name, _, _ := strings.Cut(sampleType.Field(index).Tag.Get("json"), ",")
		switch name {
		case "query", "sql", "query_text", "sql_text":
			t.Fatalf("long query sample exposes SQL text field %q", name)
		}
	}
}
