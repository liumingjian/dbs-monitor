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
	originalCount := int64(longQuerySampleLimit + 1)
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
	snapshot, err := decodeStatActivitySnapshot([]byte(`[]`), 0, false, encoded, originalCount, true)
	if err != nil {
		t.Fatalf("decode bounded snapshot: %v", err)
	}
	if len(snapshot.longQuerySamples) != longQuerySampleLimit || snapshot.longQuerySampleCount != originalCount || !snapshot.longQuerySamplesTruncated {
		t.Fatalf("bounded snapshot = rows %d original %d truncated %t, want %d/%d/true",
			len(snapshot.longQuerySamples), snapshot.longQuerySampleCount, snapshot.longQuerySamplesTruncated,
			longQuerySampleLimit, originalCount)
	}

	samples = append(samples, statActivitySession{
		PID: int32(longQuerySampleLimit + 1), QueryStartedAt: &queryStartedAt, QueryDurationMS: &duration,
	})
	encoded, err = json.Marshal(samples)
	if err != nil {
		t.Fatalf("encode oversized long query samples: %v", err)
	}
	if _, err := decodeStatActivitySnapshot([]byte(`[]`), 0, false, encoded, originalCount, true); err == nil {
		t.Fatalf("decoder accepted more than %d long query samples", longQuerySampleLimit)
	}
}
