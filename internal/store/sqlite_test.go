package store

import (
	"context"
	"path/filepath"
	"testing"
)

func TestOpenInsertStates(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	if err := db.InsertSamples(ctx, []Sample{
		{Ts: NowUnix(), Probe: "gateway", Metric: "latency_ms", Value: 12, Labels: map[string]string{}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.SetState(ctx, "overall", StateOK, NowUnix(), map[string]any{}); err != nil {
		t.Fatal(err)
	}
	states, err := db.GetStates(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("states len %d", len(states))
	}
}

const StateOK = "ok"

func TestPercentile(t *testing.T) {
	vals := []float64{1, 2, 3, 4, 100}
	if p := Percentile(vals, 0.95); p != 100 {
		t.Fatalf("p95 got %v", p)
	}
}

func TestBuildRollups(t *testing.T) {
	ts := int64(300)
	samples := []Sample{
		{Ts: ts, Probe: "gateway", Metric: "latency_ms", Value: 10},
		{Ts: ts + 10, Probe: "gateway", Metric: "latency_ms", Value: 20},
	}
	rollups := BuildRollupsFromSamples(samples, 300)
	if len(rollups) != 1 {
		t.Fatalf("rollups %d", len(rollups))
	}
	if rollups[0].AvgVal != 15 {
		t.Fatalf("avg %v", rollups[0].AvgVal)
	}
}
