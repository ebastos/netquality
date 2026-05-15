package eval

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ebastos/netquality/internal/config"
	"github.com/ebastos/netquality/internal/store"
)

func TestWorstState(t *testing.T) {
	if g := WorstState(StateOK, StateDegraded); g != StateDegraded {
		t.Fatalf("got %s", g)
	}
	if g := WorstState(StateOK, StateDown, StateDegraded); g != StateDown {
		t.Fatalf("got %s", g)
	}
}

func TestRank(t *testing.T) {
	if Rank(StateDown) <= Rank(StateOK) {
		t.Fatal("down should rank higher")
	}
}

// TestEvaluateStoresThresholdInfo verifies that after a full evaluation cycle,
// the persisted DimensionState.Detail contains a "thresholds" map with concrete
// degraded/down values and their source ("config" or "baseline"). This is the
// foundation for rich incident and Analysis explanations.
func TestEvaluateStoresThresholdInfo(t *testing.T) {
	cfg, err := config.Load(filepath.Join("..", "..", "deploy", "config.example.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	// Force non-warm path so we get deterministic "config" source for the test.
	cfg.Baseline.WarmupDays = 0

	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "eval-detail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	engine := NewEngine(cfg, db)
	ctx := context.Background()

	// Construct samples that should push gateway into degraded via loss_pct
	// (using values above the example config thresholds: loss_pct_degraded=2).
	samplesByProbe := map[string][]store.Sample{
		"gateway": {
			{Ts: 1000, Probe: "gateway", Metric: "loss_pct", Value: 4.0},
			{Ts: 1000, Probe: "gateway", Metric: "latency_ms", Value: 30},
			{Ts: 1000, Probe: "gateway", Metric: "jitter_ms", Value: 5},
			{Ts: 1000, Probe: "gateway", Metric: "ok", Value: 1},
		},
		"dns": {
			{Ts: 1000, Probe: "dns", Metric: "latency_ms", Value: 60},
			{Ts: 1000, Probe: "dns", Metric: "ok", Value: 1},
		},
	}

	if err := engine.Evaluate(ctx, samplesByProbe); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	states, err := db.GetStates(ctx)
	if err != nil {
		t.Fatal(err)
	}

	foundGateway := false
	for _, s := range states {
		if s.Dimension != "gateway" {
			continue
		}
		foundGateway = true

		thresh, ok := s.Detail["thresholds"].(map[string]any)
		if !ok {
			t.Fatalf("expected 'thresholds' map in gateway detail, got: %#v", s.Detail)
		}

		// latency_ms should exist (even if loss was the trigger)
		lat, ok := thresh["latency_ms"].(map[string]any)
		if !ok {
			t.Fatalf("expected latency_ms threshold info, got: %#v", thresh)
		}
		if lat["source"] != "config" {
			t.Errorf("expected source 'config' for non-warm path, got %v", lat["source"])
		}
		if _, ok := lat["degraded"]; !ok {
			t.Error("latency_ms.degraded missing")
		}
		if _, ok := lat["down"]; !ok {
			t.Error("latency_ms.down missing")
		}

		// loss_pct should also be present for gateway
		loss, ok := thresh["loss_pct"].(map[string]any)
		if !ok {
			t.Fatalf("expected loss_pct threshold info for gateway, got: %#v", thresh)
		}
		if loss["source"] != "config" {
			t.Errorf("expected loss_pct source 'config', got %v", loss["source"])
		}
	}
	if !foundGateway {
		t.Fatal("no gateway dimension state was persisted")
	}
}
