package collect

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDecodeStatActivitySnapshotEnforcesSampleLimit(t *testing.T) {
	observedAt := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	queryStartedAt := observedAt.Add(-time.Minute)
	duration := int64(time.Minute / time.Millisecond)
	samples := make([]statActivitySession, longQuerySampleLimit)
	for index := range samples {
		samples[index] = statActivitySession{
			PID: int32(index + 1), QueryStartedAt: &queryStartedAt,
			QueryDurationMS: &duration, BlockingPIDs: []int32{},
		}
	}
	encoded, err := json.Marshal(samples)
	if err != nil {
		t.Fatalf("encode long query samples: %v", err)
	}
	snapshot, err := decodeStatActivitySnapshot([]byte(`[]`), 0, false, encoded, 101, true)
	if err != nil {
		t.Fatalf("decode bounded snapshot: %v", err)
	}
	if len(snapshot.longQuerySamples) != 100 || snapshot.longQuerySampleCount != 101 || !snapshot.longQueriesTruncated {
		t.Fatalf("bounded snapshot = rows %d original %d truncated %t, want 100/101/true",
			len(snapshot.longQuerySamples), snapshot.longQuerySampleCount, snapshot.longQueriesTruncated)
	}

	samples = append(samples, statActivitySession{
		PID: 101, QueryStartedAt: &queryStartedAt, QueryDurationMS: &duration,
	})
	encoded, err = json.Marshal(samples)
	if err != nil {
		t.Fatalf("encode oversized long query samples: %v", err)
	}
	if _, err := decodeStatActivitySnapshot([]byte(`[]`), 0, false, encoded, 101, true); err == nil {
		t.Fatal("decoder accepted more than 100 long query samples")
	}
}
