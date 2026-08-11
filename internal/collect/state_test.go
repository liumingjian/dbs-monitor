package collect

import (
	"testing"

	"github.com/liumingjian/dbs-monitor/internal/metric"
)

func TestCollectionErrorMessages(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{code: errorCodeConnectionFailed, want: "target connection failed"},
		{code: errorCodeQueryFailed, want: "collection query failed"},
		{code: errorCodeTimeout, want: "collection deadline exceeded"},
		{code: errorCodeCounterReset, want: "database statistics counters reset"},
		{code: errorCodeDiskEmergency, want: "sample writes rejected at disk emergency watermark"},
		{code: string(metric.CapabilityBlockPermissionDenied), want: "required database role is missing"},
		{code: string(metric.CapabilityBlockExtensionMissing), want: "required database extension is missing"},
		{code: string(metric.CapabilityBlockFeatureDisabled), want: "required database feature is not enabled"},
		{code: string(metric.CapabilityBlockNotApplicableRole), want: "collection task does not apply to this database role or topology"},
		{code: string(resultSkippedBackpressure), want: "collection skipped because scheduler capacity was unavailable"},
		{code: string(resultBackoff), want: "collection deferred by failure backoff"},
		{code: "UNKNOWN", want: "collection failed"},
	}

	for _, test := range tests {
		t.Run(test.code, func(t *testing.T) {
			if got := collectionErrorMessage(test.code); got != test.want {
				t.Fatalf("collectionErrorMessage(%q) = %q, want %q", test.code, got, test.want)
			}
		})
	}
}
