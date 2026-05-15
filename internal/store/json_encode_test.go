package store

import (
	"encoding/json"
	"testing"
)

func assertJSONKeys(t *testing.T, payload []byte, keys ...string) {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal(payload, &m); err != nil {
		t.Fatal(err)
	}
	for _, key := range keys {
		if _, ok := m[key]; !ok {
			t.Fatalf("missing json key %q in %s", key, string(payload))
		}
	}
}

func TestDimensionStateJSONKeys(t *testing.T) {
	b, err := json.Marshal(DimensionState{
		Dimension: "gateway",
		State:     "ok",
		SinceTs:   100,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, b, "dimension", "state", "since_ts")
}

func TestIncidentJSONKeys(t *testing.T) {
	end := int64(200)
	b, err := json.Marshal(Incident{
		ID:           1,
		StartTs:      100,
		EndTs:        &end,
		OverallState: "degraded",
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, b, "id", "start_ts", "end_ts", "overall_state")
}

func TestRollup5mJSONKeys(t *testing.T) {
	b, err := json.Marshal(Rollup5m{
		BucketTs: 100,
		Probe:    "gateway",
		Metric:   "latency_ms",
		AvgVal:   12.5,
		Count:    3,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, b, "bucket_ts", "probe", "metric", "avg_val", "count")
}

func TestSampleJSONKeys(t *testing.T) {
	b, err := json.Marshal(Sample{
		Ts:     100,
		Probe:  "gateway",
		Metric: "latency_ms",
		Value:  1.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	assertJSONKeys(t, b, "ts", "probe", "metric", "value")
}
