package collect

import "testing"

func TestCollectionErrorMessages(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{code: errorCodeConnectionFailed, want: "target connection failed"},
		{code: errorCodeQueryFailed, want: "collection query failed"},
		{code: errorCodeTimeout, want: "collection deadline exceeded"},
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
